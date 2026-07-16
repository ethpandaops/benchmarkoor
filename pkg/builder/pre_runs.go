package builder

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/datadir"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/ethpandaops/benchmarkoor/pkg/fsutil"
	"github.com/ethpandaops/benchmarkoor/pkg/gitrepo"
	"github.com/sirupsen/logrus"
)

// PreRunsBuilderName is the value returned by Name() and set on the
// benchmarkoor.builder label of every pre_runs container.
const PreRunsBuilderName = "pre-runs"

// PreRunsBuilder advances a snapshot datadir before the eest_payloads stage.
// Per target it copies a snapshot datadir into output_dir, boots a filler EL
// client on it, ramps the block gas limit via empty blocks, builds a funding
// block, runs fill-stateful (with --no-reset-between-tests so the deployed
// state persists) on the configured setup tests, then stops the filler — so
// output_dir ends up holding the advanced datadir a later eest_payloads target
// consumes as its source_dir.
//
// It reuses EESTPayloadsBuilder for the shared filler-boot and fill-image
// machinery (constructed from an equivalent EESTPayloadsConfig), and adds the
// gas-bump / funding / datadir-persistence orchestration.
type PreRunsBuilder struct {
	log  logrus.FieldLogger
	cfg  *config.PreRunsConfig
	eest *EESTPayloadsBuilder
}

// NewPreRunsBuilder constructs a pre_runs builder bound to a container manager.
// cacheDir is the shared on-disk cache (global.directories.cachedir); the EEST
// repo clone is cached there under eest-repos.
func NewPreRunsBuilder(
	log logrus.FieldLogger,
	cfg *config.PreRunsConfig,
	runtime string,
	mgr docker.ContainerManager,
	cacheDir string,
) *PreRunsBuilder {
	// The shared fill toolchain (image build/pull, EEST clone, fill command) is
	// identical to eest_payloads; mirror the fields into an EESTPayloadsConfig so
	// the reused EESTPayloadsBuilder methods resolve them.
	eestCfg := &config.EESTPayloadsConfig{
		ContainerRuntime: cfg.ContainerRuntime,
		FillImage:        cfg.FillImage,
		FillDockerfile:   cfg.FillDockerfile,
		PullPolicy:       cfg.PullPolicy,
		JWT:              cfg.JWT,
		FillCommand:      cfg.FillCommand,
		EESTRepo:         cfg.EESTRepo,
		EESTRef:          cfg.EESTRef,
	}

	return &PreRunsBuilder{
		log:  log.WithField("component", "builder.pre_runs"),
		cfg:  cfg,
		eest: NewEESTPayloadsBuilder(log, eestCfg, runtime, mgr, cacheDir),
	}
}

// Name implements Builder.
func (b *PreRunsBuilder) Name() string {
	return PreRunsBuilderName
}

// Targets implements Builder.
func (b *PreRunsBuilder) Targets() []TargetInfo {
	out := make([]TargetInfo, 0, len(b.cfg.Targets))

	for i := range b.cfg.Targets {
		t := &b.cfg.Targets[i]
		out = append(out, TargetInfo{
			Name:      t.EffectiveName(),
			Client:    t.FillerClient,
			OutputDir: t.OutputDir,
		})
	}

	return out
}

// Build implements Builder.
func (b *PreRunsBuilder) Build(ctx context.Context, name string, opts BuildOptions) (bool, error) {
	idx := b.findTargetIndex(name)
	if idx < 0 {
		return false, fmt.Errorf("no target named %q", name)
	}

	resolved := b.cfg.ResolveTarget(idx)
	target := &resolved

	log := b.log.WithField("target", target.EffectiveName())

	// source_dir may live under a schelk mount (a state-actor datadir promoted
	// onto schelk, left unmounted); mount it before anything reads it.
	if err := b.eest.ensureSourceSchelkMounted(ctx, log, target.SourceDir); err != nil {
		return false, err
	}

	force := opts.Force || target.Force

	// Skip a populated output_dir unless forced. Pre-runs have no config-diff
	// fast-path (--rebuild-on-diff is treated as force for this builder).
	if !force {
		populated, err := isPopulated(target.OutputDir)
		if err != nil {
			return false, err
		}

		if populated {
			log.Info("Skipping build: output_dir already populated " +
				"(pass --force or set force: true on the target to rebuild)")

			return true, nil
		}
	}

	if err := b.checkInputs(ctx, target); err != nil {
		return false, err
	}

	if err := b.run(ctx, log, target); err != nil {
		return false, err
	}

	return false, nil
}

