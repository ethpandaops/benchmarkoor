package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/datadir"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/sirupsen/logrus"
)

// StateActorBuilderName is the value of the benchmarkoor.builder label
// set on every state-actor build container, and the value returned by
// Name().
const StateActorBuilderName = "state-actor"

// gethDBSuffix is the path state-actor expects appended to a geth
// datadir for the --db flag. Other clients use the datadir root as-is.
const gethDBSuffix = "/geth/chaindata"

// StateActorBuilder runs state-actor for each declared target via the
// supplied docker/podman ContainerManager.
type StateActorBuilder struct {
	log     logrus.FieldLogger
	cfg     *config.StateActorConfig
	runtime string
	mgr     docker.ContainerManager
}

// NewStateActorBuilder constructs a builder bound to a specific container
// manager. The caller is expected to have Start()'d the manager and to
// Stop() it after the last Build() call.
func NewStateActorBuilder(
	log logrus.FieldLogger,
	cfg *config.StateActorConfig,
	runtime string,
	mgr docker.ContainerManager,
) *StateActorBuilder {
	return &StateActorBuilder{
		log:     log.WithField("component", "builder.state_actor"),
		cfg:     cfg,
		runtime: runtime,
		mgr:     mgr,
	}
}

// Name implements Builder.
func (b *StateActorBuilder) Name() string {
	return StateActorBuilderName
}

// Targets implements Builder.
func (b *StateActorBuilder) Targets() []TargetInfo {
	out := make([]TargetInfo, 0, len(b.cfg.Targets))

	for i := range b.cfg.Targets {
		t := &b.cfg.Targets[i]
		out = append(out, TargetInfo{
			Name:      t.EffectiveName(),
			Client:    t.Client,
			OutputDir: t.OutputDir,
		})
	}

	return out
}

