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
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/fsutil"
	"github.com/sirupsen/logrus"
)

// Index contains the aggregated index of all benchmark runs.
type Index struct {
	Generated int64         `json:"generated"`
	Entries   []*IndexEntry `json:"entries"`
}

// IndexEntry contains summary information for a single benchmark run.
type IndexEntry struct {
	RunID             string          `json:"run_id"`
	Timestamp         int64           `json:"timestamp"`
	TimestampEnd      int64           `json:"timestamp_end,omitempty"`
	SuiteHash         string          `json:"suite_hash,omitempty"`
	Instance          *IndexInstance  `json:"instance"`
	Tests             *IndexTestStats `json:"tests"`
	Status            string          `json:"status,omitempty"`
	TerminationReason string          `json:"termination_reason,omitempty"`
}

// IndexInstance contains the client instance information for the index.
type IndexInstance struct {
	ID               string `json:"id"`
	Client           string `json:"client"`
	Image            string `json:"image"`
	RollbackStrategy string `json:"rollback_strategy,omitempty"`
}

// IndexTestStats contains aggregated test statistics for the index.
type IndexTestStats struct {
	TestsTotal  int              `json:"tests_total"`
	TestsPassed int              `json:"tests_passed"`
	TestsFailed int              `json:"tests_failed"`
	Steps       *IndexStepsStats `json:"steps"`
}

// IndexStepsStats contains per-step statistics.
type IndexStepsStats struct {
	Setup   *IndexStepStats `json:"setup,omitempty"`
	Test    *IndexStepStats `json:"test,omitempty"`
	Cleanup *IndexStepStats `json:"cleanup,omitempty"`
}

// IndexStepStats contains statistics for a single step type.
type IndexStepStats struct {
	Success         int             `json:"success"`
	Fail            int             `json:"fail"`
	Duration        int64           `json:"duration"`
	GasUsed         uint64          `json:"gas_used"`
	GasUsedDuration int64           `json:"gas_used_duration"`
	ResourceTotals  *ResourceTotals `json:"resource_totals,omitempty"`
}

// runConfigJSON is used to parse config.json files.
type runConfigJSON struct {
	Timestamp         int64  `json:"timestamp"`
	TimestampEnd      int64  `json:"timestamp_end,omitempty"`
	SuiteHash         string `json:"suite_hash,omitempty"`
	Status            string `json:"status,omitempty"`
	TerminationReason string `json:"termination_reason,omitempty"`
	Instance          struct {
		ID               string `json:"id"`
		Client           string `json:"client"`
		Image            string `json:"image"`
		RollbackStrategy string `json:"rollback_strategy,omitempty"`
	} `json:"instance"`
	TestCounts *struct {
		Total  int `json:"total"`
		Passed int `json:"passed"`
		Failed int `json:"failed"`
	} `json:"test_counts,omitempty"`
}

// GenerateIndex scans the results directory and builds an index from all runs.
func GenerateIndex(resultsDir string) (*Index, error) {
	runsDir := filepath.Join(resultsDir, "runs")

	entries, err := os.ReadDir(runsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return &Index{
				Generated: time.Now().Unix(),
				Entries:   make([]*IndexEntry, 0),
			}, nil
		}

		return nil, fmt.Errorf("reading runs directory: %w", err)
	}

	indexEntries := make([]*IndexEntry, 0, len(entries))

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		runDir := filepath.Join(runsDir, entry.Name())
		indexEntry, err := buildIndexEntry(runDir, entry.Name())

		if err != nil {
			// Skip runs that can't be parsed (incomplete or corrupted).
			continue
		}

		indexEntries = append(indexEntries, indexEntry)
	}

	// Override TestsTotal from suite summary when available.
	// This ensures tests_total reflects the suite's intended count,
	// not just the number of tests that produced results.
	suiteTestCounts := make(map[string]int, len(indexEntries))

	for _, ie := range indexEntries {
		if ie.SuiteHash == "" {
			continue
		}

		if _, ok := suiteTestCounts[ie.SuiteHash]; ok {
			continue
		}

		summaryPath := filepath.Join(
			resultsDir, "suites", ie.SuiteHash, "summary.json",
		)

		summaryData, err := os.ReadFile(summaryPath)
		if err != nil {
			continue
		}

		var suiteInfo SuiteInfo
		if err := json.Unmarshal(summaryData, &suiteInfo); err != nil {
			continue
		}

		suiteTestCounts[ie.SuiteHash] = len(suiteInfo.Tests)
	}

	for _, ie := range indexEntries {
		if count, ok := suiteTestCounts[ie.SuiteHash]; ok {
			ie.Tests.TestsTotal = count
		}
	}

	// Sort entries by timestamp, newest first.
	sort.Slice(indexEntries, func(i, j int) bool {
		return indexEntries[i].Timestamp > indexEntries[j].Timestamp
	})

	return &Index{
		Generated: time.Now().Unix(),
		Entries:   indexEntries,
	}, nil
}

