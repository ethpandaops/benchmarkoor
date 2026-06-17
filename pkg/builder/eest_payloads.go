package builder

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/datadir"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/ethpandaops/benchmarkoor/pkg/gitrepo"
	"github.com/sirupsen/logrus"
)

// EESTPayloadsBuilderName is the value of the benchmarkoor.builder label
// set on every eest_payloads container, and the value returned by Name().
const EESTPayloadsBuilderName = "eest-payloads"

const (
	// eestBuildNetwork is the docker/podman network shared by the filler
	// client and the fill-stateful container so they can reach each other.
	eestBuildNetwork = "benchmarkoor-build"

	// fillerReadyTimeout bounds how long we wait for the filler client's RPC
	// to answer — opening a large archive snapshot can take several minutes.
	fillerReadyTimeout = 15 * time.Minute

	// fillerStopTimeoutSec is the graceful-stop window for the filler client.
	fillerStopTimeoutSec = 30

	// In-container paths used by the fill-stateful container.
	fillJWTPath    = "/jwt/jwtsecret"
	fillOutputPath = "/out"
	fillStubsPath  = "/stubs.json"
	// fillRepoPath is the fill image's WORKDIR (the execution-specs checkout);
	// a config-selected EEST repo clone is mounted here when configured.
	fillRepoPath = "/eest"

	// minerGasLimit is the huge gas limit the filler geth is started with so
	// benchmark blocks of any size can be built (mirrors the fill-stateful docs).
	minerGasLimit = "1000000000000"
)

// EESTPayloadsBuilder generates stateful EEST benchmark fixtures. Per target
// it boots a filler EL client on a writable copy of a pre-populated snapshot
// datadir, runs fill-stateful against the live client, then tears it down.
type EESTPayloadsBuilder struct {
	log       logrus.FieldLogger
	cfg       *config.EESTPayloadsConfig
	runtime   string
	mgr       docker.ContainerManager
	registry  client.Registry
	repoCache string
}

// NewEESTPayloadsBuilder constructs a builder bound to a specific container
// manager. cacheDir is the shared on-disk cache (global.directories.cachedir);
// EEST repo clones are cached under <cacheDir>/eest-repos so recurring builds
// of the same ref don't re-clone. The caller is expected to have Start()'d the
// manager and to Stop() it after the last Build() call.
func NewEESTPayloadsBuilder(
	log logrus.FieldLogger,
	cfg *config.EESTPayloadsConfig,
	runtime string,
	mgr docker.ContainerManager,
	cacheDir string,
) *EESTPayloadsBuilder {
	return &EESTPayloadsBuilder{
		log:       log.WithField("component", "builder.eest_payloads"),
		cfg:       cfg,
		runtime:   runtime,
		mgr:       mgr,
		registry:  client.NewRegistry(),
		repoCache: filepath.Join(cacheDir, "eest-repos"),
	}
}

// Name implements Builder.
func (b *EESTPayloadsBuilder) Name() string {
	return EESTPayloadsBuilderName
}

