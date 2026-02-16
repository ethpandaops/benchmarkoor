package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/cpufreq"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/ethpandaops/benchmarkoor/pkg/executor"
	"github.com/ethpandaops/benchmarkoor/pkg/fsutil"
	"github.com/ethpandaops/benchmarkoor/pkg/runner"
	"github.com/ethpandaops/benchmarkoor/pkg/upload"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	limitInstanceIDs     []string
	limitInstanceClients []string
	metadataLabels       []string
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the benchmark",
	Long:  `Start all configured client instances and run the benchmark.`,
	RunE:  runBenchmark,
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.Flags().StringSliceVar(&limitInstanceIDs, "limit-instance-id", nil,
		"Limit to instances with these IDs (comma-separated or repeated flag)")
	runCmd.Flags().StringSliceVar(&limitInstanceClients, "limit-instance-client", nil,
		"Limit to instances with these client types (comma-separated or repeated flag)")
	runCmd.Flags().StringSliceVar(&metadataLabels, "metadata.label", nil,
		"Add metadata label as key=value (can be repeated)")
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

	// Merge CLI metadata labels into config (CLI wins on conflict).
	for _, entry := range metadataLabels {
		k, v, ok := strings.Cut(entry, "=")
		if !ok || k == "" {
			return fmt.Errorf("invalid metadata label %q: must be key=value", entry)
		}

		if cfg.Global.Metadata.Labels == nil {
			cfg.Global.Metadata.Labels = make(map[string]string, len(metadataLabels))
		}

		cfg.Global.Metadata.Labels[k] = v
	}

	// Filter instances if limits are specified (before validation so we can
	// scope datadir checks to active instances only).
	instances := filterInstances(cfg.Client.Instances, limitInstanceIDs, limitInstanceClients)
	if len(instances) == 0 {
		return fmt.Errorf("no instances match the specified filters")
	}

	if len(instances) != len(cfg.Client.Instances) {
		log.WithFields(logrus.Fields{
			"total":    len(cfg.Client.Instances),
			"filtered": len(instances),
		}).Info("Running filtered instances")
	}

	// Build validation opts to scope datadir validation to active instances.
	var validateOpts config.ValidateOpts
	if len(instances) != len(cfg.Client.Instances) {
		validateOpts.ActiveInstanceIDs = make(map[string]struct{}, len(instances))
		validateOpts.ActiveClients = make(map[string]struct{}, len(instances))

		for _, inst := range instances {
			validateOpts.ActiveInstanceIDs[inst.ID] = struct{}{}
			validateOpts.ActiveClients[inst.Client] = struct{}{}
		}
	}

	// Validate configuration.
	if err := cfg.Validate(validateOpts); err != nil {
		return fmt.Errorf("validating config: %w", err)
	}

	// Parse results owner configuration.
	resultsOwner, err := fsutil.ParseOwner(cfg.Benchmark.ResultsOwner)
	if err != nil {
		return fmt.Errorf("parsing results_owner: %w", err)
	}

	// Use consistent log format when client logs go to stdout.
	if cfg.Global.ClientLogsToStdout {
		log.SetFormatter(&consistentFormatter{prefix: "🔵"})
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

	if !cfg.Benchmark.SkipTestRun {
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
				Source:                          &cfg.Benchmark.Tests.Source,
				Filter:                          cfg.Benchmark.Tests.Filter,
				CacheDir:                        cacheDir,
				ResultsDir:                      cfg.Benchmark.ResultsDir,
				ResultsOwner:                    resultsOwner,
				SystemResourceCollectionEnabled: *cfg.Benchmark.SystemResourceCollectionEnabled,
				GitHubToken:                     cfg.Global.GitHubToken,
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

		// Create CPU frequency manager if CPU frequency settings are configured.
		var cpufreqMgr cpufreq.Manager
		if needsCPUFreqManager(cfg) {
			cacheDir := cfg.Global.Directories.TmpCacheDir
			if cacheDir == "" {
				var err error
				cacheDir, err = getExecutorCacheDir()
				if err != nil {
					return fmt.Errorf("getting cache directory: %w", err)
				}
			}

			cpufreqMgr = cpufreq.NewManager(log, cacheDir, cfg.GetCPUSysfsPath())
			if err := cpufreqMgr.Start(ctx); err != nil {
				return fmt.Errorf("starting cpufreq manager: %w", err)
			}

			defer func() {
				if err := cpufreqMgr.Stop(); err != nil {
					log.WithError(err).Warn("Failed to stop cpufreq manager")
				}
			}()

			log.Info("CPU frequency manager initialized")
		}

		// Create S3 uploader if configured.
		var resultsUploader upload.Uploader

		if cfg.Benchmark.ResultsUpload != nil &&
			cfg.Benchmark.ResultsUpload.S3 != nil &&
			cfg.Benchmark.ResultsUpload.S3.Enabled {
			resultsUploader, err = upload.NewS3Uploader(log, cfg.Benchmark.ResultsUpload.S3)
			if err != nil {
				return fmt.Errorf("creating S3 uploader: %w", err)
			}

			// Fail fast: verify S3 is reachable and writable before starting benchmarks.
			if err := resultsUploader.Preflight(ctx); err != nil {
				return fmt.Errorf("S3 upload preflight check failed: %w", err)
			}

			log.Info("S3 upload preflight check passed")
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
			FullConfig:         cfg,
		}

		r := runner.NewRunner(log, runnerCfg, dockerMgr, registry, exec, cpufreqMgr, resultsUploader)

		if err := r.Start(ctx); err != nil {
			return fmt.Errorf("starting runner: %w", err)
		}

		defer func() {
			if err := r.Stop(); err != nil {
				log.WithError(err).Warn("Failed to stop runner")
			}
		}()

		// Run all configured instances.
		for _, instance := range instances {
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
	} else {
		log.Info("Skipping test runs (skip_test_run is enabled)")
	}

	// Generate results index if configured.
	if cfg.Benchmark.GenerateResultsIndex {
		if err := generateResultsIndex(cmd, cfg, resultsOwner); err != nil {
			log.WithError(err).Warn("Failed to generate results index")
		}
	}

	// Generate suite stats if configured.
	if cfg.Benchmark.GenerateSuiteStats {
		if err := generateSuiteStats(cmd, cfg, resultsOwner); err != nil {
			log.WithError(err).Warn("Failed to generate suite stats")
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

// needsCPUFreqManager returns true if any instance has CPU frequency settings configured.
func needsCPUFreqManager(cfg *config.Config) bool {
	// Check global resource limits.
	if cfg.Client.Config.ResourceLimits != nil {
		if cfg.Client.Config.ResourceLimits.CPUFreq != "" ||
			cfg.Client.Config.ResourceLimits.CPUTurboBoost != nil ||
			cfg.Client.Config.ResourceLimits.CPUGovernor != "" {
			return true
		}
	}

	// Check instance-level resource limits.
	for _, instance := range cfg.Client.Instances {
		if instance.ResourceLimits != nil {
			if instance.ResourceLimits.CPUFreq != "" ||
				instance.ResourceLimits.CPUTurboBoost != nil ||
				instance.ResourceLimits.CPUGovernor != "" {
				return true
			}
		}
	}

	return false
}

// generateResultsIndex generates index.json using either the local filesystem or S3.
func generateResultsIndex(
	cmd *cobra.Command,
	cfg *config.Config,
	resultsOwner *fsutil.OwnerConfig,
) error {
	method := cfg.Benchmark.GenerateResultsIndexMethod

	switch method {
	case "", "local":
		return generateResultsIndexLocal(cfg, resultsOwner)
	case "s3":
		return generateResultsIndexS3(cmd, cfg)
	default:
		return fmt.Errorf(
			"unsupported generate_results_index_method %q (use \"local\" or \"s3\")",
			method,
		)
	}
}

// generateResultsIndexLocal generates index.json from a local results directory.
func generateResultsIndexLocal(
	cfg *config.Config,
	resultsOwner *fsutil.OwnerConfig,
) error {
	log.Info("Generating results index from local filesystem")

	index, err := executor.GenerateIndex(cfg.Benchmark.ResultsDir)
	if err != nil {
		return fmt.Errorf("generating index: %w", err)
	}

	if err := executor.WriteIndex(cfg.Benchmark.ResultsDir, index, resultsOwner); err != nil {
		return fmt.Errorf("writing index: %w", err)
	}

	log.WithField("entries", len(index.Entries)).Info("Results index generated")

	return nil
}

// generateResultsIndexS3 generates index.json by reading runs from S3
// and uploads the result back to the bucket.
func generateResultsIndexS3(cmd *cobra.Command, cfg *config.Config) error {
	if cfg.Benchmark.ResultsUpload == nil ||
		cfg.Benchmark.ResultsUpload.S3 == nil ||
		!cfg.Benchmark.ResultsUpload.S3.Enabled {
		return fmt.Errorf(
			"generate_results_index_method is \"s3\" but S3 upload " +
				"is not configured or not enabled",
		)
	}

	s3Cfg := cfg.Benchmark.ResultsUpload.S3

	prefix := s3Cfg.Prefix
	if prefix == "" {
		prefix = "results"
	}

	prefix = strings.TrimRight(prefix, "/")
	runsPrefix := prefix + "/runs/"

	reader := upload.NewS3Reader(log, s3Cfg)
	ctx := cmd.Context()

	log.WithFields(logrus.Fields{
		"bucket": s3Cfg.Bucket,
		"prefix": runsPrefix,
	}).Info("Generating results index from S3")

	index, err := executor.GenerateIndexFromS3(ctx, log, reader, runsPrefix)
	if err != nil {
		return fmt.Errorf("generating index from S3: %w", err)
	}

	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling index: %w", err)
	}

	indexKey := prefix + "/index.json"

	log.WithFields(logrus.Fields{
		"key":     indexKey,
		"entries": len(index.Entries),
	}).Info("Uploading index.json to S3")

	if err := reader.PutObject(ctx, indexKey, data, "application/json"); err != nil {
		return fmt.Errorf("uploading index.json: %w", err)
	}

	log.WithField("entries", len(index.Entries)).
		Info("Results index generated and uploaded to S3")

	return nil
}

// generateSuiteStats generates suite stats using either the local
// filesystem or S3, depending on the configured method.
func generateSuiteStats(
	cmd *cobra.Command,
	cfg *config.Config,
	resultsOwner *fsutil.OwnerConfig,
) error {
	method := cfg.Benchmark.GenerateSuiteStatsMethod

	switch method {
	case "", "local":
		return generateSuiteStatsLocal(cfg, resultsOwner)
	case "s3":
		return generateSuiteStatsS3(cmd, cfg)
	default:
		return fmt.Errorf(
			"unsupported generate_suite_stats_method %q"+
				" (use \"local\" or \"s3\")",
			method,
		)
	}
}

// generateSuiteStatsLocal generates suite stats from a local results directory.
func generateSuiteStatsLocal(
	cfg *config.Config,
	resultsOwner *fsutil.OwnerConfig,
) error {
	log.Info("Generating suite stats from local filesystem")

	allStats, err := executor.GenerateAllSuiteStats(cfg.Benchmark.ResultsDir)
	if err != nil {
		return fmt.Errorf("generating suite stats: %w", err)
	}

	for suiteHash, stats := range allStats {
		if err := executor.WriteSuiteStats(
			cfg.Benchmark.ResultsDir, suiteHash, stats, resultsOwner,
		); err != nil {
			log.WithError(err).WithField("suite", suiteHash).
				Warn("Failed to write suite stats")
		}
	}

	log.WithField("suites", len(allStats)).Info("Suite stats generated")

	return nil
}

// generateSuiteStatsS3 generates suite stats by reading runs from S3
// and uploads each stats.json back to the bucket.
func generateSuiteStatsS3(cmd *cobra.Command, cfg *config.Config) error {
	if cfg.Benchmark.ResultsUpload == nil ||
		cfg.Benchmark.ResultsUpload.S3 == nil ||
		!cfg.Benchmark.ResultsUpload.S3.Enabled {
		return fmt.Errorf(
			"generate_suite_stats_method is \"s3\" but S3 upload " +
				"is not configured or not enabled",
		)
	}

	s3Cfg := cfg.Benchmark.ResultsUpload.S3

	prefix := s3Cfg.Prefix
	if prefix == "" {
		prefix = "results"
	}

	prefix = strings.TrimRight(prefix, "/")
	runsPrefix := prefix + "/runs/"
	suitesBase := prefix + "/suites/"

	reader := upload.NewS3Reader(log, s3Cfg)
	ctx := cmd.Context()

	log.WithFields(logrus.Fields{
		"bucket": s3Cfg.Bucket,
		"prefix": runsPrefix,
	}).Info("Generating suite stats from S3")

	allStats, err := executor.GenerateAllSuiteStatsFromS3(
		ctx, log, reader, runsPrefix,
	)
	if err != nil {
		return fmt.Errorf("generating suite stats from S3: %w", err)
	}

	for suiteHash, stats := range allStats {
		data, err := json.MarshalIndent(stats, "", "  ")
		if err != nil {
			log.WithError(err).WithField("suite", suiteHash).
				Warn("Failed to marshal suite stats")

			continue
		}

		key := suitesBase + suiteHash + "/stats.json"
		if err := reader.PutObject(ctx, key, data, "application/json"); err != nil {
			log.WithError(err).WithField("suite", suiteHash).
				Warn("Failed to upload suite stats")

			continue
		}
	}

	log.WithField("suites", len(allStats)).
		Info("Suite stats generated and uploaded to S3")

	return nil
}

// filterInstances filters instances by ID and/or client type.
// If no filters are specified, all instances are returned.
func filterInstances(instances []config.ClientInstance, ids, clients []string) []config.ClientInstance {
	// No filters, return all.
	if len(ids) == 0 && len(clients) == 0 {
		return instances
	}

	// Build lookup sets for O(1) matching.
	idSet := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
	}

	clientSet := make(map[string]struct{}, len(clients))
	for _, c := range clients {
		clientSet[c] = struct{}{}
	}

	filtered := make([]config.ClientInstance, 0, len(instances))

	for _, instance := range instances {
		// If ID filter is set, instance must match.
		if len(idSet) > 0 {
			if _, ok := idSet[instance.ID]; !ok {
				continue
			}
		}

		// If client filter is set, instance must match.
		if len(clientSet) > 0 {
			if _, ok := clientSet[instance.Client]; !ok {
				continue
			}
		}

		filtered = append(filtered, instance)
	}

	return filtered
}