// AggregateStepStats aggregates per-step statistics from a RunResult.
// Returns the step stats, total tests passed, and total tests failed.
func AggregateStepStats(result *RunResult) (*IndexStepsStats, int, int) {
	setupStats := &IndexStepStats{}
	testStepStats := &IndexStepStats{}
	cleanupStats := &IndexStepStats{}

	var setupResources, testResources, cleanupResources ResourceTotals

	hasSetupResources := false
	hasTestResources := false
	hasCleanupResources := false

	testsPassed := 0
	testsFailed := 0

	for _, test := range result.Tests {
		if test.Steps == nil {
			continue
		}

		// Aggregate setup stats.
		if test.Steps.Setup != nil && test.Steps.Setup.Aggregated != nil {
			agg := test.Steps.Setup.Aggregated
			setupStats.Success += agg.Succeeded
			setupStats.Fail += agg.Failed
			setupStats.Duration += agg.TotalTime
			setupStats.GasUsed += agg.GasUsedTotal
			setupStats.GasUsedDuration += agg.GasUsedTimeTotal

			if agg.ResourceTotals != nil {
				hasSetupResources = true
				setupResources.CPUUsec += agg.ResourceTotals.CPUUsec
				setupResources.MemoryDelta += agg.ResourceTotals.MemoryDelta
				setupResources.DiskReadBytes += agg.ResourceTotals.DiskReadBytes
				setupResources.DiskWriteBytes += agg.ResourceTotals.DiskWriteBytes
				setupResources.DiskReadIOPS += agg.ResourceTotals.DiskReadIOPS
				setupResources.DiskWriteIOPS += agg.ResourceTotals.DiskWriteIOPS

				if agg.ResourceTotals.MemoryBytes > setupResources.MemoryBytes {
					setupResources.MemoryBytes = agg.ResourceTotals.MemoryBytes
				}
			}
		}

		// Aggregate test stats.
		if test.Steps.Test != nil && test.Steps.Test.Aggregated != nil {
			agg := test.Steps.Test.Aggregated
			testStepStats.Success += agg.Succeeded
			testStepStats.Fail += agg.Failed
			testStepStats.Duration += agg.TotalTime
			testStepStats.GasUsed += agg.GasUsedTotal
			testStepStats.GasUsedDuration += agg.GasUsedTimeTotal

			if agg.ResourceTotals != nil {
				hasTestResources = true
				testResources.CPUUsec += agg.ResourceTotals.CPUUsec
				testResources.MemoryDelta += agg.ResourceTotals.MemoryDelta
				testResources.DiskReadBytes += agg.ResourceTotals.DiskReadBytes
				testResources.DiskWriteBytes += agg.ResourceTotals.DiskWriteBytes
				testResources.DiskReadIOPS += agg.ResourceTotals.DiskReadIOPS
				testResources.DiskWriteIOPS += agg.ResourceTotals.DiskWriteIOPS

				if agg.ResourceTotals.MemoryBytes > testResources.MemoryBytes {
					testResources.MemoryBytes = agg.ResourceTotals.MemoryBytes
				}
			}
		}

		// Aggregate cleanup stats.
		if test.Steps.Cleanup != nil && test.Steps.Cleanup.Aggregated != nil {
			agg := test.Steps.Cleanup.Aggregated
			cleanupStats.Success += agg.Succeeded
			cleanupStats.Fail += agg.Failed
			cleanupStats.Duration += agg.TotalTime
			cleanupStats.GasUsed += agg.GasUsedTotal
			cleanupStats.GasUsedDuration += agg.GasUsedTimeTotal

			if agg.ResourceTotals != nil {
				hasCleanupResources = true
				cleanupResources.CPUUsec += agg.ResourceTotals.CPUUsec
				cleanupResources.MemoryDelta += agg.ResourceTotals.MemoryDelta
				cleanupResources.DiskReadBytes += agg.ResourceTotals.DiskReadBytes
				cleanupResources.DiskWriteBytes += agg.ResourceTotals.DiskWriteBytes
				cleanupResources.DiskReadIOPS += agg.ResourceTotals.DiskReadIOPS
				cleanupResources.DiskWriteIOPS += agg.ResourceTotals.DiskWriteIOPS

				if agg.ResourceTotals.MemoryBytes > cleanupResources.MemoryBytes {
					cleanupResources.MemoryBytes = agg.ResourceTotals.MemoryBytes
				}
			}
		}

		// Determine test-level pass/fail.
		testFailed := false

		if test.Steps.Setup != nil && test.Steps.Setup.Aggregated != nil &&
			test.Steps.Setup.Aggregated.Failed > 0 {
			testFailed = true
		}

		if test.Steps.Test != nil && test.Steps.Test.Aggregated != nil &&
			test.Steps.Test.Aggregated.Failed > 0 {
			testFailed = true
		}

		if test.Steps.Cleanup != nil && test.Steps.Cleanup.Aggregated != nil &&
			test.Steps.Cleanup.Aggregated.Failed > 0 {
			testFailed = true
		}

		if testFailed {
			testsFailed++
		} else {
			testsPassed++
		}
	}

	// Assign resource totals if present.
	if hasSetupResources {
		setupStats.ResourceTotals = &setupResources
	}

	if hasTestResources {
		testStepStats.ResourceTotals = &testResources
	}

	if hasCleanupResources {
		cleanupStats.ResourceTotals = &cleanupResources
	}

	steps := &IndexStepsStats{}

	// Only include step stats if they have data.
	if setupStats.Success > 0 || setupStats.Fail > 0 {
		steps.Setup = setupStats
	}

	if testStepStats.Success > 0 || testStepStats.Fail > 0 {
		steps.Test = testStepStats
	}

	if cleanupStats.Success > 0 || cleanupStats.Fail > 0 {
		steps.Cleanup = cleanupStats
	}

	return steps, testsPassed, testsFailed
}

