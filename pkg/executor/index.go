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
	RunID     string          `json:"run_id"`
	Timestamp int64           `json:"timestamp"`
	SuiteHash string          `json:"suite_hash,omitempty"`
	Instance  *IndexInstance  `json:"instance"`
	Tests     *IndexTestStats `json:"tests"`
}

// IndexInstance contains the client instance information for the index.
type IndexInstance struct {
	ID     string `json:"id"`
	Client string `json:"client"`
	Image  string `json:"image"`
}

// IndexTestStats contains aggregated test statistics for the index.
type IndexTestStats struct {
	Success         int             `json:"success"`
	Fail            int             `json:"fail"`
	Duration        int64           `json:"duration"`
	GasUsed         uint64          `json:"gas_used"`
	GasUsedDuration int64           `json:"gas_used_duration"`
	ResourceTotals  *ResourceTotals `json:"resource_totals,omitempty"`
}

// runConfigJSON is used to parse config.json files.
type runConfigJSON struct {
	Timestamp int64  `json:"timestamp"`
	SuiteHash string `json:"suite_hash,omitempty"`
	Instance  struct {
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

	testStats := &IndexTestStats{}

	resultData, err := os.ReadFile(resultPath)
	if err == nil {
		var runResult RunResult
		if err := json.Unmarshal(resultData, &runResult); err == nil {
			var resourceTotals ResourceTotals
			hasResources := false

			for _, test := range runResult.Tests {
				if test.Aggregated != nil {
					testStats.Success += test.Aggregated.Succeeded
					testStats.Fail += test.Aggregated.Failed
					testStats.Duration += test.Aggregated.TotalTime
					testStats.GasUsed += test.Aggregated.GasUsedTotal
					testStats.GasUsedDuration += test.Aggregated.GasUsedTimeTotal

					if test.Aggregated.ResourceTotals != nil {
						hasResources = true
						resourceTotals.CPUUsec += test.Aggregated.ResourceTotals.CPUUsec
						resourceTotals.MemoryDelta += test.Aggregated.ResourceTotals.MemoryDelta
						resourceTotals.DiskReadBytes += test.Aggregated.ResourceTotals.DiskReadBytes
						resourceTotals.DiskWriteBytes += test.Aggregated.ResourceTotals.DiskWriteBytes
						resourceTotals.DiskReadIOPS += test.Aggregated.ResourceTotals.DiskReadIOPS
						resourceTotals.DiskWriteIOPS += test.Aggregated.ResourceTotals.DiskWriteIOPS
					}
				}
			}

			if hasResources {
				testStats.ResourceTotals = &resourceTotals
			}
		}
	}

	return &IndexEntry{
		RunID:     runID,
		Timestamp: runConfig.Timestamp,
		SuiteHash: runConfig.SuiteHash,
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