// Targets implements Builder.
func (b *EESTPayloadsBuilder) Targets() []TargetInfo {
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
func (b *EESTPayloadsBuilder) Build(ctx context.Context, name string, opts BuildOptions) (bool, error) {
	idx := b.findTargetIndex(name)
	if idx < 0 {
		return false, fmt.Errorf("no target named %q", name)
	}

	resolved := b.cfg.ResolveTarget(idx)
	target := &resolved

	// Keep only the target on the per-line logger; source_dir/output_dir/fork
	// are reference details logged once below rather than suffixed onto every
	// orchestration and streamed-client log line.
	log := b.log.WithField("target", target.EffectiveName())

	force := opts.Force || target.Force

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

	if err := b.checkInputs(target); err != nil {
		return false, err
	}

	if err := prepareOutputDir(target.OutputDir, force); err != nil {
		return false, err
	}

	return false, b.run(ctx, log, target)
}

// findTargetIndex returns the index of the first target whose EffectiveName
// matches name, or -1 when nothing matches.
func (b *EESTPayloadsBuilder) findTargetIndex(name string) int {
	for i := range b.cfg.Targets {
		if b.cfg.Targets[i].EffectiveName() == name {
			return i
		}
	}

	return -1
}

// checkInputs verifies the build-time inputs exist. Existence is checked
// here (not at config-validation time) because a state-actor target earlier
// in the same config may still need to produce source_dir.
func (b *EESTPayloadsBuilder) checkInputs(t *config.EESTPayloadTarget) error {
	if info, err := os.Stat(t.SourceDir); err != nil {
		return fmt.Errorf("source_dir %q: %w", t.SourceDir, err)
	} else if !info.IsDir() {
		return fmt.Errorf("source_dir %q is not a directory", t.SourceDir)
	}

	if t.GenesisFile != "" {
		if _, err := os.Stat(t.GenesisFile); err != nil {
			return fmt.Errorf("genesis_file: %w", err)
		}
	}

	if t.AddressStubsFile != "" {
		if _, err := os.Stat(t.AddressStubsFile); err != nil {
			return fmt.Errorf("address_stubs_file: %w", err)
		}
	}

	return nil
}

// run performs the orchestration: temp JWT, network, datadir copy, filler
// boot, fill-stateful, and teardown.
func (b *EESTPayloadsBuilder) run(ctx context.Context, log logrus.FieldLogger, t *config.EESTPayloadTarget) error {
	// Record the build details once; the per-line logger carries only target.
	log.WithFields(logrus.Fields{
		"filler_client": t.FillerClient,
		"source_dir":    t.SourceDir,
		"output_dir":    t.OutputDir,
		"fork":          t.Fork,
	}).Info("Generating EEST payloads")

	// Clone the EEST repo at the configured ref into the cache and mount it into
	// the fill container at /eest. The fill image carries only the uv/python
	// toolchain (no repo), so this is always done; the EEST version is
	// config-driven (eest_repo / eest_ref).
	repo, ref := b.cfg.ResolveEESTRepo(), b.cfg.ResolveEESTRef()

	eestRepoPath, err := gitrepo.CloneOrUpdate(ctx, log, repo, ref, b.repoCache)
	if err != nil {
		return fmt.Errorf("cloning EEST repo %s@%s: %w", repo, ref, err)
	}

	sha, _ := gitrepo.HeadSHA(ctx, eestRepoPath)
	log.WithFields(logrus.Fields{
		"repo": repo, "ref": ref, "commit": sha, "path": eestRepoPath,
	}).Info("Using cloned EEST repo for fill")

	spec, err := b.registry.Get(client.ClientType(t.FillerClient))
	if err != nil {
		return fmt.Errorf("resolving filler client %q: %w", t.FillerClient, err)
	}

	jwtPath, cleanupJWT, err := writeTempJWT(b.cfg.JWT)
	if err != nil {
		return err
	}

	defer cleanupJWT()

	if err := b.mgr.EnsureNetwork(ctx, eestBuildNetwork); err != nil {
		return fmt.Errorf("ensuring network %q: %w", eestBuildNetwork, err)
	}

	provider, err := datadir.NewProvider(b.log, t.DataDirMethod)
	if err != nil {
		return fmt.Errorf("creating datadir provider: %w", err)
	}

	log.Info("Preparing writable copy of snapshot datadir")

	prepared, err := provider.Prepare(ctx, &datadir.ProviderConfig{
		SourceDir:  t.SourceDir,
		InstanceID: "eest-fill-" + t.EffectiveName(),
		TmpDir:     mountTempDir(),
	})
	if err != nil {
		return fmt.Errorf("preparing datadir copy: %w", err)
	}

	defer func() {
		if cleanupErr := prepared.Cleanup(); cleanupErr != nil {
			log.WithError(cleanupErr).Warn("Failed to clean up datadir copy")
		}
	}()

	// Stream the filler's logs for the lifetime of this build.
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	fillerID, fillerIP, configCleanup, err := b.startFiller(ctx, streamCtx, log, t, spec, prepared.MountPath, jwtPath)
	if err != nil {
		return err
	}

	// Order (defers run LIFO): stop the container first, then remove the temp
	// config file it bind-mounted. The host file must outlive the container —
	// removing it earlier makes /tmp/config.toml vanish inside the container
	// (Docker Desktop syncs the bind mount by path), which geth reports as
	// "open /tmp/config.toml: no such file or directory".
	defer configCleanup()
	defer b.stopFiller(log, fillerID)

	log.Info("Waiting for filler client RPC to become ready")

	readyCtx, cancel := context.WithTimeout(ctx, fillerReadyTimeout)
	version, err := waitForRPC(readyCtx, fillerIP, spec.RPCPort())
	cancel()

	if err != nil {
		return fmt.Errorf("filler client never became ready: %w", err)
	}

	snapshotHash, err := getLatestBlockHash(ctx, fillerIP, spec.RPCPort())
	if err != nil {
		return fmt.Errorf("fetching snapshot block hash: %w", err)
	}

	log.WithFields(logrus.Fields{
		"client_version": version,
		"snapshot_block": snapshotHash,
	}).Info("Filler client ready; running fill-stateful")

	return b.runFill(ctx, log, t, fillerIP, spec, jwtPath, snapshotHash, eestRepoPath)
}

// startFiller boots the filler EL client and returns its container ID and IP.
//
// On success it returns a cleanup func that removes the temp config file; the
// caller must defer it for the lifetime of the container (the bind-mounted host
// file has to outlive geth's startup read of /tmp/config.toml). On error the
// cleanup is run here and a no-op is returned.
func (b *EESTPayloadsBuilder) startFiller(
	ctx, streamCtx context.Context,
	log logrus.FieldLogger,
	t *config.EESTPayloadTarget,
	spec client.Spec,
	dataMount, jwtPath string,
) (id string, ip string, cleanup func(), err error) {
	configCleanup := func() {}

	// Until the container is successfully handed off to the caller, any error
	// return must remove the temp config file itself — the caller only defers
	// the cleanup once startFiller succeeds.
	defer func() {
		if err != nil {
			configCleanup()
		}
	}()

	if err = b.mgr.PullImage(ctx, t.FillerImage, b.cfg.PullPolicy); err != nil {
		return "", "", nil, fmt.Errorf("pulling filler image %q: %w", t.FillerImage, err)
	}

	mounts := []docker.Mount{
		{Source: dataMount, Target: spec.DataDir(), Type: "bind"},
		{Source: jwtPath, Target: spec.JWTPath(), Type: "bind", ReadOnly: true},
	}

	if files := spec.DefaultConfigFiles(); len(files) > 0 {
		configMounts, cfgCleanup, cfgErr := writeTempConfigFiles(files)
		if cfgErr != nil {
			err = cfgErr

			return "", "", nil, cfgErr
		}

		mounts = append(mounts, configMounts...)
		configCleanup = cfgCleanup
	}

	if t.GenesisFile != "" {
		mounts = append(mounts, docker.Mount{
			Source: t.GenesisFile, Target: spec.GenesisPath(), Type: "bind", ReadOnly: true,
		})
	}

	suffix, err := randSuffix()
	if err != nil {
		return "", "", nil, fmt.Errorf("generating container name suffix: %w", err)
	}

	cmd := fillerGethCommand(t, spec)

	containerSpec := &docker.ContainerSpec{
		Name:        fmt.Sprintf("benchmarkoor-build-eest-filler-%s-%s", t.FillerClient, suffix),
		Image:       t.FillerImage,
		Command:     cmd,
		Mounts:      mounts,
		NetworkName: eestBuildNetwork,
		SecurityOpt: []string{"seccomp=unconfined"},
		// Run as the invoking host user so the state the filler writes into the
		// copied datadir is owned by that user and can be cleaned up afterwards
		// (the copy is made by the host user, not root).
		User:   currentUserSpec(),
		Labels: b.labels(t),
	}

	log.WithField("argv", cmd).Info("Starting filler client")

	id, err = b.mgr.CreateContainer(ctx, containerSpec)
	if err != nil {
		return "", "", nil, fmt.Errorf("creating filler container: %w", err)
	}

	if err = b.mgr.StartContainer(ctx, id); err != nil {
		_ = b.mgr.RemoveContainer(context.Background(), id)

		return "", "", nil, fmt.Errorf("starting filler container: %w", err)
	}

	go func() {
		// Stream filler-client output in the same "🟣 … CLIE | <client> |"
		// format `benchmarkoor run` uses for client logs.
		w := containerStream("CLIE", t.FillerClient)
		if streamErr := b.mgr.StreamLogs(streamCtx, id, w, w); streamErr != nil {
			log.WithError(streamErr).Debug("Filler log streaming stopped")
		}
	}()

	ip, err = b.mgr.GetContainerIP(ctx, id, eestBuildNetwork)
	if err != nil {
		_ = b.mgr.RemoveContainer(context.Background(), id)

		return "", "", nil, fmt.Errorf("getting filler container IP: %w", err)
	}

	return id, ip, configCleanup, nil
}

// stopFiller stops and removes the filler container. It uses a background
// context so cleanup still runs when the build context is cancelled.
func (b *EESTPayloadsBuilder) stopFiller(log logrus.FieldLogger, id string) {
	timeout := fillerStopTimeoutSec
	if err := b.mgr.StopContainer(context.Background(), id, &timeout); err != nil {
		log.WithError(err).Warn("Failed to stop filler container")
	}

	if err := b.mgr.RemoveContainer(context.Background(), id); err != nil {
		log.WithError(err).Warn("Failed to remove filler container")
	}
}

// runFill runs the fill-stateful container against the live filler client.
func (b *EESTPayloadsBuilder) runFill(
	ctx context.Context,
	log logrus.FieldLogger,
	t *config.EESTPayloadTarget,
	fillerIP string,
	spec client.Spec,
	jwtPath, snapshotHash, eestRepoPath string,
) error {
	if err := b.mgr.PullImage(ctx, b.cfg.FillImage, b.cfg.PullPolicy); err != nil {
		return fmt.Errorf("pulling fill image %q: %w", b.cfg.FillImage, err)
	}

	args := buildFillArgs(b.cfg.ResolveFillCommand(), t, fillerIP, spec, snapshotHash)

	mounts := []docker.Mount{
		{Source: jwtPath, Target: fillJWTPath, Type: "bind", ReadOnly: true},
		{Source: t.OutputDir, Target: fillOutputPath, Type: "bind"},
	}

	if t.AddressStubsFile != "" {
		mounts = append(mounts, docker.Mount{
			Source: t.AddressStubsFile, Target: fillStubsPath, Type: "bind", ReadOnly: true,
		})
	}

	// Run as the invoking host user so fixtures written to /out are owned by
	// that user. The uv/pytest fill image keeps writable state under /tmp.
	env := map[string]string{
		"HOME":           "/tmp",
		"UV_CACHE_DIR":   "/tmp/uv-cache",
		"PYTEST_ADDOPTS": "-o cache_dir=/tmp/.pytest_cache",
		// fill-stateful reads its commit hash from the /eest git checkout; as a
		// non-root user git can refuse with "dubious ownership". Inject
		// safe.directory=* via git's env-based config so it trusts the repo.
		"GIT_CONFIG_COUNT":   "1",
		"GIT_CONFIG_KEY_0":   "safe.directory",
		"GIT_CONFIG_VALUE_0": "*",
	}

	// Mount the host-cloned, user-owned EEST checkout at /eest. uv builds the
	// venv into this writable dir on first use (cached across runs), so we do
	// NOT skip the sync — the toolchain image carries no prebuilt venv.
	mounts = append(mounts, docker.Mount{
		Source: eestRepoPath, Target: fillRepoPath, Type: "bind",
	})

	suffix, err := randSuffix()
	if err != nil {
		return fmt.Errorf("generating container name suffix: %w", err)
	}

	containerSpec := &docker.ContainerSpec{
		Name:        fmt.Sprintf("benchmarkoor-build-eest-fill-%s-%s", t.FillerClient, suffix),
		Image:       b.cfg.FillImage,
		Command:     args,
		Mounts:      mounts,
		NetworkName: eestBuildNetwork,
		User:        currentUserSpec(),
		Env:         env,
		Labels:      b.labels(t),
	}

	tail := newTailBuffer(64 * 1024)
	out := io.MultiWriter(containerStream("BULD", "fill-stateful"), tail)

	log.WithField("argv", args).Info("Running fill-stateful")

	if err := b.mgr.RunInitContainer(ctx, containerSpec, out, out); err != nil {
		return fmt.Errorf("running fill-stateful: %w (output tail: %s)", err, tail.String())
	}

	log.Info("Build completed")

	return nil
}

// labels returns the standard label set for a builder container.
func (b *EESTPayloadsBuilder) labels(t *config.EESTPayloadTarget) map[string]string {
	return map[string]string{
		"benchmarkoor.managed-by": "benchmarkoor",
		"benchmarkoor.builder":    EESTPayloadsBuilderName,
		"benchmarkoor.client":     t.FillerClient,
		"benchmarkoor.target":     t.EffectiveName(),
		"benchmarkoor.output-dir": t.OutputDir,
	}
}

// fillerGethCommand builds the geth argv for the filler client. Only geth is
// supported today (validated in config), so the namespaces and flags are
// geth-specific: the http API exposes the testing/engine/miner namespaces
// fill-stateful needs, archive gcmode keeps full state, and peering is
// disabled. spec supplies the in-container paths and ports.
func fillerGethCommand(t *config.EESTPayloadTarget, spec client.Spec) []string {
	args := []string{
		"--config=/tmp/config.toml",
		"--datadir=" + spec.DataDir(),
		"--port=0",
		"--nodiscover",
		"--maxpeers=0",
		"--bootnodes=",
		"--nat=none",
		"--syncmode=full",
		"--gcmode=archive",
		"--snapshot=false",
		"--http",
		"--http.addr=0.0.0.0",
		"--http.vhosts=*",
		"--http.corsdomain=*",
		"--http.api=admin,debug,eth,miner,net,txpool,web3,testing,engine",
		"--http.port=" + strconv.Itoa(spec.RPCPort()),
		"--authrpc.jwtsecret=" + spec.JWTPath(),
		"--authrpc.addr=0.0.0.0",
		"--authrpc.port=" + strconv.Itoa(spec.EnginePort()),
		"--authrpc.vhosts=*",
		"--miner.gaslimit=" + minerGasLimit,
	}

	if t.GenesisFile != "" {
		args = append(args, spec.GenesisFlag()+spec.GenesisPath())
	}

	return append(args, t.FillerExtraArgs...)
}

// buildFillArgs assembles the fill-stateful argv: the configured command
// prefix, the live-client endpoints, the run knobs, and the test selection.
func buildFillArgs(
	prefix []string,
	t *config.EESTPayloadTarget,
	fillerIP string,
	spec client.Spec,
	snapshotHash string,
) []string {
	// NB: we deliberately do NOT pass --clean. fill-stateful's --clean does
	// shutil.rmtree(output), which fails with EBUSY when output is a bind
	// mount (it can't remove the mountpoint). benchmarkoor already owns the
	// output_dir lifecycle — Build leaves it empty (skip-if-populated; --force
	// wipes it) before we get here, so fill-stateful just mkdirs into it.
	args := append([]string{}, prefix...)
	args = append(args,
		// -v makes the underlying pytest print each test node id and its
		// outcome as it is built, so the fill progress is visible instead of
		// bare progress dots.
		"-v",
		fmt.Sprintf("--rpc-endpoint=http://%s:%d", fillerIP, spec.RPCPort()),
		fmt.Sprintf("--engine-endpoint=http://%s:%d", fillerIP, spec.EnginePort()),
		"--engine-jwt-secret-file="+fillJWTPath,
		"--fork="+t.Fork,
		"--snapshot-block="+snapshotHash,
		"--output="+fillOutputPath,
	)

	if len(t.GasBenchmarkValues) > 0 {
		vals := make([]string, len(t.GasBenchmarkValues))
		for i, v := range t.GasBenchmarkValues {
			vals[i] = strconv.Itoa(v)
		}

		// fill-stateful's --gas-benchmark-values takes one comma-separated
		// argument, e.g. "10,30" for 10M and 30M gas.
		args = append(args, "--gas-benchmark-values="+strings.Join(vals, ","))
	}

	// --fixed-opcode-count is mutually exclusive with --gas-benchmark-values
	// (config validation enforces this). A non-nil but empty list passes the
	// flag bare, which makes fill-stateful use its .fixed_opcode_counts.json.
	if t.FixedOpcodeCount != nil {
		if counts := *t.FixedOpcodeCount; len(counts) > 0 {
			vals := make([]string, len(counts))
			for i, v := range counts {
				vals[i] = strconv.FormatFloat(v, 'g', -1, 64)
			}

			args = append(args, "--fixed-opcode-count="+strings.Join(vals, ","))
		} else {
			args = append(args, "--fixed-opcode-count")
		}
	}

	if t.MaxGasPerTest != nil {
		args = append(args, fmt.Sprintf("--max-gas-per-test=%d", *t.MaxGasPerTest))
	}

	if t.RPCSeedKey != "" {
		args = append(args, "--rpc-seed-key="+t.RPCSeedKey)
	}

	if t.AddressStubsFile != "" {
		args = append(args, "--address-stubs="+fillStubsPath)
	}

	args = append(args, t.Tests...)

	if t.Filter != "" {
		args = append(args, "-k", t.Filter)
	}

	return args
}

// writeTempJWT writes the JWT secret to a temp file readable by the
// container UID (0644) and returns its path plus a cleanup callback.
func writeTempJWT(secret string) (string, func(), error) {
	f, err := os.CreateTemp(mountTempDir(), "benchmarkoor-eest-jwt-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp jwt file: %w", err)
	}

	path := f.Name()

	cleanup := func() { _ = os.Remove(path) }

	if _, err := f.WriteString(secret); err != nil {
		_ = f.Close()
		cleanup()

		return "", nil, fmt.Errorf("writing temp jwt file: %w", err)
	}

	if err := f.Close(); err != nil {
		cleanup()

		return "", nil, fmt.Errorf("closing temp jwt file: %w", err)
	}

	if err := os.Chmod(path, 0o644); err != nil {
		cleanup()

		return "", nil, fmt.Errorf("chmod temp jwt file: %w", err)
	}

	return path, cleanup, nil
}