// Build implements Builder.
func (b *StateActorBuilder) Build(ctx context.Context, name string, opts BuildOptions) (bool, error) {
	idx := b.findTargetIndex(name)
	if idx < 0 {
		return false, fmt.Errorf("no target named %q", name)
	}

	// Resolve so global defaults from builder.state_actor.config are
	// merged into the per-target view exactly once, then handed to every
	// downstream helper.
	resolved := b.cfg.ResolveTarget(idx)
	target := &resolved

	image := b.cfg.ImageFor(target.Client)
	if image == "" {
		return false, fmt.Errorf("no image configured for client %q", target.Client)
	}

	// Keep only the target on the per-line logger; client/output_dir are already
	// in build.go's "Building target" header and image is logged on the
	// "Running state-actor" line, so they don't repeat on every streamed line.
	log := b.log.WithField("target", target.EffectiveName())

	// When output_dir lives under a schelk mount, make sure the scratch is
	// mounted before anything inspects or writes it: the datadir is materialised
	// onto the mount, and the skip/diff checks below must read the mounted
	// content, not an empty mount point. Non-schelk output_dirs are untouched.
	schelkMount, isSchelk, err := datadir.SchelkDir(target.OutputDir)
	if err != nil {
		return false, fmt.Errorf("checking schelk state for %q: %w", target.OutputDir, err)
	}

	if isSchelk {
		log.WithField("mount_point", schelkMount).Info("output_dir is under a schelk mount; ensuring mounted")

		if err := datadir.EnsureSchelkMounted(ctx, log); err != nil {
			return false, fmt.Errorf("ensuring schelk mount: %w", err)
		}
	}

	// The CLI `--force` flag and per-target `force: true` both bypass the
	// skip-on-populated check, wipe the existing output_dir, and forward
	// `--force` to state-actor.
	force := opts.Force || target.Force

	// Fast path: a populated output_dir with neither --force nor
	// --rebuild-on-diff skips without resolving the spec (the original cheap
	// skip). Diff detection and building both need the spec, resolved below.
	if !force && !opts.RebuildOnDiff {
		populated, err := isPopulated(target.OutputDir)
		if err != nil {
			return false, err
		}

		if populated {
			log.Info("Skipping build: output_dir already populated " +
				"(pass --force, --rebuild-on-diff, or set force: true on the target to rebuild)")

			return true, nil
		}
	}

	// Resolve the spec: an absolute host path to a spec file (real or temp) when
	// this target inherits from the top-level spec/spec_file, or "" when the
	// target opted out via its own target_size. Needed both to run the build and
	// to fingerprint the spec content.
	specPath, cleanupSpec, err := b.resolveSpecPath()
	if err != nil {
		return false, err
	}

	if cleanupSpec != nil {
		defer cleanupSpec()
	}

	inputs, err := stateActorFingerprintInputs(target, image, specPath)
	if err != nil {
		return false, err
	}

	if !force && opts.RebuildOnDiff {
		populated, err := isPopulated(target.OutputDir)
		if err != nil {
			return false, err
		}

		if populated {
			dec, err := decideRebuild(target.OutputDir, inputs)
			if err != nil {
				return false, err
			}

			if !dec.rebuild {
				log.Infof("Skipping build: output_dir already populated (%s)", dec.reason)

				return true, nil
			}

			log.WithField("changed", dec.changed).Info("Config changed since last build; rebuilding")

			force = true
		}
	}

	if err := prepareOutputDir(target.OutputDir, force); err != nil {
		return false, err
	}

	mounts, err := b.buildMounts(target, specPath)
	if err != nil {
		return false, err
	}

	args := buildArgs(target, specPath)

	if err := b.mgr.PullImage(ctx, image, b.cfg.PullPolicy); err != nil {
		return false, fmt.Errorf("pulling image %q: %w", image, err)
	}

	suffix, err := randSuffix()
	if err != nil {
		return false, fmt.Errorf("generating container name suffix: %w", err)
	}

	spec := &docker.ContainerSpec{
		Name:    fmt.Sprintf("benchmarkoor-build-state-actor-%s-%s", target.Client, suffix),
		Image:   image,
		Command: args,
		Mounts:  mounts,
		// Run as the invoking host user so the output datadir is owned by that
		// user (not root) and is readable when a later step copies it.
		User: currentUserSpec(),
		Labels: map[string]string{
			"benchmarkoor.managed-by": "benchmarkoor",
			"benchmarkoor.builder":    StateActorBuilderName,
			"benchmarkoor.client":     target.Client,
			"benchmarkoor.target":     target.EffectiveName(),
			"benchmarkoor.output-dir": target.OutputDir,
		},
	}

	// Capture the tail of the container's combined output so a non-zero
	// exit yields a useful error without spamming the whole log.
	tail := newTailBuffer(64 * 1024)

	stdout := io.MultiWriter(containerStream("BULD", "state-actor"), tail)
	stderr := io.MultiWriter(containerStream("BULD", "state-actor"), tail)

	log.WithFields(logrus.Fields{"image": image, "argv": args}).Info("Running state-actor")

	if err := b.mgr.RunInitContainer(ctx, spec, stdout, stderr); err != nil {
		return false, fmt.Errorf("running state-actor: %w (output tail: %s)",
			err, tail.String())
	}

	// Record the docker image (name + resolved sha256 digest) into the manifest
	// so it travels with the state-actor metadata and surfaces in the build
	// summary. Best-effort: a failure here must not fail an otherwise-good build.
	if err := b.recordManifestImage(ctx, log, target.OutputDir, image); err != nil {
		log.WithError(err).Warn("Failed to record docker image in state-actor manifest")
	}

	// Record the config fingerprint so a later --rebuild-on-diff run can tell
	// whether the datadir is stale. Best-effort: a failure here must not fail an
	// otherwise-successful build.
	if err := writeBuildSidecar(target.OutputDir, StateActorBuilderName, inputs); err != nil {
		log.WithError(err).Warn("Failed to write build fingerprint sidecar")
	}

	// The datadir just changed (a build ran), so persist it as the new schelk
	// baseline. This reaches here only when a build actually happened — the skip
	// paths return earlier — so an unchanged, skipped target is never promoted.
	// Failure is fatal: leaving the mount ahead of the baseline would let a
	// later `schelk recover` silently revert to the stale datadir.
	if isSchelk {
		log.Info("Persisting datadir as the new schelk baseline (`schelk promote`)")

		if err := datadir.SchelkPromote(ctx, log); err != nil {
			return false, fmt.Errorf("schelk promote: %w", err)
		}
	}

	log.Info("Build completed")

	return false, nil
}

