package builder

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/datadir"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/ethpandaops/benchmarkoor/pkg/genesis"
	"github.com/ethpandaops/benchmarkoor/pkg/gitrepo"
	"github.com/sirupsen/logrus"
)

// EESTPayloadsBuilderName is the value of the benchmarkoor.builder label
// set on every eest_payloads container, and the value returned by Name().
const EESTPayloadsBuilderName = "eest-payloads"

const (
	// EESTBuildNetwork is the docker/podman network shared by the filler
	// client and the fill-stateful container so they can reach each other.
	EESTBuildNetwork = "benchmarkoor-build"

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

	// besuDefaultPriorityFeeWei pins fill-stateful's session tip for the besu
	// filler. besu's eth_maxPriorityFeePerGas returns 0 on a freshly-booted
	// snapshot (no fee history), which fill-stateful rejects with "requires the
	// backend to carry non-zero session fees". geth suggests a non-zero tip on
	// its own, so this is only needed for besu. 1 gwei.
	besuDefaultPriorityFeeWei = "1000000000"
)

// embeddedFillDockerfile is the default fill-image Dockerfile, compiled into the
// binary so the fill image can be built without a Dockerfile on disk. It is used
// when neither fill_image nor fill_dockerfile is configured. The Dockerfile
// copies nothing from the build context (execution-specs is mounted at run
// time), so it builds against an empty context written to a temp dir.
//
//go:embed Dockerfile.eest-filler
var embeddedFillDockerfile []byte

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

	// fillImage is resolved once per builder: built from fill_dockerfile or the
	// configured fill_image. Guarded by fillImageOnce so a multi-target build
	// only builds the image a single time.
	fillImageOnce sync.Once
	fillImage     string
	fillImageErr  error
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

	// source_dir may live under a schelk mount (a state-actor datadir promoted
	// onto schelk, left unmounted). Mount it before anything reads the source:
	// the cascade fingerprint below, the genesis hash in the diff fingerprint,
	// and checkInputs all touch source_dir/genesis and would otherwise fail on an
	// unmounted path.
	if err := b.ensureSourceSchelkMounted(ctx, log, target.SourceDir); err != nil {
		return false, err
	}

	// Cascade: fold the source snapshot's fingerprint into ours so a rebuilt
	// state-actor datadir (its config changed) also invalidates these fixtures.
	// Best-effort — an absent/unreadable source sidecar contributes "".
	sourceFP := ""
	if sc, err := readBuildSidecar(target.SourceDir); err != nil {
		log.WithError(err).Warn("Failed to read source snapshot fingerprint; ignoring for cascade")
	} else if sc != nil {
		sourceFP = sc.Fingerprint
	}

	// Resolve the EEST ref to a commit SHA lazily (a network round-trip) — only
	// when a diff check or the sidecar write actually needs it.
	var eestSHA string
	var eestSHAResolved bool
	resolveEESTSHA := func() (string, error) {
		if eestSHAResolved {
			return eestSHA, nil
		}

		sha, err := gitrepo.RemoteSHA(ctx, b.cfg.ResolveEESTRepo(), b.cfg.ResolveEESTRef())
		if err != nil {
			return "", err
		}

		eestSHA, eestSHAResolved = sha, true

		return sha, nil
	}

	force := opts.Force || target.Force

	// Fast path: a populated output_dir with neither --force nor
	// --rebuild-on-diff skips before doing any fingerprint work.
	if !force && !opts.RebuildOnDiff {
		populated, err := isPopulated(target.OutputDir)
		if err != nil {
			return false, err
		}

		if populated {
			log.Info("Skipping build: output_dir already populated " +
				"(pass --force, --rebuild-on-diff, or set force: true on the target to rebuild)")

			b.backfillFillSidecarIfMissing(log, target)

			return true, nil
		}
	}

	// Compute the fingerprint up front: run() mutates target's genesis /
	// address-stubs paths to temp files it then deletes, so the sidecar must be
	// fingerprinted from the pre-run config (and from the same inputs the diff
	// check used). Best-effort — a failure (e.g. a transient ls-remote) only
	// skips the sidecar for a plain build, but is fatal for a diff check, which
	// cannot proceed without it.
	var inputs fingerprintInputs
	sha, inputsErr := resolveEESTSHA()
	if inputsErr == nil {
		inputs, inputsErr = b.eestFingerprintInputs(target, sha, sourceFP)
	}

	if !force && opts.RebuildOnDiff {
		populated, err := isPopulated(target.OutputDir)
		if err != nil {
			return false, err
		}

		if populated {
			if inputsErr != nil {
				return false, fmt.Errorf("computing fingerprint for diff check: %w", inputsErr)
			}

			dec, err := decideRebuild(target.OutputDir, inputs)
			if err != nil {
				return false, err
			}

			if !dec.rebuild {
				log.Infof("Skipping build: output_dir already populated (%s)", dec.reason)

				b.backfillFillSidecarIfMissing(log, target)

				return true, nil
			}

			log.WithField("changed", dec.changed).Info("Config changed since last build; rebuilding")

			force = true
		}
	}

	if err := b.checkInputs(ctx, target); err != nil {
		return false, err
	}

	if err := prepareOutputDir(target.OutputDir, force); err != nil {
		return false, err
	}

	if err := b.run(ctx, log, target); err != nil {
		return false, err
	}

	// Record the config fingerprint (computed pre-run) for a later
	// --rebuild-on-diff run. Best-effort; a failure must not fail the build.
	if inputsErr != nil {
		log.WithError(inputsErr).Warn("Failed to compute build fingerprint; sidecar not written")
	} else if err := writeBuildSidecar(target.OutputDir, EESTPayloadsBuilderName, inputs); err != nil {
		log.WithError(err).Warn("Failed to write build fingerprint sidecar")
	}

	return false, nil
}