// findTargetIndex returns the index of the first target whose EffectiveName
// matches name, or -1 when nothing matches.
func (b *PreRunsBuilder) findTargetIndex(name string) int {
	for i := range b.cfg.Targets {
		if b.cfg.Targets[i].EffectiveName() == name {
			return i
		}
	}

	return -1
}

// checkInputs verifies the source snapshot (and optional genesis/stubs) exist.
func (b *PreRunsBuilder) checkInputs(ctx context.Context, t *config.PreRunTarget) error {
	if err := b.eest.ensureSourceSchelkMounted(ctx, b.log, t.SourceDir); err != nil {
		return err
	}

	if info, err := os.Stat(t.SourceDir); err != nil {
		return fmt.Errorf("source_dir %q: %w", t.SourceDir, err)
	} else if !info.IsDir() {
		return fmt.Errorf("source_dir %q is not a directory", t.SourceDir)
	}

	if t.Genesis != "" {
		if _, err := os.Stat(t.Genesis); err != nil {
			return fmt.Errorf("genesis: %w", err)
		}
	}

	if t.AddressStubsFile != "" {
		if _, err := os.Stat(t.AddressStubsFile); err != nil {
			return fmt.Errorf("address_stubs_file: %w", err)
		}
	}

	return nil
}

// preRunToEESTTarget projects a PreRunTarget onto an EESTPayloadTarget so the
// reused filler-boot / fill helpers (which operate on EESTPayloadTarget) apply.
// The filler boots on outputDir (already populated with a copy of the snapshot)
// mounted directly (DataDirMethod "direct"), so its writes land in outputDir and
// outputDir becomes the advanced datadir. Fixtures go to fixturesDir (a
// throwaway temp dir — the setup fixtures are not consumed by the benchmark,
// which recomputes CREATE2 addresses).
func preRunToEESTTarget(src *config.PreRunTarget, outputDir, fixturesDir string) *config.EESTPayloadTarget {
	return &config.EESTPayloadTarget{
		Name:                src.EffectiveName(),
		FillerClient:        src.FillerClient,
		SourceDir:           outputDir,
		OutputDir:           fixturesDir,
		Genesis:             src.Genesis,
		GenesisForkOverride: src.GenesisForkOverride,
		GenesisEIPOverride:  src.GenesisEIPOverride,
		FillerImage:         src.FillerImage,
		Fork:                src.Fork,
		Tests:               src.Tests,
		Filter:              src.Filter,
		Marker:              src.Marker,
		AddressStubsFile:    src.AddressStubsFile,
		AddressStubs:        src.AddressStubs,
		GasBenchmarkValues:  src.GasBenchmarkValues,
		RPCSeedKey:          src.RPCSeedKey,
		DataDirMethod:       "direct",
		FillerExtraArgs:     src.FillerExtraArgs,
	}
}

