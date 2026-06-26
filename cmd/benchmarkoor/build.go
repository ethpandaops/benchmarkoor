package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sort"
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
	buildTargetFilter       []string
	buildStateActorTargets  []string
	buildEESTPayloadTargets []string
	buildForce              bool
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
		"Only build targets whose name matches, across all builders (comma-separated or repeated)")
	buildCmd.Flags().StringSliceVar(&buildStateActorTargets, "limit-state-actor-target", nil,
		"Only build builder.state_actor targets whose name matches (comma-separated or repeated)")
	buildCmd.Flags().StringSliceVar(&buildEESTPayloadTargets, "limit-eest-payload-target", nil,
		"Only build builder.eest_payloads targets whose name matches (comma-separated or repeated)")
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
	perBuilder := map[string]builderFilter{
		builder.StateActorBuilderName:   {flag: "--limit-state-actor-target", values: buildStateActorTargets},
		builder.EESTPayloadsBuilderName: {flag: "--limit-eest-payload-target", values: buildEESTPayloadTargets},
	}

	targets, err := selectTargets(builders, buildTargetFilter, perBuilder)
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

// builderFilter is a per-builder `--limit-<builder>-target` filter: the wanted
// target names plus the flag name, used for a precise "matched nothing" error.
type builderFilter struct {
	flag   string
	values []string
}

// selectTargets flattens all builders' targets in declaration order and filters
// them. A target is selected when it passes both the global `--target` filter
// and the per-builder filter for the builder that owns it (keyed by Builder
// Name()). An empty filter imposes no restriction. Unmatched filter values
// produce an error so typos surface immediately — the global filter is checked
// against every target name, each per-builder filter against only that builder's
// target names.
func selectTargets(
	builders []builder.Builder, global []string, perBuilder map[string]builderFilter,
) ([]selectedTarget, error) {
	globalWanted := nameSet(global)

	var out []selectedTarget

	for _, b := range builders {
		bf := perBuilder[b.Name()]
		builderWanted := nameSet(bf.values)

		for _, info := range b.Targets() {
			if len(globalWanted) > 0 && !globalWanted[info.Name] {
				continue
			}

			if len(builderWanted) > 0 && !builderWanted[info.Name] {
				continue
			}

			out = append(out, selectedTarget{builder: b, info: info})
		}
	}

	if err := checkFiltersMatched(builders, globalWanted, perBuilder); err != nil {
		return nil, err
	}

	return out, nil
}

// nameSet builds a lookup set from filter values, trimming blanks.
func nameSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))

	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			set[v] = true
		}
	}

	return set
}

// checkFiltersMatched verifies every filter value names an existing target,
// returning an error listing any that matched nothing.
func checkFiltersMatched(
	builders []builder.Builder, global map[string]bool, perBuilder map[string]builderFilter,
) error {
	allNames := make(map[string]bool)
	namesByBuilder := make(map[string]map[string]bool, len(builders))

	for _, b := range builders {
		names := make(map[string]bool)

		for _, info := range b.Targets() {
			names[info.Name] = true
			allNames[info.Name] = true
		}

		namesByBuilder[b.Name()] = names
	}

	if missing := unmatched(global, allNames); len(missing) > 0 {
		return errors.New("--target filter matched no targets: " + strings.Join(missing, ", "))
	}

	for builderName, bf := range perBuilder {
		if missing := unmatched(nameSet(bf.values), namesByBuilder[builderName]); len(missing) > 0 {
			return fmt.Errorf(
				"%s matched no %s targets: %s", bf.flag, builderName, strings.Join(missing, ", "),
			)
		}
	}

	return nil
}

// unmatched returns the wanted names absent from available, sorted for a stable
// error message.
func unmatched(wanted, available map[string]bool) []string {
	var missing []string

	for name := range wanted {
		if !available[name] {
			missing = append(missing, name)
		}
	}

	sort.Strings(missing)

	return missing
}