// buildIndexEntry creates an index entry from a single run directory.
func buildIndexEntry(runDir, runID string) (*IndexEntry, error) {
	configPath := filepath.Join(runDir, "config.json")

	configData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading config.json: %w", err)
	}

	resultPath := filepath.Join(runDir, "result.json")

	//nolint:gosec // result.json is a trusted local file written by the tool.
	resultData, _ := os.ReadFile(resultPath)

	return buildIndexEntryFromData(runID, configData, resultData)
}

// buildIndexEntryFromData creates an index entry from raw config and result
// JSON bytes. configData is required; resultData may be nil.
func buildIndexEntryFromData(
	runID string, configData, resultData []byte,
) (*IndexEntry, error) {
	var runConfig runConfigJSON
	if err := json.Unmarshal(configData, &runConfig); err != nil {
		return nil, fmt.Errorf("parsing config.json: %w", err)
	}

	testStats := &IndexTestStats{
		Steps: &IndexStepsStats{},
	}

	if len(resultData) > 0 {
		var runResult RunResult
		if err := json.Unmarshal(resultData, &runResult); err == nil {
			testStats.TestsTotal = len(runResult.Tests)

			steps, passed, failed := AggregateStepStats(&runResult)
			testStats.Steps = steps
			testStats.TestsPassed = passed
			testStats.TestsFailed = failed
		}
	}

	// Use test_counts from config.json when available (more accurate than
	// result.json counts, especially for crashed runs).
	if runConfig.TestCounts != nil {
		testStats.TestsTotal = runConfig.TestCounts.Total
		testStats.TestsPassed = runConfig.TestCounts.Passed
		testStats.TestsFailed = runConfig.TestCounts.Failed
	}

	return &IndexEntry{
		RunID:             runID,
		Timestamp:         runConfig.Timestamp,
		TimestampEnd:      runConfig.TimestampEnd,
		SuiteHash:         runConfig.SuiteHash,
		Status:            runConfig.Status,
		TerminationReason: runConfig.TerminationReason,
		Instance: &IndexInstance{
			ID:               runConfig.Instance.ID,
			Client:           runConfig.Instance.Client,
			Image:            runConfig.Instance.Image,
			RollbackStrategy: runConfig.Instance.RollbackStrategy,
		},
		Tests: testStats,
	}, nil
}