// eestFingerprintInputs builds the canonical fingerprint of an eest_payloads
// target's output-affecting config: the fill parameters, the content hashes of
// its input files (genesis, address stubs, fill Dockerfile), the resolved EEST
// commit SHA, and the source snapshot's fingerprint (so a rebuilt snapshot
// cascades into a fixtures rebuild).
func (b *EESTPayloadsBuilder) eestFingerprintInputs(target *config.EESTPayloadTarget, eestSHA, sourceFingerprint string) (fingerprintInputs, error) {
	genesisHash, err := sha256File(target.Genesis)
	if err != nil {
		return nil, err
	}

	stubsHash, err := b.addressStubsHash(target)
	if err != nil {
		return nil, err
	}

	fillDockerfileHash, err := b.fillDockerfileHash()
	if err != nil {
		return nil, err
	}

	return fingerprintInputs{
		"filler_client":          target.FillerClient,
		"filler_image":           target.FillerImage,
		"filler_extra_args":      target.FillerExtraArgs,
		"fork":                   target.Fork,
		"tests":                  target.Tests,
		"filter":                 target.Filter,
		"marker":                 target.Marker,
		"gas_benchmark_values":   target.GasBenchmarkValues,
		"fixed_opcode_count":     target.FixedOpcodeCount,
		"max_gas_per_test":       target.MaxGasPerTest,
		"rpc_seed_key":           target.RPCSeedKey,
		"datadir_method":         target.DataDirMethod,
		"genesis_sha256":         genesisHash,
		"genesis_fork_override":  target.GenesisForkOverride,
		"genesis_eip_override":   target.GenesisEIPOverride,
		"address_stubs_sha256":   stubsHash,
		"fill_command":           b.cfg.ResolveFillCommand(),
		"fill_image":             b.cfg.FillImage,
		"fill_dockerfile_sha256": fillDockerfileHash,
		"eest_repo":              b.cfg.ResolveEESTRepo(),
		"eest_sha":               eestSHA,
		// source_dir alongside source_fingerprint: the fingerprint catches a
		// rebuilt source snapshot, and the path catches a swap to a *different*
		// snapshot that carries no sidecar (so contributes an empty fingerprint).
		"source_dir":         target.SourceDir,
		"source_fingerprint": sourceFingerprint,
	}, nil
}

// addressStubsHash hashes the target's address stubs — the referenced file's
// contents, or the inline map — returning "" when none are configured.
func (b *EESTPayloadsBuilder) addressStubsHash(target *config.EESTPayloadTarget) (string, error) {
	if target.AddressStubsFile != "" {
		return sha256File(target.AddressStubsFile)
	}

	if len(target.AddressStubs) > 0 {
		data, err := json.Marshal(target.AddressStubs)
		if err != nil {
			return "", fmt.Errorf("hashing address_stubs: %w", err)
		}

		return sha256Hex(data), nil
	}

	return "", nil
}

