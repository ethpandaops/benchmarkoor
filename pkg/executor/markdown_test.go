package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{
			name:     "sub-second",
			duration: 500 * time.Millisecond,
			expected: "500ms",
		},
		{
			name:     "seconds only",
			duration: 45 * time.Second,
			expected: "45s",
		},
		{
			name:     "minutes and seconds",
			duration: 10*time.Minute + 8*time.Second,
			expected: "10m 8s",
		},
		{
			name:     "hours minutes seconds",
			duration: 2*time.Hour + 30*time.Minute + 15*time.Second,
			expected: "2h 30m 15s",
		},
		{
			name:     "zero",
			duration: 0,
			expected: "0s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, formatDuration(tt.duration))
		})
	}
}

func TestFormatDurationNs(t *testing.T) {
	tests := []struct {
		name     string
		ns       int64
		expected string
	}{
		{
			name:     "zero",
			ns:       0,
			expected: "0s",
		},
		{
			name:     "one second",
			ns:       int64(time.Second),
			expected: "1s",
		},
		{
			name:     "ten minutes eight seconds",
			ns:       int64(10*time.Minute + 8*time.Second),
			expected: "10m 8s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, formatDurationNs(tt.ns))
		})
	}
}

func TestFormatGas(t *testing.T) {
	tests := []struct {
		name     string
		gas      uint64
		expected string
	}{
		{name: "zero", gas: 0, expected: "0"},
		{name: "small", gas: 100, expected: "100"},
		{name: "thousands", gas: 1234, expected: "1,234"},
		{name: "millions", gas: 1234567, expected: "1,234,567"},
		{name: "billions", gas: 1234567890, expected: "1,234,567,890"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, formatGas(tt.gas))
		})
	}
}

func TestFormatMGasPerSec(t *testing.T) {
	tests := []struct {
		name       string
		gas        uint64
		durationNs int64
		expected   string
	}{
		{
			name:       "zero gas",
			gas:        0,
			durationNs: int64(time.Second),
			expected:   "-",
		},
		{
			name:       "zero duration",
			gas:        1_000_000,
			durationNs: 0,
			expected:   "-",
		},
		{
			name:       "one mgas in one second",
			gas:        1_000_000,
			durationNs: int64(time.Second),
			expected:   "1.00",
		},
		{
			name:       "100 mgas in 10 seconds",
			gas:        100_000_000,
			durationNs: int64(10 * time.Second),
			expected:   "10.00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected,
				formatMGasPerSec(tt.gas, tt.durationNs))
		})
	}
}

func TestCollectFailedTests(t *testing.T) {
	t.Run("nil result", func(t *testing.T) {
		assert.Nil(t, collectFailedTests(nil))
	})

	t.Run("no failures", func(t *testing.T) {
		result := &RunResult{
			Tests: map[string]*TestEntry{
				"test1": {
					Steps: &StepsResult{
						Test: &StepResult{
							Aggregated: &AggregatedStats{
								Succeeded: 1,
								Failed:    0,
							},
						},
					},
				},
			},
		}
		assert.Empty(t, collectFailedTests(result))
	})

	t.Run("with failures sorted", func(t *testing.T) {
		result := &RunResult{
			Tests: map[string]*TestEntry{
				"zz_test": {
					Steps: &StepsResult{
						Test: &StepResult{
							Aggregated: &AggregatedStats{Failed: 1},
						},
					},
				},
				"aa_test": {
					Steps: &StepsResult{
						Setup: &StepResult{
							Aggregated: &AggregatedStats{Failed: 1},
						},
						Test: &StepResult{
							Aggregated: &AggregatedStats{Failed: 2},
						},
					},
				},
			},
		}

		failed := collectFailedTests(result)
		require.Len(t, failed, 2)
		assert.Equal(t, "aa_test", failed[0].Name)
		assert.Equal(t, []string{"setup", "test"}, failed[0].FailedSteps)
		assert.Equal(t, "zz_test", failed[1].Name)
		assert.Equal(t, []string{"test"}, failed[1].FailedSteps)
	})
}

func TestGenerateRunMarkdown(t *testing.T) {
	t.Run("full run with result", func(t *testing.T) {
		dir := t.TempDir()
		writeFixtureConfig(t, dir)
		writeFixtureResult(t, dir)

		md, err := GenerateRunMarkdown(dir, "test_run_123", 65000)
		require.NoError(t, err)

		// Check sections are present.
		assert.Contains(t, md, "# Benchmark Run: test_run_123")
		assert.Contains(t, md, "## Overview")
		assert.Contains(t, md, "| Status | completed |")
		assert.Contains(t, md, "| Client | geth |")
		assert.Contains(t, md, "| Image | `ethpandaops/geth:latest` |")
		assert.Contains(t, md, "| Client Version | Geth/v1.17.0 |")
		assert.Contains(t, md, "## System")
		assert.Contains(t, md, "| Hostname | test-host |")
		assert.Contains(t, md, "| CPU | AMD Ryzen 9 |")
		assert.Contains(t, md, "## Resource Limits")
		assert.Contains(t, md, "| CPU Set | 0-3 |")
		assert.Contains(t, md, "## Metadata")
		assert.Contains(t, md, "| env | staging |")
		assert.Contains(t, md, "## Start Block")
		assert.Contains(t, md, "| Number | 100 |")
		assert.Contains(t, md, "## Test Results")
		assert.Contains(t, md, "## Aggregated Step Stats")
		assert.Contains(t, md, "| Test |")
	})

	t.Run("missing result.json", func(t *testing.T) {
		dir := t.TempDir()
		writeFixtureConfig(t, dir)

		md, err := GenerateRunMarkdown(dir, "crashed_run", 65000)
		require.NoError(t, err)
		assert.Contains(t, md, "# Benchmark Run: crashed_run")
		assert.Contains(t, md, "## Test Results")
		// Should still work but with zero counts.
		assert.Contains(t, md, "| 10 | 8 | 2 |")
	})

	t.Run("missing config.json", func(t *testing.T) {
		dir := t.TempDir()

		_, err := GenerateRunMarkdown(dir, "bad_run", 65000)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reading config.json")
	})

	t.Run("no resource limits or metadata", func(t *testing.T) {
		dir := t.TempDir()
		writeMinimalConfig(t, dir)

		md, err := GenerateRunMarkdown(dir, "minimal_run", 65000)
		require.NoError(t, err)
		assert.NotContains(t, md, "## Resource Limits")
		assert.NotContains(t, md, "## Metadata")
		assert.NotContains(t, md, "## Start Block")
	})
}

