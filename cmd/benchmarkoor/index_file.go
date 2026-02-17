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
	indexResultsDir string
	indexMethod     string
)

var indexFileCmd = &cobra.Command{
	Use:   "generate-index-file",
	Short: "Generate index.json from all runs in results directory",
	Long: `Scan all runs/*/config.json and result.json files to generate
an index.json summary. Supports local filesystem or S3 as source.`,
	RunE: runIndexFile,
}

func init() {
	rootCmd.AddCommand(indexFileCmd)
	indexFileCmd.Flags().StringVar(
		&indexResultsDir, "results-dir", "",
		"Path to the results directory (required for --method=local)",
	)
	indexFileCmd.Flags().StringVar(
		&indexMethod, "method", "local",
		`Source method: "local" (filesystem) or "s3" (remote bucket)`,
	)
}

func runIndexFile(cmd *cobra.Command, _ []string) error {
	switch indexMethod {
	case "local":
		return runIndexFileLocal()
	case "s3":
		return runIndexFileS3(cmd)
	default:
		return fmt.Errorf(
			"unsupported method %q (use \"local\" or \"s3\")", indexMethod,
		)
	}
}

// runIndexFileLocal generates index.json from a local results directory.
func runIndexFileLocal() error {
	if indexResultsDir == "" {
		return fmt.Errorf("--results-dir is required for --method=local")
	}

	log.WithField("results_dir", indexResultsDir).
		Info("Generating index.json from local results")

	index, err := executor.GenerateIndex(indexResultsDir)
	if err != nil {
		return fmt.Errorf("generating index: %w", err)
	}

	if err := executor.WriteIndex(indexResultsDir, index, nil); err != nil {
		return fmt.Errorf("writing index: %w", err)
	}

	log.WithField("entries_count", len(index.Entries)).
		Info("index.json generated successfully")

	return nil
}

// runIndexFileS3 generates index.json by reading runs from S3 and
// uploads the result back to the bucket.
func runIndexFileS3(cmd *cobra.Command) error {
	if len(cfgFiles) == 0 {
		return fmt.Errorf("--config is required for --method=s3")
	}

	cfg, err := config.Load(cfgFiles...)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if cfg.Runner.Benchmark.ResultsUpload == nil ||
		cfg.Runner.Benchmark.ResultsUpload.S3 == nil ||
		!cfg.Runner.Benchmark.ResultsUpload.S3.Enabled {
		return fmt.Errorf(
			"S3 upload is not configured or not enabled in config",
		)
	}

	s3Cfg := cfg.Runner.Benchmark.ResultsUpload.S3

	prefix := s3Cfg.Prefix
	if prefix == "" {
		prefix = "results"
	}

	prefix = strings.TrimRight(prefix, "/")
	runsPrefix := prefix + "/runs/"

	reader := upload.NewS3Reader(log, s3Cfg)
	ctx := cmd.Context()

	log.WithFields(map[string]any{
		"bucket": s3Cfg.Bucket,
		"prefix": runsPrefix,
	}).Info("Generating index.json from S3")

	index, err := executor.GenerateIndexFromS3(ctx, log, reader, runsPrefix)
	if err != nil {
		return fmt.Errorf("generating index from S3: %w", err)
	}

	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling index: %w", err)
	}

	indexKey := prefix + "/index.json"

	log.WithFields(map[string]any{
		"key":           indexKey,
		"entries_count": len(index.Entries),
	}).Info("Uploading index.json to S3")

	if err := reader.PutObject(ctx, indexKey, data, "application/json"); err != nil {
		return fmt.Errorf("uploading index.json: %w", err)
	}

	log.WithField("entries_count", len(index.Entries)).
		Info("index.json generated and uploaded successfully")

	return nil
}
