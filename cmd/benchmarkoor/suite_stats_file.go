package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/executor"
	"github.com/ethpandaops/benchmarkoor/pkg/upload"
	"github.com/spf13/cobra"
)

var (
	suiteStatsResultsDir string
	suiteStatsMethod     string
)

var suiteStatsFileCmd = &cobra.Command{
	Use:   "generate-suite-stats-file",
	Short: "Generate stats.json for each suite from all runs",
	Long: `Scan all runs, group by suite hash, and generate stats.json per suite.
Supports local filesystem or S3 as source.`,
	RunE: runSuiteStatsFile,
}

func init() {
	rootCmd.AddCommand(suiteStatsFileCmd)
	suiteStatsFileCmd.Flags().StringVar(
		&suiteStatsResultsDir, "results-dir", "",
		"Path to the results directory (required for --method=local)",
	)
	suiteStatsFileCmd.Flags().StringVar(
		&suiteStatsMethod, "method", "local",
		`Source method: "local" (filesystem) or "s3" (remote bucket)`,
	)
}

func runSuiteStatsFile(cmd *cobra.Command, _ []string) error {
	switch suiteStatsMethod {
	case "local":
		return runSuiteStatsFileLocal()
	case "s3":
		return runSuiteStatsFileS3(cmd)
	default:
		return fmt.Errorf(
			"unsupported method %q (use \"local\" or \"s3\")",
			suiteStatsMethod,
		)
	}
}

// runSuiteStatsFileLocal generates suite stats from a local results directory.
func runSuiteStatsFileLocal() error {
	if suiteStatsResultsDir == "" {
		return fmt.Errorf("--results-dir is required for --method=local")
	}

	log.WithField("results_dir", suiteStatsResultsDir).
		Info("Generating suite stats from local results")

	allStats, err := executor.GenerateAllSuiteStats(suiteStatsResultsDir)
	if err != nil {
		return fmt.Errorf("generating suite stats: %w", err)
	}

	for suiteHash, stats := range allStats {
		if err := executor.WriteSuiteStats(
			suiteStatsResultsDir, suiteHash, stats, nil,
		); err != nil {
			return fmt.Errorf("writing stats for suite %s: %w", suiteHash, err)
		}

		log.WithField("suite_hash", suiteHash).
			WithField("test_count", len(*stats)).
			Info("Wrote stats.json")
	}

	log.WithField("suites_count", len(allStats)).
		Info("Suite stats generation complete")

	return nil
}

// runSuiteStatsFileS3 generates suite stats by reading runs from S3
// and uploads each stats.json back to the bucket.
func runSuiteStatsFileS3(cmd *cobra.Command) error {
	if len(cfgFiles) == 0 {
		return fmt.Errorf("--config is required for --method=s3")
	}

	cfg, err := config.Load(cfgFiles...)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if cfg.Benchmark.ResultsUpload == nil ||
		cfg.Benchmark.ResultsUpload.S3 == nil ||
		!cfg.Benchmark.ResultsUpload.S3.Enabled {
		return fmt.Errorf(
			"S3 upload is not configured or not enabled in config",
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

	log.WithFields(map[string]any{
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
			return fmt.Errorf("marshaling stats for suite %s: %w", suiteHash, err)
		}

		key := suitesBase + suiteHash + "/stats.json"

		log.WithFields(map[string]any{
			"suite_hash": suiteHash,
			"key":        key,
			"test_count": len(*stats),
		}).Info("Uploading stats.json to S3")

		if err := reader.PutObject(ctx, key, data, "application/json"); err != nil {
			return fmt.Errorf("uploading stats for suite %s: %w", suiteHash, err)
		}
	}

	log.WithField("suites_count", len(allStats)).
		Info("Suite stats generated and uploaded successfully")

	return nil
}