// fillDockerfileHash hashes the Dockerfile the fill image is built from (a
// custom fill_dockerfile or the embedded default), or returns "" when a
// pre-built fill_image is pulled instead (its identity is covered separately).
func (b *EESTPayloadsBuilder) fillDockerfileHash() (string, error) {
	if !b.cfg.BuildsFillImage() {
		return "", nil
	}

	if b.cfg.FillDockerfile != "" {
		return sha256File(b.cfg.FillDockerfile)
	}

	return sha256Hex(embeddedFillDockerfile), nil
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
// ensureSourceSchelkMounted mounts the schelk scratch when source_dir lives
// under a schelk mount. A preceding state-actor build promotes its datadir as
// the schelk baseline but leaves it unmounted (`schelk promote`), so the source
// snapshot — its build sidecar, the genesis alongside it, and the fixtures — is
// only a real path once remounted. Idempotent; a no-op for non-schelk sources.
func (b *EESTPayloadsBuilder) ensureSourceSchelkMounted(ctx context.Context, log logrus.FieldLogger, sourceDir string) error {
	_, isSchelk, err := datadir.SchelkDir(sourceDir)
	if err != nil {
		return fmt.Errorf("checking schelk state for source_dir %q: %w", sourceDir, err)
	}

	if isSchelk {
		if err := datadir.EnsureSchelkMounted(ctx, log); err != nil {
			return fmt.Errorf("ensuring schelk mount for source_dir %q: %w", sourceDir, err)
		}
	}

	return nil
}

func (b *EESTPayloadsBuilder) checkInputs(ctx context.Context, t *config.EESTPayloadTarget) error {
	// Build already mounts the schelk scratch up front (before the diff
	// fingerprint reads the source), so by here source_dir/genesis are real
	// paths. Re-run the idempotent ensure defensively in case checkInputs is
	// reached via a path that skipped it.
	if err := b.ensureSourceSchelkMounted(ctx, b.log, t.SourceDir); err != nil {
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

	if err := b.mgr.EnsureNetwork(ctx, EESTBuildNetwork); err != nil {
		return fmt.Errorf("ensuring network %q: %w", EESTBuildNetwork, err)
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

	// genesis_fork_override / genesis_eip_override patch the boot genesis before
	// the filler mounts it, to activate a fork the file doesn't schedule (e.g.
	// amsterdam on an osaka snapshot) — identical to the runner. Used by fillers
	// that read forks from the genesis (besu/reth/ethrex/nethermind). geth/erigon
	// boot from the datadir and instead activate forks via --override.<fork> in
	// filler_extra_args.
	if len(t.GenesisForkOverride) > 0 ||
		(t.GenesisEIPOverride != nil && len(t.GenesisEIPOverride.EIPs) > 0) {
		patched, cleanup, perr := patchFillerGenesis(log, t)
		if perr != nil {
			return perr
		}

		defer cleanup()

		t.Genesis = patched
	}

	// address_stubs defines the stub mapping inline; materialize it to a temp
	// JSON file so the existing mount + --address-stubs path (keyed on
	// AddressStubsFile) works unchanged — identical to the genesis patch above.
	if len(t.AddressStubs) > 0 {
		stubsPath, cleanup, serr := materializeAddressStubs(log, t)
		if serr != nil {
			return serr
		}

		defer cleanup()

		t.AddressStubsFile = stubsPath
	}

	// Stream the filler's logs for the lifetime of this build.
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	fillerID, fillerIP, configCleanup, err := b.startFiller(ctx, streamCtx, log, t, spec, fillerCommand(t, spec), prepared.MountPath, jwtPath)
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

	version, err := b.waitForFillerReady(ctx, fillerID, fillerIP, spec.RPCPort())
	if err != nil {
		return err
	}

	snapshotHash, err := getLatestBlockHash(ctx, fillerIP, spec.RPCPort())
	if err != nil {
		return fmt.Errorf("fetching snapshot block hash: %w", err)
	}

	log.WithFields(logrus.Fields{
		"client_version": version,
		"snapshot_block": snapshotHash,
	}).Info("Filler client ready; running fill-stateful")

	return b.runFill(ctx, log, t, fillerIP, spec, jwtPath, snapshotHash, eestRepoPath, sha)
}

// waitForFillerReady blocks until the filler's RPC answers or the filler
// container exits, whichever happens first, returning the client version.
//
// A filler that dies before its RPC comes up (panic on boot, bad flags, OOM,
// corrupt datadir) would otherwise leave waitForRPC polling a dead endpoint for
// the full fillerReadyTimeout. Watching the container exit lets us fail fast
// with the exit code instead of hanging for 15 minutes.
func (b *EESTPayloadsBuilder) waitForFillerReady(
	ctx context.Context,
	containerID, ip string,
	port int,
) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, fillerReadyTimeout)
	defer cancel()

	exitCh, exitErrCh := b.mgr.WaitForContainerExit(ctx, containerID)

	type rpcResult struct {
		version string
		err     error
	}

	resultCh := make(chan rpcResult, 1)

	go func() {
		version, err := waitForRPC(ctx, ip, port)
		resultCh <- rpcResult{version: version, err: err}
	}()

	select {
	case res := <-resultCh:
		if res.err != nil {
			return "", fmt.Errorf("filler client never became ready: %w", res.err)
		}

		return res.version, nil
	case info := <-exitCh:
		// cancel() unblocks the waitForRPC goroutine; the deferred cancel also
		// covers it, but stopping the poll immediately is tidier.
		cancel()

		return "", fmt.Errorf(
			"filler client exited before RPC became ready "+
				"(exit code %d, oom_killed=%t)",
			info.ExitCode, info.OOMKilled,
		)
	case err := <-exitErrCh:
		// A wait error from our own timeout/cancel isn't actionable — defer to
		// the RPC goroutine, whose timeout message describes it better.
		if err == nil ||
			errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			res := <-resultCh

			if res.err != nil {
				return "", fmt.Errorf("filler client never became ready: %w", res.err)
			}

			return res.version, nil
		}

		cancel()

		return "", fmt.Errorf("watching filler container: %w", err)
	}
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
	cmd []string,
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

	if t.Genesis != "" {
		mounts = append(mounts, docker.Mount{
			Source: t.Genesis, Target: spec.GenesisPath(), Type: "bind", ReadOnly: true,
		})
	}

	suffix, err := randSuffix()
	if err != nil {
		return "", "", nil, fmt.Errorf("generating container name suffix: %w", err)
	}

	containerSpec := &docker.ContainerSpec{
		Name:        fmt.Sprintf("benchmarkoor-build-eest-filler-%s-%s", t.FillerClient, suffix),
		Image:       t.FillerImage,
		Command:     cmd,
		Mounts:      mounts,
		NetworkName: EESTBuildNetwork,
		SecurityOpt: []string{"seccomp=unconfined"},
		// Run as the invoking host user so the state the filler writes into the
		// copied datadir is owned by that user and can be cleaned up afterwards
		// (the copy is made by the host user, not root).
		User: currentUserSpec(),
		// A writable HOME: as the host user there is no home dir, and some clients
		// (reth writes ~/.cache/reth/logs) abort on boot without one. Merge the
		// client's own env last so it can override.
		Env:    fillerBootEnv(spec),
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

	ip, err = b.mgr.GetContainerIP(ctx, id, EESTBuildNetwork)
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
// ensureFillImage returns the fill image reference, building it from
// fill_dockerfile (once, regardless of target count) when configured, otherwise
// pulling the configured fill_image.
func (b *EESTPayloadsBuilder) ensureFillImage(ctx context.Context, log logrus.FieldLogger) (string, error) {
	if !b.cfg.BuildsFillImage() {
		if err := b.mgr.PullImage(ctx, b.cfg.FillImage, b.cfg.PullPolicy); err != nil {
			return "", fmt.Errorf("pulling fill image %q: %w", b.cfg.FillImage, err)
		}

		return b.cfg.FillImage, nil
	}

	b.fillImageOnce.Do(func() {
		dockerfile, cleanup, err := b.resolveFillDockerfile()
		if err != nil {
			b.fillImageErr = err

			return
		}
		defer cleanup()

		tag := b.cfg.ResolveFillImageTag()
		if err := b.buildFillImage(ctx, log, dockerfile, tag); err != nil {
			b.fillImageErr = err

			return
		}

		b.fillImage = tag
	})

	return b.fillImage, b.fillImageErr
}

// resolveFillDockerfile returns the Dockerfile path to build the fill image
// from, plus a cleanup func to run once the build completes. When
// fill_dockerfile is configured it is used directly (no-op cleanup). Otherwise
// the embedded default Dockerfile is written to a temp dir — which doubles as
// the (empty) build context — and cleanup removes it.
func (b *EESTPayloadsBuilder) resolveFillDockerfile() (string, func(), error) {
	noop := func() {}

	if b.cfg.FillDockerfile != "" {
		return b.cfg.FillDockerfile, noop, nil
	}

	dir, err := os.MkdirTemp("", "benchmarkoor-fill-dockerfile-")
	if err != nil {
		return "", noop, fmt.Errorf("creating temp dir for embedded fill Dockerfile: %w", err)
	}

	path := filepath.Join(dir, "Dockerfile.eest-filler")
	if err := os.WriteFile(path, embeddedFillDockerfile, 0o600); err != nil {
		_ = os.RemoveAll(dir)

		return "", noop, fmt.Errorf("writing embedded fill Dockerfile: %w", err)
	}

	return path, func() { _ = os.RemoveAll(dir) }, nil
}

// buildFillImage builds the fill image from dockerfile, tagging it tag, by
// shelling out to the configured container runtime's `build`. The build
// context is the Dockerfile's directory.
func (b *EESTPayloadsBuilder) buildFillImage(ctx context.Context, log logrus.FieldLogger, dockerfile, tag string) error {
	contextDir := filepath.Dir(dockerfile)

	log.WithFields(logrus.Fields{
		"dockerfile": dockerfile,
		"tag":        tag,
		"runtime":    b.runtime,
	}).Info("Building fill image")

	cmd := exec.CommandContext(ctx, b.runtime, "build", "-f", dockerfile, "-t", tag, contextDir)

	w := containerStream("BULD", "fill-image-build")
	cmd.Stdout = w
	cmd.Stderr = w

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("building fill image from %q: %w", dockerfile, err)
	}

	return nil
}

const (
	// eestFillResultFile is the sidecar written next to the fixtures recording
	// how many tests the fill produced/failed (read by the build markdown summary).
	eestFillResultFile = ".benchmarkoor-fill.json"

	// pytestReportFile is the pytest-json-report output written under output_dir
	// (via PYTEST_ADDOPTS --json-report); it carries the authoritative
	// passed/failed tally. Relative to output_dir (= the fill container's /out).
	pytestReportFile = ".benchmarkoor-pytest-report.json"
)

// dirSize returns the total size in bytes of all regular files under dir.
func dirSize(dir string) int64 {
	var total int64

	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}

		return nil
	})

	return total
}

