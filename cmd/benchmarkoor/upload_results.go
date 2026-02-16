package main

import (
	"fmt"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/upload"
	"github.com/spf13/cobra"
)

var (
	uploadMethod    string
	uploadResultDir string
)

var uploadResultsCmd = &cobra.Command{
	Use:   "upload-results",
	Short: "Upload benchmark results to remote storage",
	Long:  `Upload a local results directory to S3-compatible storage using the config file settings.`,
	RunE:  runUploadResults,
}

func init() {
	rootCmd.AddCommand(uploadResultsCmd)
	uploadResultsCmd.Flags().StringVar(&uploadMethod, "method", "s3",
		"Upload method (currently only \"s3\")")
	uploadResultsCmd.Flags().StringVar(&uploadResultDir, "result-dir", "",
		"Path to the result directory to upload")

	_ = uploadResultsCmd.MarkFlagRequired("result-dir")
}

func runUploadResults(cmd *cobra.Command, args []string) error {
	if len(cfgFiles) == 0 {
		return fmt.Errorf("config file is required (use --config)")
	}

	if uploadMethod != "s3" {
		return fmt.Errorf("unsupported method %q (only \"s3\" is supported)", uploadMethod)
	}

	cfg, err := config.Load(cfgFiles...)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if cfg.Benchmark.ResultsUpload == nil ||
		cfg.Benchmark.ResultsUpload.S3 == nil ||
		!cfg.Benchmark.ResultsUpload.S3.Enabled {
		return fmt.Errorf("S3 upload is not configured or not enabled in config")
	}

	uploader, err := upload.NewS3Uploader(log, cfg.Benchmark.ResultsUpload.S3)
	if err != nil {
		return fmt.Errorf("creating S3 uploader: %w", err)
	}

	ctx := cmd.Context()

	log.WithField("dir", uploadResultDir).Info("Uploading results")

	if err := uploader.Upload(ctx, uploadResultDir); err != nil {
		return fmt.Errorf("uploading results: %w", err)
	}

	log.Info("Upload completed successfully")

	return nil
}