// WriteIndex writes the index to index.json in the runs subdirectory.
func WriteIndex(resultsDir string, index *Index, owner *fsutil.OwnerConfig) error {
	indexPath := filepath.Join(resultsDir, "runs", "index.json")

	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling index: %w", err)
	}

	if err := fsutil.WriteFile(indexPath, data, 0644, owner); err != nil {
		return fmt.Errorf("writing index.json: %w", err)
	}

	return nil
}

// IndexObjectReader abstracts reading objects from a remote store
// (e.g. S3) so that the executor package does not depend on upload.
type IndexObjectReader interface {
	// ListPrefixes lists immediate sub-prefixes under prefix.
	ListPrefixes(ctx context.Context, prefix string) ([]string, error)
	// GetObject returns the contents of key, or (nil, nil) when the
	// key does not exist.
	GetObject(ctx context.Context, key string) ([]byte, error)
}

// GenerateIndexFromS3 builds an Index by reading config.json and
// result.json from each run stored under runsPrefix in remote storage.
func GenerateIndexFromS3(
	ctx context.Context,
	log logrus.FieldLogger,
	reader IndexObjectReader,
	runsPrefix string,
) (*Index, error) {
	// Ensure the prefix ends with "/".
	if !strings.HasSuffix(runsPrefix, "/") {
		runsPrefix += "/"
	}

	prefixes, err := reader.ListPrefixes(ctx, runsPrefix)
	if err != nil {
		return nil, fmt.Errorf("listing run prefixes: %w", err)
	}

	indexEntries := make([]*IndexEntry, 0, len(prefixes))

	for _, prefix := range prefixes {
		// Extract run ID from prefix (e.g. "demo/results/runs/abc123/" → "abc123").
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

		resultData, err := reader.GetObject(ctx, prefix+"result.json")
		if err != nil {
			log.WithError(err).WithField("run_id", runID).
				Warn("Failed to read result.json, continuing without it")

			resultData = nil
		}

		entry, err := buildIndexEntryFromData(runID, configData, resultData)
		if err != nil {
			log.WithError(err).WithField("run_id", runID).
				Warn("Skipping run: failed to build index entry")

			continue
		}

		indexEntries = append(indexEntries, entry)
	}

	// Override TestsTotal from suite summaries when available.
	suitesPrefix := deriveSuitesPrefix(runsPrefix)
	suiteTestCounts := make(map[string]int, len(indexEntries))

	for _, ie := range indexEntries {
		if ie.SuiteHash == "" {
			continue
		}

		if _, ok := suiteTestCounts[ie.SuiteHash]; ok {
			continue
		}

		summaryKey := suitesPrefix + ie.SuiteHash + "/summary.json"

		summaryData, err := reader.GetObject(ctx, summaryKey)
		if err != nil || summaryData == nil {
			continue
		}

		var suiteInfo SuiteInfo
		if err := json.Unmarshal(summaryData, &suiteInfo); err != nil {
			continue
		}

		suiteTestCounts[ie.SuiteHash] = len(suiteInfo.Tests)
	}

	for _, ie := range indexEntries {
		if count, ok := suiteTestCounts[ie.SuiteHash]; ok {
			ie.Tests.TestsTotal = count
		}
	}

	// Sort entries by timestamp, newest first.
	sort.Slice(indexEntries, func(i, j int) bool {
		return indexEntries[i].Timestamp > indexEntries[j].Timestamp
	})

	return &Index{
		Generated: time.Now().Unix(),
		Entries:   indexEntries,
	}, nil
}

// deriveSuitesPrefix replaces the last path segment of a runs prefix
// with "suites". For example "demo/results/runs/" → "demo/results/suites/".
func deriveSuitesPrefix(runsPrefix string) string {
	trimmed := strings.TrimRight(runsPrefix, "/")
	parent := path.Dir(trimmed)

	if parent == "." || parent == "" {
		return "suites/"
	}

	return parent + "/suites/"
}