// writeTempConfigFiles materialises a client's in-container config files
// (path → content) to host temp files and returns the corresponding
// read-only bind mounts plus a cleanup callback.
func writeTempConfigFiles(files map[string]string) ([]docker.Mount, func(), error) {
	mounts := make([]docker.Mount, 0, len(files))
	paths := make([]string, 0, len(files))

	cleanup := func() {
		for _, p := range paths {
			_ = os.Remove(p)
		}
	}

	for target, content := range files {
		f, err := os.CreateTemp(mountTempDir(), "benchmarkoor-eest-config-*")
		if err != nil {
			cleanup()

			return nil, nil, fmt.Errorf("creating temp config file: %w", err)
		}

		path := f.Name()
		paths = append(paths, path)

		if _, err := f.WriteString(content); err != nil {
			_ = f.Close()
			cleanup()

			return nil, nil, fmt.Errorf("writing temp config file: %w", err)
		}

		if err := f.Close(); err != nil {
			cleanup()

			return nil, nil, fmt.Errorf("closing temp config file: %w", err)
		}

		if err := os.Chmod(path, 0o644); err != nil {
			cleanup()

			return nil, nil, fmt.Errorf("chmod temp config file: %w", err)
		}

		mounts = append(mounts, docker.Mount{
			Source: path, Target: target, Type: "bind", ReadOnly: true,
		})
	}

	return mounts, cleanup, nil
}