// stateActorManifestFile is the metadata JSON the external state-actor binary
// writes into each datadir.
const stateActorManifestFile = "state-actor-manifest.json"

// recordManifestImage augments the state-actor manifest with the docker image
// used to produce the datadir and its resolved sha256 digest, under a
// benchmarkoor-namespaced key, so the image travels with the state-actor
// metadata (and surfaces in the build summary).
func (b *StateActorBuilder) recordManifestImage(ctx context.Context, log logrus.FieldLogger, outputDir, image string) error {
	digest, err := b.mgr.GetImageDigest(ctx, image)
	if err != nil {
		log.WithError(err).Debug("Could not resolve state-actor image digest")

		digest = ""
	}

	path := filepath.Join(outputDir, stateActorManifestFile)

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading manifest: %w", err)
	}

	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parsing manifest: %w", err)
	}

	manifest["benchmarkoor"] = map[string]any{
		"image":        image,
		"image_digest": digest,
	}

	out, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling manifest: %w", err)
	}

	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}

	return nil
}

// findTargetIndex returns the index of the first target whose
// EffectiveName matches `name`. Returns -1 when nothing matches.
func (b *StateActorBuilder) findTargetIndex(name string) int {
	for i := range b.cfg.Targets {
		if b.cfg.Targets[i].EffectiveName() == name {
			return i
		}
	}

	return -1
}

// buildMounts assembles the bind mounts state-actor needs: an identity
// mount of the output_dir (RW) and, when a spec path is resolved, a
// read-only mount of that file at the same path inside the container so
// the in-container --spec arg resolves transparently.
func (b *StateActorBuilder) buildMounts(target *config.StateActorTarget, specPath string) ([]docker.Mount, error) {
	mounts := []docker.Mount{{
		Source: target.OutputDir,
		Target: target.OutputDir,
		Type:   "bind",
	}}

	if specPath != "" {
		mounts = append(mounts, docker.Mount{
			Source:   specPath,
			Target:   specPath,
			Type:     "bind",
			ReadOnly: true,
		})
	}

	return mounts, nil
}

// resolveSpecPath determines the absolute host path of the spec file
// state-actor should consume for this target. Returns "" when no spec
// is configured (the target then runs with --target-size only). When
// the top-level config carries inline spec YAML, it is materialised to
// a temp file and the cleanup callback removes it. spec + target_size
// are complementary — both flags can be passed together so state-actor
// fills any headroom past the spec's projected cost.
func (b *StateActorBuilder) resolveSpecPath() (string, func(), error) {
	kind, value := b.cfg.ResolveSpec()
	switch kind {
	case config.StateActorSpecNone:
		return "", nil, nil
	case config.StateActorSpecFile:
		abs, err := filepath.Abs(value)
		if err != nil {
			return "", nil, fmt.Errorf("resolving spec_file path %q: %w", value, err)
		}

		if _, err := os.Stat(abs); err != nil {
			return "", nil, fmt.Errorf("spec_file: %w", err)
		}

		return abs, nil, nil
	case config.StateActorSpecInline:
		f, err := os.CreateTemp(mountTempDir(), "benchmarkoor-state-actor-spec-*.yaml")
		if err != nil {
			return "", nil, fmt.Errorf("creating temp spec file: %w", err)
		}

		path := f.Name()

		if _, err := f.WriteString(value); err != nil {
			_ = f.Close()
			_ = os.Remove(path)

			return "", nil, fmt.Errorf("writing temp spec file: %w", err)
		}

		if err := f.Close(); err != nil {
			_ = os.Remove(path)

			return "", nil, fmt.Errorf("closing temp spec file: %w", err)
		}

		// Allow the container UID to read the file (we don't know what UID
		// the state-actor image uses; 0644 is the broadest safe default).
		if err := os.Chmod(path, 0o644); err != nil {
			_ = os.Remove(path)

			return "", nil, fmt.Errorf("chmod temp spec file: %w", err)
		}

		cleanup := func() {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				b.log.WithError(err).WithField("path", path).
					Warn("Failed to remove temp spec file")
			}
		}

		return path, cleanup, nil
	}

	return "", nil, fmt.Errorf("internal: unhandled spec kind %v", kind)
}

