package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// MethodStats contains aggregated statistics for a single method.
type MethodStats struct {
	Count int64 `json:"count"`
	Min   int64 `json:"min"`
	Max   int64 `json:"max"`
	P50   int64 `json:"p50"`
	P95   int64 `json:"p95"`
	P99   int64 `json:"p99"`
	Mean  int64 `json:"mean"`
}

// AggregatedStats contains the full aggregated output.
type AggregatedStats struct {
	TotalTime int64                   `json:"TotalTime"`
	Succeeded int                     `json:"Succeeded"`
	Failed    int                     `json:"Failed"`
	TotalMsgs int                     `json:"TotalMsgs"`
	Methods   map[string]*MethodStats `json:"Methods"`
}

// TestEntry contains the result entry for a single test in the run result.
type TestEntry struct {
	Dir        string           `json:"dir"`
	Aggregated *AggregatedStats `json:"aggregated"`
}

// RunResult contains the aggregated results for all tests in a run.
type RunResult struct {
	SuiteHash string                `json:"suite_hash,omitempty"`
	Tests     map[string]*TestEntry `json:"tests"`
}

// TestResult contains results for a single test file execution.
type TestResult struct {
	TestFile    string
	Responses   []string
	Times       []int64
	MethodTimes map[string][]int64
	Succeeded   int
	Failed      int
}

// NewTestResult creates a new TestResult.
func NewTestResult(testFile string) *TestResult {
	return &TestResult{
		TestFile:    testFile,
		Responses:   make([]string, 0),
		Times:       make([]int64, 0),
		MethodTimes: make(map[string][]int64),
	}
}

// AddResult adds a single RPC call result.
func (r *TestResult) AddResult(method, response string, elapsed int64, succeeded bool) {
	r.Responses = append(r.Responses, response)
	r.Times = append(r.Times, elapsed)
	r.MethodTimes[method] = append(r.MethodTimes[method], elapsed)

	if succeeded {
		r.Succeeded++
	} else {
		r.Failed++
	}
}

// CalculateStats computes aggregated statistics from the test result.
func (r *TestResult) CalculateStats() *AggregatedStats {
	stats := &AggregatedStats{
		Succeeded: r.Succeeded,
		Failed:    r.Failed,
		TotalMsgs: len(r.Times),
		Methods:   make(map[string]*MethodStats, len(r.MethodTimes)),
	}

	for _, t := range r.Times {
		stats.TotalTime += t
	}

	for method, times := range r.MethodTimes {
		stats.Methods[method] = calculateMethodStats(times)
	}

	return stats
}

// calculateMethodStats computes statistics for a single method.
func calculateMethodStats(times []int64) *MethodStats {
	if len(times) == 0 {
		return &MethodStats{}
	}

	// Sort times for percentile calculation.
	sorted := make([]int64, len(times))
	copy(sorted, times)
	slices.Sort(sorted)

	var sum int64
	for _, t := range sorted {
		sum += t
	}

	return &MethodStats{
		Count: int64(len(times)),
		Min:   sorted[0],
		Max:   sorted[len(sorted)-1],
		P50:   percentile(sorted, 50),
		P95:   percentile(sorted, 95),
		P99:   percentile(sorted, 99),
		Mean:  sum / int64(len(times)),
	}
}

// percentile calculates the p-th percentile from sorted values.
func percentile(sorted []int64, p int) int64 {
	if len(sorted) == 0 {
		return 0
	}

	if len(sorted) == 1 {
		return sorted[0]
	}

	// Use nearest-rank method.
	idx := (p * len(sorted)) / 100
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}

	return sorted[idx]
}

// WriteResults writes the three output files for a test.
func WriteResults(resultDir, testName string, result *TestResult) error {
	// Ensure the directory structure exists.
	testDir := filepath.Dir(filepath.Join(resultDir, testName))
	if err := os.MkdirAll(testDir, 0755); err != nil {
		return fmt.Errorf("creating test result directory: %w", err)
	}

	basePath := filepath.Join(resultDir, testName)

	// Write .response file.
	responsePath := basePath + ".response"
	if err := os.WriteFile(responsePath, []byte(strings.Join(result.Responses, "\n")+"\n"), 0644); err != nil {
		return fmt.Errorf("writing response file: %w", err)
	}

	// Write .times file.
	timesPath := basePath + ".times"
	timesContent := make([]string, len(result.Times))
	for i, t := range result.Times {
		timesContent[i] = strconv.FormatInt(t, 10)
	}

	if err := os.WriteFile(timesPath, []byte(strings.Join(timesContent, "\n")+"\n"), 0644); err != nil {
		return fmt.Errorf("writing times file: %w", err)
	}

	// Write .times_aggregated.json file.
	stats := result.CalculateStats()
	statsPath := basePath + ".times_aggregated.json"

	statsJSON, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling stats: %w", err)
	}

	if err := os.WriteFile(statsPath, statsJSON, 0644); err != nil {
		return fmt.Errorf("writing stats file: %w", err)
	}

	return nil
}

// GenerateRunResult scans a results directory and builds a RunResult from all aggregated files.
func GenerateRunResult(resultsDir string) (*RunResult, error) {
	result := &RunResult{
		Tests: make(map[string]*TestEntry),
	}

	// Walk the results directory looking for .times_aggregated.json files.
	err := filepath.Walk(resultsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Only process aggregated stats files.
		if !strings.HasSuffix(path, ".times_aggregated.json") {
			return nil
		}

		// Read and parse the aggregated stats file.
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		var stats AggregatedStats
		if err := json.Unmarshal(data, &stats); err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}

		// Extract relative path from resultsDir.
		relPath, err := filepath.Rel(resultsDir, path)
		if err != nil {
			relPath = path
		}

		// Remove .times_aggregated.json suffix to get the test name.
		testName := strings.TrimSuffix(relPath, ".times_aggregated.json")

		// Extract directory (e.g., "000752/test.txt" -> dir is "000752").
		dir := filepath.Dir(testName)
		if dir == "." {
			dir = ""
		}

		// Use just the filename as the test key.
		testFile := filepath.Base(testName)

		result.Tests[testFile] = &TestEntry{
			Dir:        dir,
			Aggregated: &stats,
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking results directory: %w", err)
	}

	return result, nil
}

// WriteRunResult writes the run result to result.json in the results directory.
func WriteRunResult(resultsDir string, result *RunResult) error {
	resultPath := filepath.Join(resultsDir, "result.json")

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling run result: %w", err)
	}

	if err := os.WriteFile(resultPath, data, 0644); err != nil {
		return fmt.Errorf("writing result.json: %w", err)
	}

	return nil
}
