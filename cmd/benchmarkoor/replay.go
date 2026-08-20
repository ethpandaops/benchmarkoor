package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/executor"
	"github.com/spf13/cobra"
)

var (
	replayManifest       string
	replayEngineEndpoint string
	replayJWT            string
	replayJWTFile        string
	replayResultsDir     string
	replayRunID          string
	replayFilter         string
)

var replayCmd = &cobra.Command{
	Use:   "replay",
	Short: "Replay a semantic suite against an already running Engine API endpoint",
	Long: `Runs a tempo-engine-suite/v1 manifest against an existing node without
container management. The caller owns node startup and isolation. This is useful
for local development and environments where Docker or Podman is unavailable.`,
	RunE: runReplay,
}

func init() {
	rootCmd.AddCommand(replayCmd)
	replayCmd.Flags().StringVar(&replayManifest, "manifest", "", "Tempo suite manifest")
	replayCmd.Flags().StringVar(
		&replayEngineEndpoint, "engine-endpoint", "http://127.0.0.1:8551",
		"Authenticated Engine API endpoint",
	)
	replayCmd.Flags().StringVar(&replayJWT, "jwt", "", "JWT secret as hex")
	replayCmd.Flags().StringVar(&replayJWTFile, "jwt-file", "", "File containing the JWT secret")
	replayCmd.Flags().StringVar(&replayResultsDir, "results-dir", "./results", "Results root")
	replayCmd.Flags().StringVar(&replayRunID, "run-id", "", "Stable run ID (defaults to UTC timestamp)")
	replayCmd.Flags().StringVar(&replayFilter, "filter", "", "Optional suite test/tag filter")

	if err := replayCmd.MarkFlagRequired("manifest"); err != nil {
		panic(err)
	}
}

func runReplay(cmd *cobra.Command, _ []string) error {
	jwt, err := resolveReplayJWT()
	if err != nil {
		return err
	}

	runID := replayRunID
	if runID == "" {
		runID = "local-tempo-" + time.Now().UTC().Format("20060102T150405Z")
	}
	runDir := filepath.Join(replayResultsDir, "runs", runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return fmt.Errorf("creating replay run directory: %w", err)
	}

	started := time.Now().UTC()
	exec := executor.NewExecutor(log, &executor.Config{
		Source:     &config.SourceConfig{TempoSuite: &config.TempoSuiteSource{Manifest: replayManifest}},
		Filter:     replayFilter,
		ResultsDir: replayResultsDir,
	})
	if err := exec.Start(cmd.Context()); err != nil {
		return fmt.Errorf("preparing replay suite: %w", err)
	}
	defer func() {
		if stopErr := exec.Stop(); stopErr != nil {
			log.WithError(stopErr).Warn("Failed to stop replay executor")
		}
	}()

	tests := exec.GetTests()
	if len(tests) == 0 {
		return fmt.Errorf("manifest/filter selected no tests")
	}
	if len(tests) != 1 {
		return fmt.Errorf(
			"local replay requires exactly one selected test, got %d; use --filter or a single-test manifest so tests do not share node state",
			len(tests),
		)
	}
	exec.ResetProgress(len(tests))
	result, execErr := exec.ExecuteTests(cmd.Context(), &executor.ExecuteOptions{
		EngineEndpoint:   replayEngineEndpoint,
		JWT:              jwt,
		ResultsDir:       runDir,
		Filter:           replayFilter,
		RollbackStrategy: config.RollbackStrategyNone,
		FailFast:         true,
	})

	status := "completed"
	if execErr != nil || result.Failed > 0 {
		status = "failed"
	}
	if err := writeReplayConfig(runDir, exec.GetSuiteHash(), runID, status, started, result); err != nil {
		return err
	}

	markdown, markdownErr := executor.GenerateRunMarkdown(runDir, runID, maxMarkdownChars)
	if markdownErr == nil {
		markdownErr = os.WriteFile(filepath.Join(runDir, "summary.md"), []byte(markdown), 0o644)
	}
	if markdownErr != nil {
		log.WithError(markdownErr).Warn("Failed to generate replay Markdown")
	}

	log.WithField("run_dir", runDir).
		WithField("tests", result.TotalTests).
		WithField("passed", result.Passed).
		WithField("failed", result.Failed).
		Info("Local Engine API replay complete")

	if execErr != nil {
		return fmt.Errorf("executing replay: %w", execErr)
	}
	if result.Failed > 0 {
		return fmt.Errorf("%d replay test(s) failed", result.Failed)
	}
	return nil
}

func resolveReplayJWT() (string, error) {
	if replayJWT != "" && replayJWTFile != "" {
		return "", fmt.Errorf("--jwt and --jwt-file are mutually exclusive")
	}
	if replayJWTFile == "" {
		return strings.TrimSpace(replayJWT), nil
	}

	data, err := os.ReadFile(replayJWTFile)
	if err != nil {
		return "", fmt.Errorf("reading JWT file: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func writeReplayConfig(
	runDir, suiteHash, runID, status string,
	started time.Time,
	result *executor.ExecutionResult,
) error {
	data := map[string]any{
		"timestamp":     started.Unix(),
		"timestamp_end": time.Now().UTC().Unix(),
		"suite_hash":    suiteHash,
		"status":        status,
		"instance": map[string]any{
			"id":                runID,
			"client":            "tempo",
			"image":             "local-process",
			"rollback_strategy": config.RollbackStrategyNone,
		},
		"test_counts": map[string]int{
			"total":  result.TotalTests,
			"passed": result.Passed,
			"failed": result.Failed,
		},
	}

	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding replay config: %w", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "config.json"), encoded, 0o644); err != nil {
		return fmt.Errorf("writing replay config: %w", err)
	}
	return nil
}