func TestGenerateRunMarkdownCharLimit(t *testing.T) {
	dir := t.TempDir()
	writeMinimalConfig(t, dir)

	// Create a result with many failed tests with long names.
	result := &RunResult{
		Tests: make(map[string]*TestEntry, 500),
	}

	for i := range 500 {
		name := fmt.Sprintf(
			"benchmark/compute/precompile/test_very_long_name_%04d"+
				"_with_extra_padding_to_make_it_really_long", i)
		result.Tests[name] = &TestEntry{
			Steps: &StepsResult{
				Test: &StepResult{
					Aggregated: &AggregatedStats{Failed: 1},
				},
			},
		}
	}

	resultData, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)

	err = os.WriteFile(
		filepath.Join(dir, "result.json"), resultData, 0644)
	require.NoError(t, err)

	md, err := GenerateRunMarkdown(dir, "limit_run", 10000)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(md), 10000)
	assert.Contains(t, md, "more failed test(s) not shown")
}

// writeFixtureConfig writes a comprehensive config.json for testing.
func writeFixtureConfig(t *testing.T, dir string) {
	t.Helper()

	turbo := true
	freqKHz := uint64(4500000)

	cfg := markdownRunConfig{
		Timestamp:    1700000000,
		TimestampEnd: 1700000608,
		SuiteHash:    "abc123def456",
		Status:       "completed",
		System: &markdownSystemInfo{
			Hostname:      "test-host",
			OS:            "linux",
			Platform:      "debian",
			KernelVersion: "6.1.0",
			Arch:          "x86_64",
			CPUModel:      "AMD Ryzen 9",
			CPUCores:      16,
			CPUMhz:        5756.0,
			MemoryTotalGB: 64.0,
		},
		Instance: &markdownInstance{
			ID:            "geth",
			Client:        "geth",
			Image:         "ethpandaops/geth:latest",
			ClientVersion: "Geth/v1.17.0",
			ResourceLimits: &markdownResourceLimits{
				CpusetCpus:    "0-3",
				Memory:        "16g",
				CPUFreqKHz:    &freqKHz,
				CPUTurboBoost: &turbo,
			},
		},
		Metadata: &markdownMetadata{
			Labels: map[string]string{
				"env":    "staging",
				"region": "us-east",
			},
		},
		StartBlock: &markdownStartBlock{
			Number:    100,
			Hash:      "0xabc123",
			StateRoot: "0xdef456",
		},
		TestCounts: &markdownTestCounts{
			Total:  10,
			Passed: 8,
			Failed: 2,
		},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(dir, "config.json"), data, 0644)
	require.NoError(t, err)
}

// writeFixtureResult writes a result.json with test data for testing.
func writeFixtureResult(t *testing.T, dir string) {
	t.Helper()

	result := &RunResult{
		Tests: map[string]*TestEntry{
			"passing_test": {
				Steps: &StepsResult{
					Test: &StepResult{
						Aggregated: &AggregatedStats{
							TotalTime:        int64(5 * time.Second),
							GasUsedTotal:     50_000_000,
							GasUsedTimeTotal: int64(5 * time.Second),
							Succeeded:        10,
							Failed:           0,
						},
					},
				},
			},
			"failing_test": {
				Steps: &StepsResult{
					Test: &StepResult{
						Aggregated: &AggregatedStats{
							TotalTime: int64(1 * time.Second),
							Succeeded: 5,
							Failed:    3,
						},
					},
				},
			},
		},
	}

	data, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(dir, "result.json"), data, 0644)
	require.NoError(t, err)
}

// writeMinimalConfig writes a minimal config.json without optional fields.
func writeMinimalConfig(t *testing.T, dir string) {
	t.Helper()

	cfg := markdownRunConfig{
		Timestamp: 1700000000,
		Status:    "completed",
		Instance: &markdownInstance{
			ID:     "geth",
			Client: "geth",
			Image:  "ethpandaops/geth:latest",
		},
		TestCounts: &markdownTestCounts{
			Total:  10,
			Passed: 8,
			Failed: 2,
		},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(dir, "config.json"), data, 0644)
	require.NoError(t, err)
}

func TestWriteFailedTestsTruncation(t *testing.T) {
	failed := make([]failedTestInfo, 100)
	for i := range 100 {
		failed[i] = failedTestInfo{
			Name:        fmt.Sprintf("very_long_test_name_%d", i),
			FailedSteps: []string{"test"},
		}
	}

	var sb strings.Builder

	// Start with some content already in the builder.
	sb.WriteString("# Header\n\n")

	writeFailedTests(&sb, failed, 500)

	output := sb.String()
	assert.LessOrEqual(t, len(output), 500)
	assert.Contains(t, output, "more failed test(s) not shown")
}