// backfillFillSidecarIfMissing writes the fill result sidecar for a skipped
// target when it's absent — e.g. fixtures produced by an older benchmarkoor or
// restored from a baseline — so the build summary still shows the target's data
// instead of just "skipped". Best-effort; an existing sidecar is left untouched.
func (b *EESTPayloadsBuilder) backfillFillSidecarIfMissing(log logrus.FieldLogger, t *config.EESTPayloadTarget) {
	if _, err := os.Stat(filepath.Join(t.OutputDir, eestFillResultFile)); err == nil {
		return
	}

	if err := recordEESTFillResult(t, eestSHAFromFingerprint(t.OutputDir)); err != nil {
		log.WithError(err).Warn("Failed to backfill fill result sidecar on skip")
	}
}

// eestSHAFromFingerprint recovers the EEST commit recorded in the build
// fingerprint sidecar, if present ("" when absent/unparseable).
func eestSHAFromFingerprint(outputDir string) string {
	data, err := os.ReadFile(filepath.Join(outputDir, buildSidecarFile))
	if err != nil {
		return ""
	}

	var sidecar struct {
		Inputs struct {
			EESTSHA string `json:"eest_sha"`
		} `json:"inputs"`
	}
	if json.Unmarshal(data, &sidecar) != nil {
		return ""
	}

	return sidecar.Inputs.EESTSHA
}

