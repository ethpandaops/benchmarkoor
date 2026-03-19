package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/ethpandaops/benchmarkoor/pkg/generate"
	"github.com/ethpandaops/benchmarkoor/pkg/podman"
	"github.com/spf13/cobra"
)

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate EEST test fixtures",
	Long: `Generate EEST test fixtures by running execution-specs' execute remote
against an EL client through a reverse proxy that captures Engine API payloads.

The generation client must support testing_buildBlockV1.`,
	RunE: runGenerate,
}

func init() {
	rootCmd.AddCommand(generateCmd)
}

func runGenerate(cmd *cobra.Command, args []string) error {
	if len(cfgFiles) == 0 {
		return fmt.Errorf("config file is required (use --config)")
	}

	// Load configuration.
	cfg, err := config.Load(cfgFiles...)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if cfg.Generate == nil {
		return fmt.Errorf("generate section is required in config")
	}

	// Validate generate config.
	if err := cfg.ValidateGenerate(); err != nil {
		return fmt.Errorf("validating config: %w", err)
	}

	// Setup context with signal handling.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.WithField("signal", sig).Info(
			"Received shutdown signal, shutting down gracefully...",
		)

		cancel()

		sig = <-sigCh
		log.WithField("signal", sig).Fatal("Received second signal, forcing exit")
	}()

	// Create container manager.
	var containerMgr docker.ContainerManager

	runtime := cfg.Generate.ContainerRuntime
	if runtime == "" {
		runtime = cfg.GetContainerRuntime()
	}

	switch runtime {
	case "podman":
		containerMgr, err = podman.NewManager(log)
	default:
		containerMgr, err = docker.NewManager(log)
	}

	if err != nil {
		return fmt.Errorf("creating container manager: %w", err)
	}

	if err := containerMgr.Start(ctx); err != nil {
		return fmt.Errorf("starting container manager: %w", err)
	}

	defer func() {
		if err := containerMgr.Stop(); err != nil {
			log.WithError(err).Warn("Failed to stop container manager")
		}
	}()

	// Create client registry.
	registry := client.NewRegistry()

	// Create and run generator.
	gen := generate.NewGenerator(log, cfg.Generate, containerMgr, registry)

	if err := gen.Run(ctx); err != nil {
		return fmt.Errorf("generate: %w", err)
	}

	return nil
}
