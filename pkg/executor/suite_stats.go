package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ethpandaops/benchmarkoor/pkg/fsutil"
	"github.com/sirupsen/logrus"
)

// SuiteStats maps test names directly to their durations.
type SuiteStats map[string]*TestDurations

// TestDurations contains the duration entries for a test across multiple runs.
type TestDurations struct {
	Durations []*RunDuration `json:"durations"`
}

// RunDuration contains timing information for a single run of a test.
type RunDuration struct {
	ID       string                 `json:"id"`
	Client   string                 `json:"client"`
	GasUsed  uint64                 `json:"gas_used"`
	Time     int64                  `json:"time_ns"`
	RunStart int64                  `json:"run_start"`
	RunEnd   int64                  `json:"run_end,omitempty"`
	Steps    *RunDurationStepsStats `json:"steps,omitempty"`
}

// RunDurationStepsStats contains per-step gas and time data.
type RunDurationStepsStats struct {
	Setup   *RunDurationStepStats `json:"setup,omitempty"`
	Test    *RunDurationStepStats `json:"test,omitempty"`
	Cleanup *RunDurationStepStats `json:"cleanup,omitempty"`
}

// RunDurationStepStats contains gas and time data for a single step.
type RunDurationStepStats struct {
	GasUsed uint64 `json:"gas_used"`
	Time    int64  `json:"time_ns"`
}