// recordEESTFillResult writes the .benchmarkoor-fill.json sidecar for the build
// summary: the target's provenance plus authoritative counts from EEST's own
// report artifacts — the generated-fixture count from .meta/index.json and the
// failed/errored count from the pytest json report. It is written even after a
// failed fill (fill-stateful continues through failures and still writes both the
// fixtures and the reports), so a failed target is still fully described.
// Best-effort: missing/unparseable reports yield zero counts rather than an error.
func recordEESTFillResult(t *config.EESTPayloadTarget, eestSHA string) error {
	result := struct {
		SourceDir    string `json:"source_dir"`
		FillerClient string `json:"filler_client"`
		FillerImage  string `json:"filler_image"`
		EESTSHA      string `json:"eest_sha"`
		Fork         string `json:"fork"`
		Filter       string `json:"filter"`
		SizeBytes    int64  `json:"size_bytes"`
		Filled       int    `json:"filled"`
		Failed       int    `json:"failed"`
	}{
		SourceDir:    t.SourceDir,
		FillerClient: t.FillerClient,
		FillerImage:  t.FillerImage,
		EESTSHA:      eestSHA,
		Fork:         t.Fork,
		Filter:       t.Filter,
		SizeBytes:    dirSize(t.OutputDir),
	}

	// Generated fixtures (filled) — EEST's index.json test_count.
	if data, err := os.ReadFile(filepath.Join(t.OutputDir, ".meta", "index.json")); err == nil {
		var idx struct {
			TestCount int `json:"test_count"`
		}
		if json.Unmarshal(data, &idx) == nil {
			result.Filled = idx.TestCount
		}
	}

	// Failures — pytest's json report summary (failed + errored).
	if data, err := os.ReadFile(filepath.Join(t.OutputDir, pytestReportFile)); err == nil {
		var rep struct {
			Summary struct {
				Failed int `json:"failed"`
				Error  int `json:"error"`
			} `json:"summary"`
		}
		if json.Unmarshal(data, &rep) == nil {
			result.Failed = rep.Summary.Failed + rep.Summary.Error
		}
	}

	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshalling fill result: %w", err)
	}

	if err := os.WriteFile(filepath.Join(t.OutputDir, eestFillResultFile), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing fill result: %w", err)
	}

	return nil
}

