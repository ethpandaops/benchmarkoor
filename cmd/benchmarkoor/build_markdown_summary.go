package main

import (
	"fmt"
	"os"

	"github.com/ethpandaops/benchmarkoor/pkg/executor"
	"github.com/spf13/cobra"
)

var generateBuildMarkdownSummaryCmd = &cobra.Command{
	Use:   "generate-build-markdown-summary",
	Short: "Generate a markdown summary from a benchmarkoor build",
	Long: `Reads a build summary JSON (written by 'benchmarkoor build --summary-json') and
produces a markdown summary of the state-actor and eest-payloads builds, enriched
with each target's on-disk artifacts (state-actor manifest, eest fill counts).`,
	RunE: runGenerateBuildMarkdownSummary,
}

var (
	buildToMdInput  string
	buildToMdOutput string
)

func init() {
	rootCmd.AddCommand(generateBuildMarkdownSummaryCmd)
	generateBuildMarkdownSummaryCmd.Flags().StringVar(&buildToMdInput, "input", "",
		"Path to the build summary JSON written by `benchmarkoor build --summary-json`")
	generateBuildMarkdownSummaryCmd.Flags().StringVar(&buildToMdOutput, "output", "",
		"Output file path (default: build-summary.md)")

	if err := generateBuildMarkdownSummaryCmd.MarkFlagRequired("input"); err != nil {
		panic(err)
	}
}

func runGenerateBuildMarkdownSummary(_ *cobra.Command, _ []string) error {
	log.WithField("input", buildToMdInput).Info("Generating build markdown summary")

	md, err := executor.GenerateBuildMarkdown(buildToMdInput, maxMarkdownChars)
	if err != nil {
		return fmt.Errorf("generating build markdown: %w", err)
	}

	output := buildToMdOutput
	if output == "" {
		output = "build-summary.md"
	}

	if err := os.WriteFile(output, []byte(md), 0644); err != nil {
		return fmt.Errorf("writing output file: %w", err)
	}

	log.WithField("output", output).Info("Build markdown summary written")

	return nil
}
