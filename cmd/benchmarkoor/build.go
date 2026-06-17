package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ethpandaops/benchmarkoor/pkg/builder"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/ethpandaops/benchmarkoor/pkg/podman"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	buildTargetFilter []string
	buildForce        bool
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build datadirs and fixtures declared under the builder.* config blocks",
	Long: `Run each configured builder:

  - builder.state_actor   materialises pre-populated client datadirs by invoking
                          state-actor (https://github.com/ethereum/state-actor).
  - builder.eest_payloads generates stateful EEST benchmark fixtures by running
                          fill-stateful against a filler client booted on a snapshot.

Builds are decoupled from "benchmarkoor run": this command produces artifacts on
disk that subsequent runs consume via their normal datadir.* / test source providers.
Builders run in declaration order (state_actor before eest_payloads) so a fixture
build can consume a datadir produced earlier in the same invocation.`,
	RunE: runBuild,
}

func init() {
	rootCmd.AddCommand(buildCmd)
	buildCmd.Flags().StringSliceVar(&buildTargetFilter, "target", nil,
		"Only build targets whose name matches (comma-separated or repeated)")
	buildCmd.Flags().BoolVar(&buildForce, "force", false,
		"Remove each target's output_dir before building")
}

func runBuild(_ *cobra.Command, _ []string) error {
	// Match the `benchmarkoor run` log format (🔵 prefix); container output is
	// streamed in the same 🟣 client-log style for a consistent look.
	log.SetFormatter(&consistentFormatter{prefix: "🔵"})

	if len(cfgFiles) == 0 {
		return fmt.Errorf("config file is required (use --config)")
	}

	cfg, err := config.Load(cfgFiles...)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if err := cfg.ValidateBuilder(); err != nil {
		return fmt.Errorf("validating config: %w", err)
	}

	if cfg.Builder == nil ||
		(cfg.Builder.StateActor == nil && cfg.Builder.EESTPayloads == nil) {
		return fmt.Errorf("no builders configured; nothing to build")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	installSignalHandler(cancel)

	builders, stop, err := buildBuilders(ctx, cfg)
	if err != nil {
		return err
	}

	defer stop()

	return runBuilders(ctx, builders)
}

// installSignalHandler cancels the context on the first SIGINT/SIGTERM and
// force-exits on the second.
func installSignalHandler(cancel context.CancelFunc) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.WithField("signal", sig).Info("Received shutdown signal, cancelling build")
		cancel()

		sig = <-sigCh
		log.WithField("signal", sig).Fatal("Received second signal, forcing exit")
	}()
}

// buildBuilders constructs every configured builder, creating and starting a
// container manager per distinct runtime. The returned stop func stops all
// managers and must be deferred by the caller.
func buildBuilders(ctx context.Context, cfg *config.Config) ([]builder.Builder, func(), error) {
	managers := make(map[string]docker.ContainerManager, 2)

	stop := func() {
		for _, mgr := range managers {
			if err := mgr.Stop(); err != nil {
				log.WithError(err).Warn("Failed to stop container manager")
			}
		}
	}

	getManager := func(runtime string) (docker.ContainerManager, error) {
		if mgr, ok := managers[runtime]; ok {
			return mgr, nil
		}

		mgr, err := newContainerManager(runtime)
		if err != nil {
			return nil, err
		}

		if err := mgr.Start(ctx); err != nil {
			return nil, fmt.Errorf("starting %s container manager: %w", runtime, err)
		}

		managers[runtime] = mgr

		return mgr, nil
	}

	var builders []builder.Builder

	if cfg.Builder.StateActor != nil {
		runtime := cfg.GetStateActorContainerRuntime()

		mgr, err := getManager(runtime)
		if err != nil {
			stop()

			return nil, nil, err
		}

		builders = append(builders, builder.NewStateActorBuilder(log, cfg.Builder.StateActor, runtime, mgr))
	}

	if cfg.Builder.EESTPayloads != nil {
		runtime := cfg.GetEESTPayloadsContainerRuntime()

		mgr, err := getManager(runtime)
		if err != nil {
			stop()

			return nil, nil, err
		}

		cacheDir, err := cfg.ResolveCacheDir()
		if err != nil {
			stop()

			return nil, nil, err
		}

		builders = append(builders,
			builder.NewEESTPayloadsBuilder(log, cfg.Builder.EESTPayloads, runtime, mgr, cacheDir))
	}

	return builders, stop, nil
}