// buildArgs translates a target into the argv state-actor expects. Only
// flags whose value differs from state-actor's default are emitted —
// pointer fields stay nil when the user did not set them so we never
// forward, say, `--chain-id=0` when the user wanted the default 1337.
// specPath, when non-empty, is the host (= container) path to the
// state-actor spec file resolved from the top-level spec/spec_file.
//
// The CLI `--force` and per-target `force` flags are intentionally not
// forwarded — state-actor has no `--force` flag of its own. Wiping the
// output_dir before the run (done in Build via prepareOutputDir) is
// what the user-facing flag actually does.
func buildArgs(target *config.StateActorTarget, specPath string) []string {
	args := []string{
		"--db=" + dbPath(target),
		"--client=" + target.Client,
	}

	if target.TargetSize != "" {
		args = append(args, "--target-size="+target.TargetSize)
	}

	if specPath != "" {
		args = append(args, "--spec="+specPath)
	}

	if target.Seed != nil {
		args = append(args, fmt.Sprintf("--seed=%d", *target.Seed))
	}

	if target.Fork != "" {
		args = append(args, "--fork="+target.Fork)
	}

	if target.ChainID != nil {
		args = append(args, fmt.Sprintf("--chain-id=%d", *target.ChainID))
	}

	if target.GasLimit != nil {
		args = append(args, fmt.Sprintf("--gas-limit=%d", *target.GasLimit))
	}

	if target.Timestamp != nil {
		args = append(args, fmt.Sprintf("--timestamp=%d", *target.Timestamp))
	}

	if target.ExtraData != "" {
		args = append(args, "--extra-data="+target.ExtraData)
	}

	if target.Archive != nil && *target.Archive {
		args = append(args, "--archive")
	}

	if target.BinaryTrie != nil && *target.BinaryTrie {
		args = append(args, "--binary-trie")
	}

	if target.GroupDepth != nil {
		args = append(args, fmt.Sprintf("--group-depth=%d", *target.GroupDepth))
	}

	return args
}

// stateActorFingerprintInputs builds the canonical fingerprint of a state-actor
// target's output-affecting config: the exact argv state-actor is invoked with
// (minus the volatile --db path), the resolved image, and the spec content
// hash. Deriving the args from buildArgs keeps the fingerprint in lockstep with
// whatever actually determines the datadir.
func stateActorFingerprintInputs(target *config.StateActorTarget, image, specPath string) (fingerprintInputs, error) {
	raw := buildArgs(target, "")
	args := make([]string, 0, len(raw))

	for _, a := range raw {
		if strings.HasPrefix(a, "--db=") {
			continue
		}

		args = append(args, a)
	}

	specHash, err := sha256File(specPath)
	if err != nil {
		return nil, err
	}

	return fingerprintInputs{
		"args":        args,
		"image":       image,
		"spec_sha256": specHash,
	}, nil
}

// dbPath returns the value for state-actor's --db flag. Geth requires
// the path end at <datadir>/geth/chaindata; everything else takes the
// datadir root directly.
func dbPath(target *config.StateActorTarget) string {
	if target.Client == "geth" {
		return target.OutputDir + gethDBSuffix
	}

	return target.OutputDir
}

// randSuffix, logWriter/lineLogger and tailBuffer live in util.go — shared
// across builders.
