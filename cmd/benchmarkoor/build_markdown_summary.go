package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ethpandaops/benchmarkoor/pkg/datadir"
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

	// The summary renders each target from its on-disk output_dir (state-actor
	// manifest, eest fill sidecar). A state-actor datadir promoted onto a schelk
	// mount may be unmounted by now, so its manifest would read as missing and
	// the (skipped) target would show no data. Mount it first.
	ensureSchelkForBuildSummary(context.Background(), buildToMdInput)

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

// ensureSchelkForBuildSummary mounts the schelk scratch when any build target's
// output_dir lives under a schelk mount, so the renderer can read its on-disk
// artifacts (e.g. a state-actor manifest). Best-effort — a non-schelk host or an
// unreadable summary is a no-op; one schelk volume covers every schelk output_dir.
func ensureSchelkForBuildSummary(ctx context.Context, summaryPath string) {
	summary, err := executor.ReadBuildSummary(summaryPath)
	if err != nil {
		return
	}

	for _, t := range summary.Targets {
		if _, isSchelk, err := datadir.SchelkDir(t.OutputDir); err == nil && isSchelk {
			if err := datadir.EnsureSchelkMounted(ctx, log); err != nil {
				log.WithError(err).Warn("Failed to ensure schelk mount for build summary")
			}

			return
		}
	}
}
