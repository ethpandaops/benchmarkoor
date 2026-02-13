package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethpandaops/benchmarkoor/pkg/executor"
	"github.com/spf13/cobra"
)

var generateMarkdownSummaryCmd = &cobra.Command{
	Use:   "generate-markdown-summary",
	Short: "Generate a markdown summary from a benchmark run directory",
	Long:  `Reads config.json and result.json from a run directory and produces a markdown summary file.`,
	RunE:  runGenerateMarkdownSummary,
}

var (
	runToMdRunDir string
	runToMdOutput string
)

const maxMarkdownChars = 65000

func init() {
	rootCmd.AddCommand(generateMarkdownSummaryCmd)
	generateMarkdownSummaryCmd.Flags().StringVar(&runToMdRunDir, "run-dir", "",
		"Path to the benchmark run directory")
	generateMarkdownSummaryCmd.Flags().StringVar(&runToMdOutput, "output", "",
		"Output file path (default: summary-<run_id>.md)")

	if err := generateMarkdownSummaryCmd.MarkFlagRequired("run-dir"); err != nil {
		panic(err)
	}
}

func runGenerateMarkdownSummary(_ *cobra.Command, _ []string) error {
	runID := filepath.Base(runToMdRunDir)

	log.WithField("run_dir", runToMdRunDir).
		Info("Generating markdown summary")

	md, err := executor.GenerateRunMarkdown(
		runToMdRunDir, runID, maxMarkdownChars,
	)
	if err != nil {
		return fmt.Errorf("generating markdown: %w", err)
	}

	output := runToMdOutput
	if output == "" {
		output = fmt.Sprintf("summary-%s.md", runID)
	}

	if err := os.WriteFile(output, []byte(md), 0644); err != nil {
		return fmt.Errorf("writing output file: %w", err)
	}

	log.WithField("output", output).
		Info("Markdown summary generated successfully")

	return nil
}
