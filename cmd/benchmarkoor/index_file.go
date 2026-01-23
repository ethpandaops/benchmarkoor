package main

import (
	"fmt"

	"github.com/ethpandaops/benchmarkoor/pkg/executor"
	"github.com/spf13/cobra"
)

var indexResultsDir string

var indexFileCmd = &cobra.Command{
	Use:   "generate-index-file",
	Short: "Generate index.json from all runs in results directory",
	Long:  `Scan all runs/*/config.json and result.json files to generate an index.json summary.`,
	RunE:  runIndexFile,
}

func init() {
	rootCmd.AddCommand(indexFileCmd)
	indexFileCmd.Flags().StringVar(&indexResultsDir, "results-dir", "", "Path to the results directory")

	if err := indexFileCmd.MarkFlagRequired("results-dir"); err != nil {
		panic(err)
	}
}

func runIndexFile(_ *cobra.Command, _ []string) error {
	log.WithField("results_dir", indexResultsDir).Info("Generating index.json")

	// Generate the index by scanning all run directories.
	index, err := executor.GenerateIndex(indexResultsDir)
	if err != nil {
		return fmt.Errorf("generating index: %w", err)
	}

	// Write the index.json file.
	if err := executor.WriteIndex(indexResultsDir, index, nil); err != nil {
		return fmt.Errorf("writing index: %w", err)
	}

	log.WithField("entries_count", len(index.Entries)).Info("index.json generated successfully")

	return nil
}