// RunInfo holds information about a run for grouping purposes.
type RunInfo struct {
	RunID        string
	Client       string
	Timestamp    int64
	TimestampEnd int64
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
	suiteRuns := make(map[string][]RunInfo)

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

		suiteRuns[runConfig.SuiteHash] = append(suiteRuns[runConfig.SuiteHash], RunInfo{
			RunID:        runID,
			Client:       runConfig.Instance.Client,
			Timestamp:    runConfig.Timestamp,
			TimestampEnd: runConfig.TimestampEnd,
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
func buildSuiteStats(runsDir string, runs []RunInfo) (*SuiteStats, error) {
	stats := make(SuiteStats)

	for _, run := range runs {
		runDir := filepath.Join(runsDir, run.RunID)
		resultPath := filepath.Join(runDir, "result.json")

		resultData, err := os.ReadFile(resultPath)
		if err != nil {
			// Skip runs without result.json.
			continue
		}

		AccumulateRunResult(&stats, resultData, run)
	}

	sortSuiteStats(&stats)

	return &stats, nil
}

// AccumulateRunResult parses a result.json payload and merges its per-test
// durations into stats. Invalid JSON is silently skipped.
func AccumulateRunResult(stats *SuiteStats, resultData []byte, run RunInfo) {
	var runResult RunResult
	if err := json.Unmarshal(resultData, &runResult); err != nil {
		return
	}

	for testName, testEntry := range runResult.Tests {
		if testEntry.Steps == nil {
			continue
		}

		// Aggregate stats from all steps.
		var totalGasUsed uint64
		var totalGasUsedTime int64

		// Build per-step stats.
		stepsStats := &RunDurationStepsStats{}

		if testEntry.Steps.Setup != nil && testEntry.Steps.Setup.Aggregated != nil {
			stepsStats.Setup = &RunDurationStepStats{
				GasUsed: testEntry.Steps.Setup.Aggregated.GasUsedTotal,
				Time:    testEntry.Steps.Setup.Aggregated.GasUsedTimeTotal,
			}
			totalGasUsed += testEntry.Steps.Setup.Aggregated.GasUsedTotal
			totalGasUsedTime += testEntry.Steps.Setup.Aggregated.GasUsedTimeTotal
		}

		if testEntry.Steps.Test != nil && testEntry.Steps.Test.Aggregated != nil {
			stepsStats.Test = &RunDurationStepStats{
				GasUsed: testEntry.Steps.Test.Aggregated.GasUsedTotal,
				Time:    testEntry.Steps.Test.Aggregated.GasUsedTimeTotal,
			}
			totalGasUsed += testEntry.Steps.Test.Aggregated.GasUsedTotal
			totalGasUsedTime += testEntry.Steps.Test.Aggregated.GasUsedTimeTotal
		}

		if testEntry.Steps.Cleanup != nil && testEntry.Steps.Cleanup.Aggregated != nil {
			stepsStats.Cleanup = &RunDurationStepStats{
				GasUsed: testEntry.Steps.Cleanup.Aggregated.GasUsedTotal,
				Time:    testEntry.Steps.Cleanup.Aggregated.GasUsedTimeTotal,
			}
			totalGasUsed += testEntry.Steps.Cleanup.Aggregated.GasUsedTotal
			totalGasUsedTime += testEntry.Steps.Cleanup.Aggregated.GasUsedTimeTotal
		}

		if (*stats)[testName] == nil {
			(*stats)[testName] = &TestDurations{
				Durations: make([]*RunDuration, 0, 4),
			}
		}

		(*stats)[testName].Durations = append((*stats)[testName].Durations, &RunDuration{
			ID:       run.RunID,
			Client:   run.Client,
			GasUsed:  totalGasUsed,
			Time:     totalGasUsedTime,
			RunStart: run.Timestamp,
			RunEnd:   run.TimestampEnd,
			Steps:    stepsStats,
		})
	}
}

// sortSuiteStats sorts durations within each test by time_ns descending.
func sortSuiteStats(stats *SuiteStats) {
	for _, testDurations := range *stats {
		sort.Slice(testDurations.Durations, func(i, j int) bool {
			return testDurations.Durations[i].Time > testDurations.Durations[j].Time
		})
	}
}

// GenerateAllSuiteStatsFromS3 builds suite stats by reading config.json and
// result.json from each run stored under runsPrefix in remote storage.
func GenerateAllSuiteStatsFromS3(
	ctx context.Context,
	log logrus.FieldLogger,
	reader IndexObjectReader,
	runsPrefix string,
) (map[string]*SuiteStats, error) {
	if !strings.HasSuffix(runsPrefix, "/") {
		runsPrefix += "/"
	}

	prefixes, err := reader.ListPrefixes(ctx, runsPrefix)
	if err != nil {
		return nil, fmt.Errorf("listing run prefixes: %w", err)
	}

	// Group runs by suite hash.
	suiteRuns := make(map[string][]RunInfo)
	runPrefixes := make(map[string]string) // runID → prefix

	for _, prefix := range prefixes {
		runID := path.Base(strings.TrimRight(prefix, "/"))

		configData, err := reader.GetObject(ctx, prefix+"config.json")
		if err != nil {
			log.WithError(err).WithField("run_id", runID).
				Warn("Skipping run: failed to read config.json")

			continue
		}

		if configData == nil {
			log.WithField("run_id", runID).
				Debug("Skipping run: config.json not found")

			continue
		}

		var runConfig runConfigJSON
		if err := json.Unmarshal(configData, &runConfig); err != nil {
			log.WithError(err).WithField("run_id", runID).
				Warn("Skipping run: invalid config.json")

			continue
		}

		if runConfig.SuiteHash == "" {
			continue
		}

		suiteRuns[runConfig.SuiteHash] = append(
			suiteRuns[runConfig.SuiteHash],
			RunInfo{
				RunID:        runID,
				Client:       runConfig.Instance.Client,
				Timestamp:    runConfig.Timestamp,
				TimestampEnd: runConfig.TimestampEnd,
			},
		)
		runPrefixes[runID] = prefix
	}

	// Build stats for each suite.
	allStats := make(map[string]*SuiteStats, len(suiteRuns))

	for suiteHash, runs := range suiteRuns {
		stats := make(SuiteStats)

		for _, run := range runs {
			prefix := runPrefixes[run.RunID]

			resultData, err := reader.GetObject(ctx, prefix+"result.json")
			if err != nil {
				log.WithError(err).WithField("run_id", run.RunID).
					Warn("Failed to read result.json, skipping")

				continue
			}

			if resultData == nil {
				continue
			}

			AccumulateRunResult(&stats, resultData, run)
		}

		sortSuiteStats(&stats)
		allStats[suiteHash] = &stats
	}

	return allStats, nil
}

// WriteSuiteStats writes suite statistics to the appropriate file.
func WriteSuiteStats(resultsDir, suiteHash string, stats *SuiteStats, owner *fsutil.OwnerConfig) error {
	suitesDir := filepath.Join(resultsDir, "suites", suiteHash)

	if err := fsutil.MkdirAll(suitesDir, 0755, owner); err != nil {
		return fmt.Errorf("creating suites directory: %w", err)
	}

	statsPath := filepath.Join(suitesDir, "stats.json")

	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling suite stats: %w", err)
	}

	if err := fsutil.WriteFile(statsPath, data, 0644, owner); err != nil {
		return fmt.Errorf("writing stats.json: %w", err)
	}

	return nil
}
