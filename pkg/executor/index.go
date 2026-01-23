package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
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
	SuiteHash         string          `json:"suite_hash,omitempty"`
	Instance          *IndexInstance  `json:"instance"`
	Tests             *IndexTestStats `json:"tests"`
	Status            string          `json:"status,omitempty"`
	TerminationReason string          `json:"termination_reason,omitempty"`
}

// IndexInstance contains the client instance information for the index.
type IndexInstance struct {
	ID     string `json:"id"`
	Client string `json:"client"`
	Image  string `json:"image"`
}

// IndexTestStats contains aggregated test statistics for the index.
type IndexTestStats struct {
	Steps *IndexStepsStats `json:"steps"`
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
	SuiteHash         string `json:"suite_hash,omitempty"`
	Status            string `json:"status,omitempty"`
	TerminationReason string `json:"termination_reason,omitempty"`
	Instance          struct {
		ID     string `json:"id"`
		Client string `json:"client"`
		Image  string `json:"image"`
	} `json:"instance"`
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

	// Sort entries by timestamp, newest first.
	sort.Slice(indexEntries, func(i, j int) bool {
		return indexEntries[i].Timestamp > indexEntries[j].Timestamp
	})

	return &Index{
		Generated: time.Now().Unix(),
		Entries:   indexEntries,
	}, nil
}

// buildIndexEntry creates an index entry from a single run directory.
func buildIndexEntry(runDir, runID string) (*IndexEntry, error) {
	// Read config.json.
	configPath := filepath.Join(runDir, "config.json")

	configData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading config.json: %w", err)
	}

	var runConfig runConfigJSON
	if err := json.Unmarshal(configData, &runConfig); err != nil {
		return nil, fmt.Errorf("parsing config.json: %w", err)
	}

	// Read result.json and aggregate stats.
	resultPath := filepath.Join(runDir, "result.json")

	testStats := &IndexTestStats{
		Steps: &IndexStepsStats{},
	}

	resultData, err := os.ReadFile(resultPath)
	if err == nil {
		var runResult RunResult
		if err := json.Unmarshal(resultData, &runResult); err == nil {
			// Initialize per-step stats.
			setupStats := &IndexStepStats{}
			testStepStats := &IndexStepStats{}
			cleanupStats := &IndexStepStats{}

			var setupResources, testResources, cleanupResources ResourceTotals

			hasSetupResources := false
			hasTestResources := false
			hasCleanupResources := false

			for _, test := range runResult.Tests {
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
					}
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

			// Only include step stats if they have data.
			if setupStats.Success > 0 || setupStats.Fail > 0 {
				testStats.Steps.Setup = setupStats
			}

			if testStepStats.Success > 0 || testStepStats.Fail > 0 {
				testStats.Steps.Test = testStepStats
			}

			if cleanupStats.Success > 0 || cleanupStats.Fail > 0 {
				testStats.Steps.Cleanup = cleanupStats
			}
		}
	}

	return &IndexEntry{
		RunID:             runID,
		Timestamp:         runConfig.Timestamp,
		SuiteHash:         runConfig.SuiteHash,
		Status:            runConfig.Status,
		TerminationReason: runConfig.TerminationReason,
		Instance: &IndexInstance{
			ID:     runConfig.Instance.ID,
			Client: runConfig.Instance.Client,
			Image:  runConfig.Instance.Image,
		},
		Tests: testStats,
	}, nil
}

// WriteIndex writes the index to index.json in the runs subdirectory.
func WriteIndex(resultsDir string, index *Index) error {
	indexPath := filepath.Join(resultsDir, "runs", "index.json")

	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling index: %w", err)
	}

	if err := os.WriteFile(indexPath, data, 0644); err != nil {
		return fmt.Errorf("writing index.json: %w", err)
	}

	return nil
}
