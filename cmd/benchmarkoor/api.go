package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ethpandaops/benchmarkoor/pkg/api"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/spf13/cobra"
)

var apiCmd = &cobra.Command{
	Use:   "api",
	Short: "Start the API server",
	Long:  `Start the benchmarkoor API server for authentication and user management.`,
	RunE:  runAPI,
}

func init() {
	rootCmd.AddCommand(apiCmd)
}

func runAPI(cmd *cobra.Command, args []string) error {
	if len(cfgFiles) == 0 {
		return fmt.Errorf("config file is required (use --config)")
	}

	cfg, err := config.Load(cfgFiles...)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if cfg.API == nil {
		return fmt.Errorf("api section is required in config")
	}

	if err := cfg.ValidateAPI(); err != nil {
		return fmt.Errorf("validating api config: %w", err)
	}

	// Set up context with signal handling.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	srv := api.NewServer(log, cfg.API)

	if err := srv.Start(ctx); err != nil {
		return fmt.Errorf("starting api server: %w", err)
	}

	// Wait for shutdown signal.
	sig := <-sigCh
	log.WithField("signal", sig).Info("Shutting down API server")
	cancel()

	if err := srv.Stop(); err != nil {
		return fmt.Errorf("stopping api server: %w", err)
	}

	return nil
}
