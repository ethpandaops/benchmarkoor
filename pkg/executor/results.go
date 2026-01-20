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

// MethodStats contains aggregated statistics for a single method (int64 values).
type MethodStats struct {
	Count int64 `json:"count"`
	Min   int64 `json:"min"`
	Max   int64 `json:"max"`
	P50   int64 `json:"p50"`
	P95   int64 `json:"p95"`
	P99   int64 `json:"p99"`
	Mean  int64 `json:"mean"`
	Last  int64 `json:"last"`
}

// MethodStatsFloat contains aggregated statistics for a single method (float64 values).
type MethodStatsFloat struct {
	Count int64   `json:"count"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
	P50   float64 `json:"p50"`
	P95   float64 `json:"p95"`
	P99   float64 `json:"p99"`
	Mean  float64 `json:"mean"`
	Last  float64 `json:"last"`
}

// MarshalJSON customizes JSON output based on Count.
// If Count == 1, only count and last are included.
// If Count > 1, all statistics are included.
func (m *MethodStats) MarshalJSON() ([]byte, error) {
	if m.Count == 1 {
		return json.Marshal(struct {
			Count int64 `json:"count"`
			Last  int64 `json:"last"`
		}{
			Count: m.Count,
			Last:  m.Last,
		})
	}

	return json.Marshal(struct {
		Count int64 `json:"count"`
		Min   int64 `json:"min"`
		Max   int64 `json:"max"`
		P50   int64 `json:"p50"`
		P95   int64 `json:"p95"`
		P99   int64 `json:"p99"`
		Mean  int64 `json:"mean"`
		Last  int64 `json:"last"`
	}{
		Count: m.Count,
		Min:   m.Min,
		Max:   m.Max,
		P50:   m.P50,
		P95:   m.P95,
		P99:   m.P99,
		Mean:  m.Mean,
		Last:  m.Last,
	})
}

// MarshalJSON customizes JSON output based on Count.
// If Count == 1, only count and last are included.
// If Count > 1, all statistics are included.
func (m *MethodStatsFloat) MarshalJSON() ([]byte, error) {
	if m.Count == 1 {
		return json.Marshal(struct {
			Count int64   `json:"count"`
			Last  float64 `json:"last"`
		}{
			Count: m.Count,
			Last:  m.Last,
		})
	}

	return json.Marshal(struct {
		Count int64   `json:"count"`
		Min   float64 `json:"min"`
		Max   float64 `json:"max"`
		P50   float64 `json:"p50"`
		P95   float64 `json:"p95"`
		P99   float64 `json:"p99"`
		Mean  float64 `json:"mean"`
		Last  float64 `json:"last"`
	}{
		Count: m.Count,
		Min:   m.Min,
		Max:   m.Max,
		P50:   m.P50,
		P95:   m.P95,
		P99:   m.P99,
		Mean:  m.Mean,
		Last:  m.Last,
	})
}

// ResourceDelta contains resource usage delta for a single RPC call.
type ResourceDelta struct {
	MemoryDelta    int64  `json:"memory_delta_bytes"`
	CPUDeltaUsec   uint64 `json:"cpu_delta_usec"`
	DiskReadBytes  uint64 `json:"disk_read_bytes"`
	DiskWriteBytes uint64 `json:"disk_write_bytes"`
	DiskReadOps    uint64 `json:"disk_read_iops"`
	DiskWriteOps   uint64 `json:"disk_write_iops"`
}

// MethodResourceStats contains aggregated resource statistics for a method.
type MethodResourceStats struct {
	CPUUsec        *MethodStats `json:"cpu_usec,omitempty"`
	DiskReadBytes  *MethodStats `json:"disk_read_bytes,omitempty"`
	DiskWriteBytes *MethodStats `json:"disk_write_bytes,omitempty"`
	DiskReadOps    *MethodStats `json:"disk_read_iops,omitempty"`
	DiskWriteOps   *MethodStats `json:"disk_write_iops,omitempty"`
}

// MethodsAggregated contains aggregated stats for both times and MGas/s.
type MethodsAggregated struct {
	Times      map[string]*MethodStats         `json:"times"`
	MGasPerSec map[string]*MethodStatsFloat    `json:"mgas_s"`
	Resources  map[string]*MethodResourceStats `json:"resources,omitempty"`
}

// AggregatedStats contains the full aggregated output.
type AggregatedStats struct {
	TotalTime         int64              `json:"time_total"`
	GasUsedTotal      uint64             `json:"gas_used_total"`
	GasUsedTimeTotal  int64              `json:"gas_used_time_total"`
	Succeeded         int                `json:"success"`
	Failed            int                `json:"fail"`
	TotalMsgs         int                `json:"msg_count"`
	TotalCPUUsec      uint64             `json:"cpu_usec_total,omitempty"`
	TotalMemoryDelta  int64              `json:"memory_delta_total,omitempty"`
	TotalDiskRead     uint64             `json:"disk_read_total,omitempty"`
	TotalDiskWrite    uint64             `json:"disk_write_total,omitempty"`
	TotalDiskReadOps  uint64             `json:"disk_read_iops_total,omitempty"`
	TotalDiskWriteOps uint64             `json:"disk_write_iops_total,omitempty"`
	MethodStats       *MethodsAggregated `json:"method_stats"`
}

// TestEntry contains the result entry for a single test in the run result.
type TestEntry struct {
	Dir        string           `json:"dir"`
	Aggregated *AggregatedStats `json:"aggregated"`
}

// RunResult contains the aggregated results for all tests in a run.
type RunResult struct {
	Tests map[string]*TestEntry `json:"tests"`
}

// TestResult contains results for a single test file execution.
type TestResult struct {
	TestFile             string
	Responses            []string
	Times                []int64
	Statuses             []int // 0=success, 1=fail
	MGasPerSec           map[int]float64
	GasUsed              map[int]uint64
	Resources            map[int]*ResourceDelta
	MethodTimes          map[string][]int64
	MethodMGasPerSec     map[string][]float64
	MethodCPUUsec        map[string][]int64
	MethodDiskReadBytes  map[string][]int64
	MethodDiskWriteBytes map[string][]int64
	MethodDiskReadOps    map[string][]int64
	MethodDiskWriteOps   map[string][]int64
	Succeeded            int
	Failed               int
}

// ResultDetails contains per-call timing and status for JSON output.
type ResultDetails struct {
	DurationNS []int64                `json:"duration_ns"`
	Status     []int                  `json:"status"`
	MGasPerSec map[int]float64        `json:"mgas_s"`
	GasUsed    map[int]uint64         `json:"gas_used"`
	Resources  map[int]*ResourceDelta `json:"resources,omitempty"`
}

// NewTestResult creates a new TestResult.
func NewTestResult(testFile string) *TestResult {
	return &TestResult{
		TestFile:             testFile,
		Responses:            make([]string, 0),
		Times:                make([]int64, 0),
		Statuses:             make([]int, 0),
		MGasPerSec:           make(map[int]float64),
		GasUsed:              make(map[int]uint64),
		Resources:            make(map[int]*ResourceDelta),
		MethodTimes:          make(map[string][]int64),
		MethodMGasPerSec:     make(map[string][]float64),
		MethodCPUUsec:        make(map[string][]int64),
		MethodDiskReadBytes:  make(map[string][]int64),
		MethodDiskWriteBytes: make(map[string][]int64),
		MethodDiskReadOps:    make(map[string][]int64),
		MethodDiskWriteOps:   make(map[string][]int64),
	}
}

// AddResult adds a single RPC call result.
func (r *TestResult) AddResult(
	method, request, response string,
	elapsed int64,
	succeeded bool,
	resources *ResourceDelta,
) {
	// Get position before appending.
	pos := len(r.Times)

	r.Responses = append(r.Responses, response)
	r.Times = append(r.Times, elapsed)
	r.MethodTimes[method] = append(r.MethodTimes[method], elapsed)

	status := 0
	if !succeeded {
		status = 1
	}

	r.Statuses = append(r.Statuses, status)

	// Store resource delta if available.
	if resources != nil {
		r.Resources[pos] = resources
		r.MethodCPUUsec[method] = append(r.MethodCPUUsec[method], int64(resources.CPUDeltaUsec))
		r.MethodDiskReadBytes[method] = append(r.MethodDiskReadBytes[method], int64(resources.DiskReadBytes))
		r.MethodDiskWriteBytes[method] = append(r.MethodDiskWriteBytes[method], int64(resources.DiskWriteBytes))
		r.MethodDiskReadOps[method] = append(r.MethodDiskReadOps[method], int64(resources.DiskReadOps))
		r.MethodDiskWriteOps[method] = append(r.MethodDiskWriteOps[method], int64(resources.DiskWriteOps))
	}

	// Calculate MGas/s for successful engine_newPayload calls.
	if succeeded && strings.HasPrefix(method, "engine_newPayload") {
		if gasUsed, err := extractGasUsed(request); err == nil && elapsed > 0 {
			r.GasUsed[pos] = gasUsed
			mgasPerSec := float64(gasUsed) * 1000 / float64(elapsed)
			r.MGasPerSec[pos] = mgasPerSec
			r.MethodMGasPerSec[method] = append(r.MethodMGasPerSec[method], mgasPerSec)
		}
	}

	if succeeded {
		r.Succeeded++
	} else {
		r.Failed++
	}
}

// extractGasUsed extracts gasUsed from an engine_newPayload request.
func extractGasUsed(request string) (uint64, error) {
	var req struct {
		Params []json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal([]byte(request), &req); err != nil {
		return 0, err
	}

	if len(req.Params) == 0 {
		return 0, fmt.Errorf("no params")
	}

	var payload struct {
		GasUsed string `json:"gasUsed"`
	}
	if err := json.Unmarshal(req.Params[0], &payload); err != nil {
		return 0, err
	}

	// Parse hex string (0x prefixed).
	return strconv.ParseUint(strings.TrimPrefix(payload.GasUsed, "0x"), 16, 64)
}

// CalculateStats computes aggregated statistics from the test result.
func (r *TestResult) CalculateStats() *AggregatedStats {
	stats := &AggregatedStats{
		Succeeded: r.Succeeded,
		Failed:    r.Failed,
		TotalMsgs: len(r.Times),
		MethodStats: &MethodsAggregated{
			Times:      make(map[string]*MethodStats, len(r.MethodTimes)),
			MGasPerSec: make(map[string]*MethodStatsFloat, len(r.MethodMGasPerSec)),
		},
	}

	for _, t := range r.Times {
		stats.TotalTime += t
	}

	for idx, g := range r.GasUsed {
		if g == 0 {
			continue
		}

		stats.GasUsedTotal += g
		stats.GasUsedTimeTotal += r.Times[idx]
	}

	// Aggregate resource metrics.
	for _, res := range r.Resources {
		if res != nil {
			stats.TotalCPUUsec += res.CPUDeltaUsec
			stats.TotalMemoryDelta += res.MemoryDelta
			stats.TotalDiskRead += res.DiskReadBytes
			stats.TotalDiskWrite += res.DiskWriteBytes
			stats.TotalDiskReadOps += res.DiskReadOps
			stats.TotalDiskWriteOps += res.DiskWriteOps
		}
	}

	for method, times := range r.MethodTimes {
		stats.MethodStats.Times[method] = calculateMethodStats(times)
	}

	for method, values := range r.MethodMGasPerSec {
		stats.MethodStats.MGasPerSec[method] = calculateMethodStatsFloat(values)
	}

	// Aggregate per-method resource stats.
	if len(r.MethodCPUUsec) > 0 {
		stats.MethodStats.Resources = make(map[string]*MethodResourceStats, len(r.MethodCPUUsec))

		for method := range r.MethodCPUUsec {
			resStats := &MethodResourceStats{}

			if cpuUsec, ok := r.MethodCPUUsec[method]; ok && len(cpuUsec) > 0 {
				resStats.CPUUsec = calculateMethodStats(cpuUsec)
			}

			if diskRead, ok := r.MethodDiskReadBytes[method]; ok && len(diskRead) > 0 {
				resStats.DiskReadBytes = calculateMethodStats(diskRead)
			}

			if diskWrite, ok := r.MethodDiskWriteBytes[method]; ok && len(diskWrite) > 0 {
				resStats.DiskWriteBytes = calculateMethodStats(diskWrite)
			}

			if readOps, ok := r.MethodDiskReadOps[method]; ok && len(readOps) > 0 {
				resStats.DiskReadOps = calculateMethodStats(readOps)
			}

			if writeOps, ok := r.MethodDiskWriteOps[method]; ok && len(writeOps) > 0 {
				resStats.DiskWriteOps = calculateMethodStats(writeOps)
			}

			stats.MethodStats.Resources[method] = resStats
		}
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
		Last:  times[len(times)-1],
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

// calculateMethodStatsFloat computes statistics for a single method (float64 values).
func calculateMethodStatsFloat(values []float64) *MethodStatsFloat {
	if len(values) == 0 {
		return &MethodStatsFloat{}
	}

	// Sort values for percentile calculation.
	sorted := make([]float64, len(values))
	copy(sorted, values)
	slices.Sort(sorted)

	var sum float64
	for _, v := range sorted {
		sum += v
	}

	return &MethodStatsFloat{
		Count: int64(len(values)),
		Min:   sorted[0],
		Max:   sorted[len(sorted)-1],
		P50:   percentileFloat(sorted, 50),
		P95:   percentileFloat(sorted, 95),
		P99:   percentileFloat(sorted, 99),
		Mean:  sum / float64(len(values)),
		Last:  values[len(values)-1],
	}
}

// percentileFloat calculates the p-th percentile from sorted float64 values.
func percentileFloat(sorted []float64, p int) float64 {
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

	// Write .result-details.json file (replaces .times).
	detailsPath := basePath + ".result-details.json"
	details := ResultDetails{
		DurationNS: result.Times,
		Status:     result.Statuses,
		MGasPerSec: result.MGasPerSec,
		GasUsed:    result.GasUsed,
		Resources:  result.Resources,
	}

	detailsJSON, err := json.MarshalIndent(details, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling result details: %w", err)
	}

	if err := os.WriteFile(detailsPath, detailsJSON, 0644); err != nil {
		return fmt.Errorf("writing result details file: %w", err)
	}

	// Write .result-aggregated.json file.
	stats := result.CalculateStats()
	statsPath := basePath + ".result-aggregated.json"

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

	// Walk the results directory looking for .result-aggregated.json files.
	err := filepath.Walk(resultsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Only process aggregated stats files.
		if !strings.HasSuffix(path, ".result-aggregated.json") {
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

		// Remove .result-aggregated.json suffix to get the test name.
		testName := strings.TrimSuffix(relPath, ".result-aggregated.json")

		// Extract directory (e.g., "000752/test.txt" -> dir is "000752").
		dir := filepath.Dir(testName)
		if dir == "." {
			dir = ""
		}

		// Use the filename as the test key.
		testFile := filepath.Base(testName)

		// Use full path as map key to handle tests with same filename in different dirs.
		testKey := testFile
		if dir != "" {
			testKey = dir + "/" + testFile
		}

		result.Tests[testKey] = &TestEntry{
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
