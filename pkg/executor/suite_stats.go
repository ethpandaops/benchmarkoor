package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// SuiteStats maps test names directly to their durations.
type SuiteStats map[string]*TestDurations

// TestDurations contains the duration entries for a test across multiple runs.
type TestDurations struct {
	Durations []*RunDuration `json:"durations"`
}

// RunDuration contains timing information for a single run of a test.
type RunDuration struct {
	ID       string `json:"id"`
	Client   string `json:"client"`
	GasUsed  uint64 `json:"gas_used"`
	Time     int64  `json:"time_ns"`
	RunStart int64  `json:"run_start"`
}

// runInfo holds information about a run for grouping purposes.
type runInfo struct {
	runID     string
	client    string
	timestamp int64
}

// GenerateAllSuiteStats scans the results directory and generates stats for all suites.
func GenerateAllSuiteStats(resultsDir string) (map[string]*SuiteStats, error) {
	runsDir := filepath.Join(resultsDir, "runs")

	entries, err := os.ReadDir(runsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]*SuiteStats), nil
		}

		return nil, fmt.Errorf("reading runs directory: %w", err)
	}

	// Group runs by suite hash.
	suiteRuns := make(map[string][]runInfo)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		runID := entry.Name()
		runDir := filepath.Join(runsDir, runID)

		// Read config.json to get suite_hash and client.
		configPath := filepath.Join(runDir, "config.json")

		configData, err := os.ReadFile(configPath)
		if err != nil {
			// Skip runs without config.
			continue
		}

		var runConfig runConfigJSON
		if err := json.Unmarshal(configData, &runConfig); err != nil {
			// Skip runs with invalid config.
			continue
		}

		if runConfig.SuiteHash == "" {
			// Skip runs without a suite hash.
			continue
		}

		suiteRuns[runConfig.SuiteHash] = append(suiteRuns[runConfig.SuiteHash], runInfo{
			runID:     runID,
			client:    runConfig.Instance.Client,
			timestamp: runConfig.Timestamp,
		})
	}

	// Build stats for each suite.
	allStats := make(map[string]*SuiteStats, len(suiteRuns))

	for suiteHash, runs := range suiteRuns {
		stats, err := buildSuiteStats(runsDir, runs)
		if err != nil {
			return nil, fmt.Errorf("building stats for suite %s: %w", suiteHash, err)
		}

		allStats[suiteHash] = stats
	}

	return allStats, nil
}

// buildSuiteStats builds statistics for a single suite from its runs.
func buildSuiteStats(runsDir string, runs []runInfo) (*SuiteStats, error) {
	stats := make(SuiteStats)

	for _, run := range runs {
		runDir := filepath.Join(runsDir, run.runID)
		resultPath := filepath.Join(runDir, "result.json")

		resultData, err := os.ReadFile(resultPath)
		if err != nil {
			// Skip runs without result.json.
			continue
		}

		var runResult RunResult
		if err := json.Unmarshal(resultData, &runResult); err != nil {
			// Skip runs with invalid result.
			continue
		}

		for testName, testEntry := range runResult.Tests {
			if testEntry.Steps == nil {
				continue
			}

			// Aggregate stats from all steps.
			var totalGasUsed uint64

			var totalGasUsedTime int64

			steps := []*StepResult{testEntry.Steps.Setup, testEntry.Steps.Test, testEntry.Steps.Cleanup}
			for _, step := range steps {
				if step == nil || step.Aggregated == nil {
					continue
				}

				totalGasUsed += step.Aggregated.GasUsedTotal
				totalGasUsedTime += step.Aggregated.GasUsedTimeTotal
			}

			if stats[testName] == nil {
				stats[testName] = &TestDurations{
					Durations: make([]*RunDuration, 0, len(runs)),
				}
			}

			stats[testName].Durations = append(stats[testName].Durations, &RunDuration{
				ID:       run.runID,
				Client:   run.client,
				GasUsed:  totalGasUsed,
				Time:     totalGasUsedTime,
				RunStart: run.timestamp,
			})
		}
	}

	// Sort durations by time_ns descending (higher first).
	for _, testDurations := range stats {
		sort.Slice(testDurations.Durations, func(i, j int) bool {
			return testDurations.Durations[i].Time > testDurations.Durations[j].Time
		})
	}

	return &stats, nil
}

// WriteSuiteStats writes suite statistics to the appropriate file.
func WriteSuiteStats(resultsDir, suiteHash string, stats *SuiteStats) error {
	suitesDir := filepath.Join(resultsDir, "suites", suiteHash)

	if err := os.MkdirAll(suitesDir, 0755); err != nil {
		return fmt.Errorf("creating suites directory: %w", err)
	}

	statsPath := filepath.Join(suitesDir, "stats.json")

	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling suite stats: %w", err)
	}

	if err := os.WriteFile(statsPath, data, 0644); err != nil {
		return fmt.Errorf("writing stats.json: %w", err)
	}

	return nil
}