// run performs the orchestration: copy snapshot → output_dir, boot the filler
// on it, gas-bump, funding block, fill setup tests (no reset between tests),
// then stop the filler so output_dir holds the advanced datadir.
func (b *PreRunsBuilder) run(ctx context.Context, log logrus.FieldLogger, t *config.PreRunTarget) error {
	log.WithFields(logrus.Fields{
		"filler_client": t.FillerClient,
		"source_dir":    t.SourceDir,
		"output_dir":    t.OutputDir,
		"fork":          t.Fork,
		"gas_limit":     t.ResolveGasLimit(),
	}).Info("Generating pre-run datadir")

	repo, ref := b.cfg.ResolveEESTRepo(), b.cfg.ResolveEESTRef()

	eestRepoPath, err := gitrepo.CloneOrUpdate(ctx, log, repo, ref, b.eest.repoCache)
	if err != nil {
		return fmt.Errorf("cloning EEST repo %s@%s: %w", repo, ref, err)
	}

	sha, _ := gitrepo.HeadSHA(ctx, eestRepoPath)
	log.WithFields(logrus.Fields{"repo": repo, "ref": ref, "commit": sha}).
		Info("Using cloned EEST repo for fill")

	spec, err := b.eest.registry.Get(client.ClientType(t.FillerClient))
	if err != nil {
		return fmt.Errorf("resolving filler client %q: %w", t.FillerClient, err)
	}

	jwtPath, cleanupJWT, err := writeTempJWT(b.cfg.JWT)
	if err != nil {
		return err
	}

	defer cleanupJWT()

	if err := b.eest.mgr.EnsureNetwork(ctx, EESTBuildNetwork); err != nil {
		return fmt.Errorf("ensuring network %q: %w", EESTBuildNetwork, err)
	}

	// Copy the snapshot datadir into output_dir; the filler boots on it in place
	// (datadir method "direct"), so its writes persist as the advanced datadir.
	if err := prepareOutputDir(t.OutputDir, true); err != nil {
		return err
	}

	log.WithField("output_dir", t.OutputDir).Info("Copying snapshot datadir into output_dir")

	if err := fsutil.CopyDir(t.SourceDir, t.OutputDir, nil); err != nil {
		return fmt.Errorf("copying snapshot datadir: %w", err)
	}

	// Throwaway fixtures dir for the fill container's --output (setup fixtures
	// are not consumed by the benchmark, which recomputes CREATE2 addresses).
	fixturesDir, err := os.MkdirTemp(mountTempDir(), "benchmarkoor-prerun-fill-")
	if err != nil {
		return fmt.Errorf("creating temp fixtures dir: %w", err)
	}

	defer func() { _ = os.RemoveAll(fixturesDir) }()

	et := preRunToEESTTarget(t, t.OutputDir, fixturesDir)

	// genesis_fork_override / genesis_eip_override patch the boot genesis before
	// the filler mounts it (besu/reth/ethrex/nethermind) — identical to eest.
	if len(et.GenesisForkOverride) > 0 ||
		(et.GenesisEIPOverride != nil && len(et.GenesisEIPOverride.EIPs) > 0) {
		patched, cleanup, perr := patchFillerGenesis(log, et)
		if perr != nil {
			return perr
		}

		defer cleanup()

		et.Genesis = patched
	}

	if len(et.AddressStubs) > 0 {
		stubsPath, cleanup, serr := materializeAddressStubs(log, et)
		if serr != nil {
			return serr
		}

		defer cleanup()

		et.AddressStubsFile = stubsPath
	}

	provider, err := datadir.NewProvider(b.log, et.DataDirMethod)
	if err != nil {
		return fmt.Errorf("creating datadir provider: %w", err)
	}

	prepared, err := provider.Prepare(ctx, &datadir.ProviderConfig{
		SourceDir:  et.SourceDir,
		InstanceID: "prerun-" + t.EffectiveName(),
		TmpDir:     mountTempDir(),
	})
	if err != nil {
		return fmt.Errorf("preparing datadir: %w", err)
	}

	defer func() {
		if cleanupErr := prepared.Cleanup(); cleanupErr != nil {
			log.WithError(cleanupErr).Warn("Failed to clean up datadir")
		}
	}()

	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	fillerID, fillerIP, configCleanup, err := b.eest.startFiller(ctx, streamCtx, log, et, spec, prepared.MountPath, jwtPath)
	if err != nil {
		return err
	}

	defer configCleanup()
	defer b.eest.stopFiller(log, fillerID)

	log.Info("Waiting for filler client RPC to become ready")

	version, err := b.eest.waitForFillerReady(ctx, fillerID, fillerIP, spec.RPCPort())
	if err != nil {
		return err
	}

	log.WithField("client_version", version).Info("Filler client ready")

	// Gas-bump + funding block via the Engine API (benchmarkoor-driven), then
	// fill the setup tests anchored at the resulting head.
	ec, err := newEngineClient(fillerIP, spec.RPCPort(), spec.EnginePort(), b.cfg.JWT, t.Fork)
	if err != nil {
		return err
	}

	if _, err := ec.bumpGasLimit(ctx, t.ResolveGasLimit(), t.ResolveGasBumpMaxBlocks(), log); err != nil {
		return err
	}

	if _, err := ec.fundingBlock(ctx, t.FundingAccounts, log); err != nil {
		return err
	}

	snapshotHash, err := getLatestBlockHash(ctx, fillerIP, spec.RPCPort())
	if err != nil {
		return fmt.Errorf("fetching post-funding head hash: %w", err)
	}

	log.WithField("start_block", snapshotHash).Info("Running fill-stateful on setup tests")

	if err := b.runFill(ctx, log, et, t.FillEnv, fillerIP, spec, jwtPath, snapshotHash, eestRepoPath); err != nil {
		return err
	}

	log.Info("Pre-run complete; stopping filler to flush datadir")

	return nil
}

