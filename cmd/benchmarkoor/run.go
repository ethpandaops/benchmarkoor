package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/ethpandaops/benchmarkoor/pkg/executor"
	"github.com/ethpandaops/benchmarkoor/pkg/fsutil"
	"github.com/ethpandaops/benchmarkoor/pkg/runner"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the benchmark",
	Long:  `Start all configured client instances and run the benchmark.`,
	RunE:  runBenchmark,
}

func init() {
	rootCmd.AddCommand(runCmd)
}

func runBenchmark(cmd *cobra.Command, args []string) error {
	if len(cfgFiles) == 0 {
		return fmt.Errorf("config file is required (use --config)")
	}

	// Load configuration.
	cfg, err := config.Load(cfgFiles...)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Validate configuration.
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validating config: %w", err)
	}

	// Parse results owner configuration.
	resultsOwner, err := fsutil.ParseOwner(cfg.Benchmark.ResultsOwner)
	if err != nil {
		return fmt.Errorf("parsing results_owner: %w", err)
	}

	// Add blue emoji prefix to benchmarkoor logs when client logs go to stdout.
	if cfg.Global.ClientLogsToStdout {
		log.SetFormatter(&prefixedFormatter{
			prefix:    "🔵 ",
			formatter: log.Formatter,
		})
	}

	// Setup context with signal handling.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.WithField("signal", sig).Info("Received shutdown signal")
		cancel()
	}()

	// Create Docker manager.
	dockerMgr, err := docker.NewManager(log)
	if err != nil {
		return fmt.Errorf("creating docker manager: %w", err)
	}

	if err := dockerMgr.Start(ctx); err != nil {
		return fmt.Errorf("starting docker manager: %w", err)
	}

	defer func() {
		if err := dockerMgr.Stop(); err != nil {
			log.WithError(err).Warn("Failed to stop docker manager")
		}
	}()

	// Perform cleanup on start if configured.
	if cfg.Global.CleanupOnStart {
		log.Info("Performing cleanup before start")

		if err := performCleanup(ctx, dockerMgr, true); err != nil {
			log.WithError(err).Warn("Cleanup failed")
		}
	}

	// Create client registry.
	registry := client.NewRegistry()

	// Create executor if tests are configured.
	var exec executor.Executor

	if cfg.Benchmark.Tests.Source.IsConfigured() {
		cacheDir := cfg.Global.Directories.TmpCacheDir
		if cacheDir == "" {
			var err error

			cacheDir, err = getExecutorCacheDir()
			if err != nil {
				return fmt.Errorf("getting cache directory: %w", err)
			}
		}

		execCfg := &executor.Config{
			Source:       &cfg.Benchmark.Tests.Source,
			Filter:       cfg.Benchmark.Tests.Filter,
			CacheDir:     cacheDir,
			ResultsDir:   cfg.Benchmark.ResultsDir,
			ResultsOwner: resultsOwner,
		}

		exec = executor.NewExecutor(log, execCfg)
		if err := exec.Start(ctx); err != nil {
			return fmt.Errorf("starting executor: %w", err)
		}

		defer func() {
			if err := exec.Stop(); err != nil {
				log.WithError(err).Warn("Failed to stop executor")
			}
		}()

		log.Info("Test executor initialized")
	}

	// Create runner.
	runnerCfg := &runner.Config{
		ResultsDir:         cfg.Benchmark.ResultsDir,
		ResultsOwner:       resultsOwner,
		ClientLogsToStdout: cfg.Global.ClientLogsToStdout,
		DockerNetwork:      cfg.Global.DockerNetwork,
		JWT:                cfg.Client.Config.JWT,
		GenesisURLs:        cfg.Client.Config.Genesis,
		DataDirs:           cfg.Client.DataDirs,
		TmpDataDir:         cfg.Global.Directories.TmpDataDir,
		TmpCacheDir:        cfg.Global.Directories.TmpCacheDir,
		TestFilter:         cfg.Benchmark.Tests.Filter,
	}

	r := runner.NewRunner(log, runnerCfg, dockerMgr, registry, exec)

	if err := r.Start(ctx); err != nil {
		return fmt.Errorf("starting runner: %w", err)
	}

	defer func() {
		if err := r.Stop(); err != nil {
			log.WithError(err).Warn("Failed to stop runner")
		}
	}()

	// Run all configured instances.
	for _, instance := range cfg.Client.Instances {
		select {
		case <-ctx.Done():
			log.Info("Benchmark interrupted")

			return ctx.Err()
		default:
		}

		log.WithField("instance", instance.ID).Info("Running instance")

		if err := r.RunInstance(ctx, &instance); err != nil {
			log.WithError(err).WithField("instance", instance.ID).Error("Instance failed")

			// Continue with next instance on failure.
			continue
		}

		log.WithField("instance", instance.ID).Info("Instance completed successfully")
	}

	log.Info("Benchmark completed")

	// Generate results index if configured.
	if cfg.Benchmark.GenerateResultsIndex {
		log.Info("Generating results index")

		index, err := executor.GenerateIndex(cfg.Benchmark.ResultsDir)
		if err != nil {
			log.WithError(err).Warn("Failed to generate results index")
		} else if err := executor.WriteIndex(cfg.Benchmark.ResultsDir, index, resultsOwner); err != nil {
			log.WithError(err).Warn("Failed to write results index")
		} else {
			log.WithField("entries", len(index.Entries)).Info("Results index generated")
		}
	}

	// Generate suite stats if configured.
	if cfg.Benchmark.GenerateSuiteStats {
		log.Info("Generating suite stats")

		allStats, err := executor.GenerateAllSuiteStats(cfg.Benchmark.ResultsDir)
		if err != nil {
			log.WithError(err).Warn("Failed to generate suite stats")
		} else {
			for suiteHash, stats := range allStats {
				if err := executor.WriteSuiteStats(cfg.Benchmark.ResultsDir, suiteHash, stats, resultsOwner); err != nil {
					log.WithError(err).WithField("suite", suiteHash).Warn("Failed to write suite stats")
				}
			}

			log.WithField("suites", len(allStats)).Info("Suite stats generated")
		}
	}

	return nil
}

// getExecutorCacheDir returns the cache directory for the executor.
func getExecutorCacheDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}

	return filepath.Join(homeDir, ".cache", "benchmarkoor"), nil
}
