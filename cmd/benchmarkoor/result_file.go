package main

import (
	"fmt"

	"github.com/ethpandaops/benchmarkoor/pkg/executor"
	"github.com/spf13/cobra"
)

var resultFileCmd = &cobra.Command{
	Use:   "generate-result-file",
	Short: "Generate result.json from an existing results directory",
	Long:  `Scan an existing results directory and generate a result.json summary file.`,
	RunE:  runResultFile,
}

var resultsDir string

func init() {
	rootCmd.AddCommand(resultFileCmd)
	resultFileCmd.Flags().StringVar(&resultsDir, "results-dir", "", "Path to the results directory")

	if err := resultFileCmd.MarkFlagRequired("results-dir"); err != nil {
		panic(err)
	}
}

func runResultFile(_ *cobra.Command, _ []string) error {
	log.WithField("results_dir", resultsDir).Info("Generating result.json")

	// Generate the run result by scanning the directory.
	result, err := executor.GenerateRunResult(resultsDir)
	if err != nil {
		return fmt.Errorf("generating run result: %w", err)
	}

	// Write the result.json file.
	if err := executor.WriteRunResult(resultsDir, result); err != nil {
		return fmt.Errorf("writing run result: %w", err)
	}

	log.WithField("tests_count", len(result.Tests)).Info("result.json generated successfully")

	return nil
}