// runFill runs fill-stateful against the live filler, with
// --no-reset-between-tests so the setup state accumulates on the datadir. It
// mirrors EESTPayloadsBuilder.runFill but writes fixtures to a throwaway dir and
// records no fill-result sidecar (the pre-run's product is the datadir).
func (b *PreRunsBuilder) runFill(
	ctx context.Context,
	log logrus.FieldLogger,
	et *config.EESTPayloadTarget,
	fillEnv map[string]string,
	fillerIP string,
	spec client.Spec,
	jwtPath, snapshotHash, eestRepoPath string,
) error {
	fillImage, err := b.eest.ensureFillImage(ctx, log)
	if err != nil {
		return err
	}

	args := buildFillArgs(b.cfg.ResolveFillCommand(), et, fillerIP, spec, snapshotHash)
	// Accumulate setup state instead of rewinding to start_block after each
	// test, so the deployed contracts persist on the datadir.
	args = append(args, "--no-reset-between-tests")

	mounts := []docker.Mount{
		{Source: jwtPath, Target: fillJWTPath, Type: "bind", ReadOnly: true},
		{Source: et.OutputDir, Target: fillOutputPath, Type: "bind"},
		{Source: eestRepoPath, Target: fillRepoPath, Type: "bind"},
	}

	if et.AddressStubsFile != "" {
		mounts = append(mounts, docker.Mount{
			Source: et.AddressStubsFile, Target: fillStubsPath, Type: "bind", ReadOnly: true,
		})
	}

	env := map[string]string{
		"HOME":               "/tmp",
		"UV_CACHE_DIR":       "/tmp/uv-cache",
		"PYTEST_ADDOPTS":     "-o cache_dir=/tmp/.pytest_cache",
		"GIT_CONFIG_COUNT":   "1",
		"GIT_CONFIG_KEY_0":   "safe.directory",
		"GIT_CONFIG_VALUE_0": "*",
	}

	// Merge configured fill_env, but benchmarkoor's own keys above win so a
	// config can't break the toolchain (HOME, git safe.directory, etc.).
	for k, v := range fillEnv {
		if _, reserved := env[k]; !reserved {
			env[k] = v
		}
	}

	suffix, err := randSuffix()
	if err != nil {
		return fmt.Errorf("generating container name suffix: %w", err)
	}

	containerSpec := &docker.ContainerSpec{
		Name:        fmt.Sprintf("benchmarkoor-build-prerun-fill-%s-%s", et.FillerClient, suffix),
		Image:       fillImage,
		Command:     args,
		Mounts:      mounts,
		NetworkName: EESTBuildNetwork,
		User:        currentUserSpec(),
		Env:         env,
		Labels:      b.labels(et),
	}

	tail := newTailBuffer(64 * 1024)
	out := io.MultiWriter(containerStream("BULD", "prerun-fill-stateful"), tail)

	log.WithField("argv", args).Info("Running fill-stateful (--no-reset-between-tests)")

	if runErr := b.eest.mgr.RunInitContainer(ctx, containerSpec, out, out); runErr != nil {
		return fmt.Errorf("running fill-stateful: %w (output tail: %s)", runErr, tail.String())
	}

	return nil
}

// labels returns the standard label set for a pre_runs container.
func (b *PreRunsBuilder) labels(et *config.EESTPayloadTarget) map[string]string {
	return map[string]string{
		"benchmarkoor.managed-by": "benchmarkoor",
		"benchmarkoor.builder":    PreRunsBuilderName,
		"benchmarkoor.client":     et.FillerClient,
		"benchmarkoor.target":     et.EffectiveName(),
		"benchmarkoor.output-dir": et.OutputDir,
	}
}

// interface check.
var _ Builder = (*PreRunsBuilder)(nil)
