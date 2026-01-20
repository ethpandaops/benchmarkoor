package main

import (
	"fmt"

	"github.com/ethpandaops/benchmarkoor/pkg/executor"
	"github.com/spf13/cobra"
)

var suiteStatsResultsDir string

var suiteStatsFileCmd = &cobra.Command{
	Use:   "generate-suite-stats-file",
	Short: "Generate stats.json for each suite from all runs",
	Long:  `Scan all runs, group by suite hash, and generate stats.json per suite.`,
	RunE:  runSuiteStatsFile,
}

func init() {
	rootCmd.AddCommand(suiteStatsFileCmd)
	suiteStatsFileCmd.Flags().StringVar(&suiteStatsResultsDir, "results-dir", "", "Path to the results directory")

	if err := suiteStatsFileCmd.MarkFlagRequired("results-dir"); err != nil {
		panic(err)
	}
}

func runSuiteStatsFile(_ *cobra.Command, _ []string) error {
	log.WithField("results_dir", suiteStatsResultsDir).Info("Generating suite stats files")

	// Generate stats for all suites.
	allStats, err := executor.GenerateAllSuiteStats(suiteStatsResultsDir)
	if err != nil {
		return fmt.Errorf("generating suite stats: %w", err)
	}

	// Write stats for each suite.
	for suiteHash, stats := range allStats {
		if err := executor.WriteSuiteStats(suiteStatsResultsDir, suiteHash, stats); err != nil {
			return fmt.Errorf("writing stats for suite %s: %w", suiteHash, err)
		}

		log.WithField("suite_hash", suiteHash).WithField("test_count", len(*stats)).Info("Wrote stats.json")
	}

	log.WithField("suites_count", len(allStats)).Info("Suite stats generation complete")

	return nil
}
