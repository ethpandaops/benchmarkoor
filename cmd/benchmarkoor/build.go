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
	Short: "Build client datadirs declared in builder.state_actor.targets",
	Long: `Materialise each datadir declared under builder.state_actor.targets
by invoking state-actor (https://github.com/ethereum/state-actor) via the
configured container runtime. Builds are decoupled from "benchmarkoor run":
this command produces datadirs on disk that subsequent runs consume via
their normal datadir.* providers.`,
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

	if cfg.Builder == nil || cfg.Builder.StateActor == nil || len(cfg.Builder.StateActor.Targets) == 0 {
		return fmt.Errorf("builder.state_actor.targets is empty or unset; nothing to build")
	}

	targets, err := selectTargets(cfg.Builder.StateActor.Targets, buildTargetFilter)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.WithField("signal", sig).Info("Received shutdown signal, cancelling build")
		cancel()

		sig = <-sigCh
		log.WithField("signal", sig).Fatal("Received second signal, forcing exit")
	}()

	runtime := cfg.GetStateActorContainerRuntime()

	var mgr docker.ContainerManager

	switch runtime {
	case "podman":
		mgr, err = podman.NewManager(log)
	default:
		mgr, err = docker.NewManager(log)
	}

	if err != nil {
		return fmt.Errorf("creating container manager: %w", err)
	}

	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("starting container manager: %w", err)
	}

	defer func() {
		if err := mgr.Stop(); err != nil {
			log.WithError(err).Warn("Failed to stop container manager")
		}
	}()

	b := builder.NewStateActorBuilder(log, cfg.Builder.StateActor, runtime, mgr)

	type result struct {
		name      string
		client    string
		outputDir string
		skipped   bool
		err       error
	}

	results := make([]result, 0, len(targets))

	for _, t := range targets {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		log.WithFields(logrus.Fields{
			"target":     t.EffectiveName(),
			"client":     t.Client,
			"output_dir": t.OutputDir,
		}).Info("Building target")

		skipped, buildErr := b.Build(ctx, t.EffectiveName(), builder.BuildOptions{Force: buildForce})

		results = append(results, result{
			name:      t.EffectiveName(),
			client:    t.Client,
			outputDir: t.OutputDir,
			skipped:   skipped,
			err:       buildErr,
		})

		if buildErr != nil {
			log.WithError(buildErr).WithField("target", t.EffectiveName()).Error("Build failed")
		}
	}

	// Summary.
	var failed []string

	log.Info("Build summary:")

	for _, r := range results {
		var status string

		switch {
		case r.err != nil:
			status = "ERR "
			failed = append(failed, r.name)
		case r.skipped:
			status = "SKIP"
		default:
			status = "OK  "
		}

		log.WithFields(logrus.Fields{
			"target":     r.name,
			"client":     r.client,
			"output_dir": r.outputDir,
		}).Infof("  %s %s", status, r.name)
	}

	if len(failed) > 0 {
		return fmt.Errorf("%d target(s) failed: %s",
			len(failed), strings.Join(failed, ", "))
	}

	return nil
}

// selectTargets filters `all` by the names in `filter`, preserving the
// order targets were declared in. An empty filter returns all targets.
// Unmatched filter values produce an error so typos surface immediately.
func selectTargets(all []config.StateActorTarget, filter []string) ([]config.StateActorTarget, error) {
	if len(filter) == 0 {
		return all, nil
	}

	wanted := make(map[string]bool, len(filter))
	for _, f := range filter {
		f = strings.TrimSpace(f)
		if f != "" {
			wanted[f] = true
		}
	}

	out := make([]config.StateActorTarget, 0, len(all))
	matched := make(map[string]bool, len(wanted))

	for _, t := range all {
		name := t.EffectiveName()
		if wanted[name] {
			out = append(out, t)
			matched[name] = true
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