// newContainerManager creates a container manager for the given runtime.
func newContainerManager(runtime string) (docker.ContainerManager, error) {
	switch runtime {
	case "podman":
		return podman.NewManager(log)
	default:
		return docker.NewManager(log)
	}
}

// buildResult captures the outcome of a single target build.
type buildResult struct {
	builder   string
	name      string
	client    string
	outputDir string
	skipped   bool
	err       error
}

// runBuilders selects and builds the requested targets across all builders,
// preserving declaration order, then prints a summary.
func runBuilders(ctx context.Context, builders []builder.Builder) error {
	targets, err := selectTargets(builders, buildTargetFilter)
	if err != nil {
		return err
	}

	results := make([]buildResult, 0, len(targets))

	for _, sel := range targets {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		log.WithFields(logrus.Fields{
			"target":     sel.info.Name,
			"client":     sel.info.Client,
			"output_dir": sel.info.OutputDir,
		}).Info("Building target")

		skipped, buildErr := sel.builder.Build(ctx, sel.info.Name, builder.BuildOptions{Force: buildForce})

		results = append(results, buildResult{
			builder:   sel.builder.Name(),
			name:      sel.info.Name,
			client:    sel.info.Client,
			outputDir: sel.info.OutputDir,
			skipped:   skipped,
			err:       buildErr,
		})

		if buildErr != nil {
			log.WithError(buildErr).WithField("target", sel.info.Name).Error("Build failed")
		}
	}

	return summarise(results)
}

// summarise logs the per-target outcome grouped under each builder and returns
// an error if any target failed. Builders are emitted in first-seen
// (declaration) order so state_actor appears before eest_payloads.
func summarise(results []buildResult) error {
	var failed []string

	var order []string

	byBuilder := make(map[string][]buildResult, 2)

	for _, r := range results {
		if _, seen := byBuilder[r.builder]; !seen {
			order = append(order, r.builder)
		}

		byBuilder[r.builder] = append(byBuilder[r.builder], r)

		if r.err != nil {
			failed = append(failed, r.name)
		}
	}

	for _, b := range order {
		log.Infof("Build summary [%s]:", b)

		for _, r := range byBuilder[b] {
			var status string

			switch {
			case r.err != nil:
				status = "ERR "
			case r.skipped:
				status = "SKIP"
			default:
				status = "OK  "
			}

			log.WithFields(logrus.Fields{
				"builder":    r.builder,
				"target":     r.name,
				"client":     r.client,
				"output_dir": r.outputDir,
			}).Infof("  %s %s", status, r.name)
		}
	}

	if len(failed) > 0 {
		return fmt.Errorf("%d target(s) failed: %s", len(failed), strings.Join(failed, ", "))
	}

	return nil
}

// selectedTarget pairs a target with the builder that owns it.
type selectedTarget struct {
	builder builder.Builder
	info    builder.TargetInfo
}

// selectTargets flattens all builders' targets in declaration order and
// filters them by the names in filter. An empty filter returns every target.
// Unmatched filter values produce an error so typos surface immediately.
func selectTargets(builders []builder.Builder, filter []string) ([]selectedTarget, error) {
	wanted := make(map[string]bool, len(filter))

	for _, f := range filter {
		if f = strings.TrimSpace(f); f != "" {
			wanted[f] = true
		}
	}

	var out []selectedTarget

	matched := make(map[string]bool, len(wanted))

	for _, b := range builders {
		for _, info := range b.Targets() {
			if len(wanted) > 0 && !wanted[info.Name] {
				continue
			}

			out = append(out, selectedTarget{builder: b, info: info})
			matched[info.Name] = true
		}
	}

	var missing []string

	for name := range wanted {
		if !matched[name] {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		return nil, errors.New("--target filter matched no targets: " + strings.Join(missing, ", "))
	}

	return out, nil
}
