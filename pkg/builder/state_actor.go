package builder

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
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

	log := b.log.WithFields(logrus.Fields{
		"target":     target.EffectiveName(),
		"client":     target.Client,
		"output_dir": target.OutputDir,
		"image":      image,
	})

	// The CLI `--force` flag and per-target `force: true` both bypass the
	// skip-on-populated check, wipe the existing output_dir, and forward
	// `--force` to state-actor.
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

	if err := prepareOutputDir(target.OutputDir, force); err != nil {
		return false, err
	}

	// Resolve which source state-actor will see: an absolute host path to
	// a spec file (real or temp) when this target inherits from the
	// top-level spec/spec_file, or "" when the target opted out via its
	// own target_size.
	specPath, cleanupSpec, err := b.resolveSpecPath()
	if err != nil {
		return false, err
	}

	if cleanupSpec != nil {
		defer cleanupSpec()
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

	stdout := io.MultiWriter(logWriter(log, logrus.InfoLevel), tail)
	stderr := io.MultiWriter(logWriter(log, logrus.InfoLevel), tail)

	log.WithField("argv", args).Info("Running state-actor")

	if err := b.mgr.RunInitContainer(ctx, spec, stdout, stderr); err != nil {
		return false, fmt.Errorf("running state-actor: %w (output tail: %s)",
			err, tail.String())
	}

	log.Info("Build completed")

	return false, nil
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
		f, err := os.CreateTemp("", "benchmarkoor-state-actor-spec-*.yaml")
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