func (b *EESTPayloadsBuilder) runFill(
	ctx context.Context,
	log logrus.FieldLogger,
	t *config.EESTPayloadTarget,
	fillerIP string,
	spec client.Spec,
	jwtPath, snapshotHash, eestRepoPath, eestSHA string,
) error {
	fillImage, err := b.ensureFillImage(ctx, log)
	if err != nil {
		return err
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
		"HOME":         "/tmp",
		"UV_CACHE_DIR": "/tmp/uv-cache",
		// Enable pytest's json report so we can read an authoritative
		// passed/failed tally for the build summary (the json-report plugin ships
		// in the fill image). fill-stateful continues through failures, so a
		// partial fill still records how many tests failed.
		//
		// --self-contained-html inlines the pytest-html report's CSS into
		// .meta/report_fill.html so it has no relative assets/ references. That
		// lets the UI serve it from a presigned S3 URL (or an iframe) with full
		// styling — a linked assets/style.css would resolve to an unsigned S3
		// URL and 403. EEST only sets htmlpath, not self_contained_html, so this
		// flag is honored.
		"PYTEST_ADDOPTS": "-o cache_dir=/tmp/.pytest_cache --self-contained-html --json-report --json-report-file=" + fillOutputPath + "/" + pytestReportFile,
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
		Image:       fillImage,
		Command:     args,
		Mounts:      mounts,
		NetworkName: EESTBuildNetwork,
		User:        currentUserSpec(),
		Env:         env,
		Labels:      b.labels(t),
	}

	tail := newTailBuffer(64 * 1024)
	out := io.MultiWriter(containerStream("BULD", "fill-stateful"), tail)

	log.WithField("argv", args).Info("Running fill-stateful")

	runErr := b.mgr.RunInitContainer(ctx, containerSpec, out, out)

	// Record fill counts (from EEST's report artifacts) for the build summary —
	// best-effort, and after a failed fill too, since fill-stateful continues
	// through failures and still writes the reports.
	if err := recordEESTFillResult(t, eestSHA); err != nil {
		log.WithError(err).Warn("Failed to record fill result")
	}

	if runErr != nil {
		return fmt.Errorf("running fill-stateful: %w (output tail: %s)", runErr, tail.String())
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

// fillerCommand builds the filler EL client's argv, dispatching on the
// configured filler_client. fill-stateful drives whichever client over the
// standard Engine API flow (eth_sendRawTransaction + engine_forkchoiceUpdated /
// getPayload / newPayload), so every client just needs HTTP RPC (with txpool),
// the Engine API (JWT), peering disabled, and the snapshot-boot workarounds the
// runner uses for the same client. config validation guarantees the client is
// one of the cases below.
func fillerCommand(t *config.EESTPayloadTarget, spec client.Spec) []string {
	switch t.FillerClient {
	case "besu":
		return fillerBesuCommand(t, spec)
	case "nethermind":
		return fillerNethermindCommand(t, spec)
	default:
		return fillerGethCommand(t, spec)
	}
}

// fillerReplayCommand builds the boot argv for a replay target. Replay needs
// only the engine API (newPayload/forkchoiceUpdated), not fill-stateful's
// testing namespace, so it uses the client's standard runner command
// (spec.DefaultCommand — correct for every client, incl. non-fillers like
// reth/ethrex) plus the genesis chain flag and any filler_extra_args.
func fillerReplayCommand(t *config.EESTPayloadTarget, spec client.Spec) []string {
	args := spec.DefaultCommand()

	if t.Genesis != "" && spec.GenesisFlag() != "" {
		args = append(args, spec.GenesisFlag()+spec.GenesisPath())
	}

	return overrideArgs(args, t.FillerExtraArgs)
}

// overrideArgs appends extra to base, first dropping any base arg whose flag is
// also set in extra (matched by the "--flag=" prefix). This lets filler_extra_args
// override the client's default command instead of duplicating a flag, which some
// clients (besu) reject. Mirrors the runner's instance extra_args handling.
func overrideArgs(base, extra []string) []string {
	prefixes := make([]string, 0, len(extra))

	for _, arg := range extra {
		if idx := strings.Index(arg, "="); idx != -1 {
			prefixes = append(prefixes, arg[:idx+1])
		}
	}

	out := make([]string, 0, len(base)+len(extra))

	for _, c := range base {
		override := false

		for _, p := range prefixes {
			if strings.HasPrefix(c, p) {
				override = true

				break
			}
		}

		if !override {
			out = append(out, c)
		}
	}

	return append(out, extra...)
}

// fillerBootEnv returns the container environment for a booted filler/replay
// client. The container runs as the host user, which has no home directory, so
// it sets a writable HOME + XDG cache under /tmp (reth aborts on boot otherwise,
// writing ~/.cache/reth/logs). The client's own default env is merged last so it
// can override.
func fillerBootEnv(spec client.Spec) map[string]string {
	env := map[string]string{
		"HOME":            "/tmp",
		"XDG_CACHE_HOME":  "/tmp/.cache",
		"XDG_DATA_HOME":   "/tmp/.local/share",
		"XDG_CONFIG_HOME": "/tmp/.config",
	}

	maps.Copy(env, spec.DefaultEnvironment())

	return env
}

// fillerGethCommand builds the geth argv for the filler client: the http API
// exposes the eth/net/web3/txpool/engine namespaces fill-stateful needs, archive
// gcmode keeps full state, and peering is disabled. spec supplies the
// in-container paths and ports.
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

	if t.Genesis != "" {
		args = append(args, spec.GenesisFlag()+spec.GenesisPath())
	}

	return append(args, t.FillerExtraArgs...)
}

// fillerBesuCommand builds the besu argv for the filler client. fill-stateful
// drives every filler over testing_buildBlockV1, which besu exposes behind the
// TESTING JSON-RPC namespace (added in besu-eth/besu#9838, in besu >= 26.6;
// without TESTING in --rpc-http-api besu answers -32604 "Method not enabled").
// Mirrors the runner besu command (pkg/client/besu.go) otherwise: the ETH/TXPOOL
// namespaces back eth_sendRawTransaction, DEBUG backs the per-test debug_setHead
// rewind, and --p2p-enabled=true lets besu's synchronizer register the snapshot
// head as in-sync — without it besu answers SYNCING to the fill tool's initial
// forkchoice_updated (--max-peers=0 + --discovery-enabled=false keep it
// isolated). The genesis file is required: besu reads chainId from
// --genesis-file at boot, not from the datadir.
func fillerBesuCommand(t *config.EESTPayloadTarget, spec client.Spec) []string {
	args := []string{
		"--data-path=" + spec.DataDir(),
		"--data-storage-format=BONSAI",
		// Trust the genesis state hash baked into the state-actor snapshot
		// instead of recomputing it from the empty chainspec alloc.
		"--genesis-state-hash-cache-enabled=true",
		"--sync-mode=FULL",
		"--p2p-enabled=true",
		"--max-peers=0",
		"--discovery-enabled=false",
		"--rpc-http-enabled=true",
		"--rpc-http-host=0.0.0.0",
		"--rpc-http-port=" + strconv.Itoa(spec.RPCPort()),
		// TESTING exposes testing_buildBlockV1 (fill-stateful's block builder).
		"--rpc-http-api=ETH,NET,WEB3,TXPOOL,DEBUG,ADMIN,MINER,TESTING",
		"--rpc-http-cors-origins=*",
		"--host-allowlist=*",
		"--Xhttp-timeout-seconds=660",
		"--engine-rpc-enabled=true",
		"--engine-jwt-secret=" + spec.JWTPath(),
		"--engine-rpc-port=" + strconv.Itoa(spec.EnginePort()),
		"--engine-host-allowlist=*",
		"--target-gas-limit=" + minerGasLimit,
	}

	if t.Genesis != "" {
		args = append(args, spec.GenesisFlag()+spec.GenesisPath())
	}

	return append(args, t.FillerExtraArgs...)
}

// fillerNethermindCommand builds the nethermind argv for the filler client.
// fill-stateful drives every filler over testing_buildBlockV1, so the HTTP RPC
// must expose the Testing module (without it nethermind answers -32604 "Method
// not enabled"); this needs a nethermind build that ships testing_buildBlockV1
// (e.g. nethermindeth/nethermind:testing_build_block_with_opcode_tracing — set
// as the target's filler_image). Module list, target gas limit and BaseDbPath
// mirror NethermindEth/gas-benchmarks' stateful generator. --Init.BaseDbPath
// points at the datadir so nethermind reads the state-actor snapshot written
// there (its BaseDbPath otherwise defaults to a nested per-network dir holding
// an empty state db). The genesis file is required: nethermind reads the chain
// config from --Init.ChainSpecPath.
func fillerNethermindCommand(t *config.EESTPayloadTarget, spec client.Spec) []string {
	args := []string{
		"--datadir=" + spec.DataDir(),
		"--Init.BaseDbPath=" + spec.DataDir(),
		"--config=none",
		"--Network.DiscoveryPort=0",
		"--Network.MaxActivePeers=0",
		"--Init.DiscoveryEnabled=false",
		"--Sync.MaxAttemptsToUpdatePivot=0",
		"--Network.ExternalIp=127.0.0.1",
		"--JsonRpc.Enabled=true",
		"--JsonRpc.Host=0.0.0.0",
		"--JsonRpc.Port=" + strconv.Itoa(spec.RPCPort()),
		// Testing exposes testing_buildBlockV1 (fill-stateful's block builder).
		"--JsonRpc.EnabledModules=Eth,Net,Web3,Admin,Debug,Trace,TxPool,Subscribe,Testing",
		"--JsonRpc.EngineEnabledModules=Net,Eth,Subscribe,Web3,Testing,Engine",
		"--JsonRpc.Timeout=600000",
		"--JsonRpc.JwtSecretFile=" + spec.JWTPath(),
		"--JsonRpc.EngineHost=0.0.0.0",
		"--JsonRpc.EnginePort=" + strconv.Itoa(spec.EnginePort()),
		"--Merge.TerminalTotalDifficulty=0",
		"--Blocks.TargetBlockGasLimit=" + minerGasLimit,
	}

	if t.Genesis != "" {
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

	// --extract-opcode-count traces each execution-phase block via
	// debug_traceBlockByHash with a custom JS tracer and records per-opcode
	// counts in the fixture's _info.metadata.opcode_counts (one entry per
	// engineNewPayloads block). Works with any filler
	// exposing debug_traceBlockByHash + JS tracer support (geth is validated).
	if t.ExtractOpcodeCount != nil && *t.ExtractOpcodeCount {
		args = append(args, "--extract-opcode-count")
	}

	if t.MaxGasPerTest != nil {
		args = append(args, fmt.Sprintf("--max-gas-per-test=%d", *t.MaxGasPerTest))
	}

	if t.RPCSeedKey != "" {
		args = append(args, "--rpc-seed-key="+t.RPCSeedKey)
	}

	// besu suggests a zero priority fee on a freshly-booted snapshot; pin a
	// non-zero tip so fill-stateful's session-fee check passes (see
	// besuDefaultPriorityFeeWei). geth derives a non-zero tip itself.
	if t.FillerClient == "besu" {
		args = append(args, "--default-max-priority-fee-per-gas="+besuDefaultPriorityFeeWei)
	}

	if t.AddressStubsFile != "" {
		args = append(args, "--address-stubs="+fillStubsPath)
	}

	args = append(args, t.Tests...)

	if t.Filter != "" {
		args = append(args, "-k", t.Filter)
	}

	if t.Marker != "" {
		args = append(args, "-m", t.Marker)
	}

	return args
}

// patchFillerGenesis reads the target's boot genesis, applies its
// genesis_fork_override / genesis_eip_override (mutually exclusive; validated),
// and writes the result to a temp file the filler container can bind-mount.
// Returns the temp path and a cleanup callback. It is the builder-side analogue
// of the runner's per-instance genesis patching.
func patchFillerGenesis(
	log logrus.FieldLogger, t *config.EESTPayloadTarget,
) (string, func(), error) {
	raw, err := os.ReadFile(t.Genesis)
	if err != nil {
		return "", nil, fmt.Errorf("reading genesis %q: %w", t.Genesis, err)
	}

	var patched []byte

	switch {
	case len(t.GenesisForkOverride) > 0:
		patched, err = genesis.ApplyForkOverrides(raw, t.GenesisForkOverride)
		if err != nil {
			return "", nil, fmt.Errorf("applying genesis_fork_override: %w", err)
		}

		log.WithField("forks", t.GenesisForkOverride).
			Info("Applied genesis fork-time overrides to filler genesis")
	case t.GenesisEIPOverride != nil:
		patched, err = genesis.ApplyEIPOverrides(
			raw, t.GenesisEIPOverride.Timestamp, t.GenesisEIPOverride.EIPs,
		)
		if err != nil {
			return "", nil, fmt.Errorf("applying genesis_eip_override: %w", err)
		}

		log.WithFields(logrus.Fields{
			"eips": t.GenesisEIPOverride.EIPs, "timestamp": t.GenesisEIPOverride.Timestamp,
		}).Info("Applied genesis EIP-time overrides to filler genesis")
	}

	f, err := os.CreateTemp(mountTempDir(), "benchmarkoor-eest-genesis-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp genesis file: %w", err)
	}

	path := f.Name()

	cleanup := func() { _ = os.Remove(path) }

	if _, err := f.Write(patched); err != nil {
		_ = f.Close()
		cleanup()

		return "", nil, fmt.Errorf("writing temp genesis file: %w", err)
	}

	if err := f.Close(); err != nil {
		cleanup()

		return "", nil, fmt.Errorf("closing temp genesis file: %w", err)
	}

	if err := os.Chmod(path, 0o644); err != nil {
		cleanup()

		return "", nil, fmt.Errorf("chmod temp genesis file: %w", err)
	}

	return path, cleanup, nil
}

// materializeAddressStubs serializes a target's inline address_stubs map to a
// temp JSON file readable by the container UID (0644) and returns its path plus
// a cleanup callback. The file mirrors the on-disk address_stubs_file format so
// downstream mount + --address-stubs handling is identical.
func materializeAddressStubs(
	log logrus.FieldLogger, t *config.EESTPayloadTarget,
) (string, func(), error) {
	data, err := json.MarshalIndent(t.AddressStubs, "", "  ")
	if err != nil {
		return "", nil, fmt.Errorf("marshaling address_stubs: %w", err)
	}

	f, err := os.CreateTemp(mountTempDir(), "benchmarkoor-eest-stubs-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp stubs file: %w", err)
	}

	path := f.Name()

	cleanup := func() { _ = os.Remove(path) }

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		cleanup()

		return "", nil, fmt.Errorf("writing temp stubs file: %w", err)
	}

	if err := f.Close(); err != nil {
		cleanup()

		return "", nil, fmt.Errorf("closing temp stubs file: %w", err)
	}

	if err := os.Chmod(path, 0o644); err != nil {
		cleanup()

		return "", nil, fmt.Errorf("chmod temp stubs file: %w", err)
	}

	log.WithField("stubs", len(t.AddressStubs)).Info("Materialized inline address_stubs to temp file")

	return path, cleanup, nil
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
