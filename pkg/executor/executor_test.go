package executor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessTemplateParams(t *testing.T) {
	data := PostTestTemplateData{
		BlockHash:      "0xabc123",
		BlockNumber:    "1234",
		BlockNumberHex: "0x4d2",
	}

	tests := []struct {
		name     string
		params   []any
		expected []any
		wantErr  bool
	}{
		{
			name:     "nil params",
			params:   nil,
			expected: nil,
		},
		{
			name:     "empty params",
			params:   []any{},
			expected: []any{},
		},
		{
			name:     "string with block hash template",
			params:   []any{"{{.BlockHash}}"},
			expected: []any{"0xabc123"},
		},
		{
			name:     "string with block number template",
			params:   []any{"{{.BlockNumber}}"},
			expected: []any{"1234"},
		},
		{
			name:     "string with block number hex template",
			params:   []any{"{{.BlockNumberHex}}"},
			expected: []any{"0x4d2"},
		},
		{
			name:     "non-string values pass through",
			params:   []any{true, 42, 3.14},
			expected: []any{true, 42, 3.14},
		},
		{
			name:     "mixed params",
			params:   []any{"{{.BlockHash}}", false},
			expected: []any{"0xabc123", false},
		},
		{
			name:     "plain string without template",
			params:   []any{"latest"},
			expected: []any{"latest"},
		},
		{
			name: "nested map with templates",
			params: []any{
				map[string]any{
					"blockHash": "{{.BlockHash}}",
					"count":     42,
				},
			},
			expected: []any{
				map[string]any{
					"blockHash": "0xabc123",
					"count":     42,
				},
			},
		},
		{
			name: "nested slice with templates",
			params: []any{
				[]any{"{{.BlockNumber}}", "static"},
			},
			expected: []any{
				[]any{"1234", "static"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := processTemplateParams(tt.params, data)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestBuildJSONRPCPayload(t *testing.T) {
	payload, err := buildJSONRPCPayload("debug_traceBlockByNumber", []any{"0x4d2", map[string]any{"tracer": "callTracer"}})
	require.NoError(t, err)
	assert.Contains(t, payload, `"method":"debug_traceBlockByNumber"`)
	assert.Contains(t, payload, `"jsonrpc":"2.0"`)
	assert.Contains(t, payload, `"id":1`)
	assert.Contains(t, payload, `"0x4d2"`)
}

func TestExecutor_ProgressGasCounters(t *testing.T) {
	e := &executor{}

	// ResetProgress seeds the total and zeroes everything else, including
	// the new gas counters that the test loop accumulates as each test
	// completes.
	e.ResetProgress(42)

	got := e.GetProgress()
	assert.Equal(t, 42, got.TestsTotal)
	assert.Equal(t, 0, got.TestsPassed)
	assert.Equal(t, 0, got.TestsFailed)
	assert.Equal(t, int64(0), got.TotalGasUsed)
	assert.Equal(t, int64(0), got.TotalGasUsedDurationNs)

	// Simulate the test-loop accumulators firing for two completed tests.
	e.progressPassed.Add(1)
	e.progressGasUsed.Add(1_000_000)
	e.progressGasUsedDuration.Add(500_000_000)
	e.progressPassed.Add(1)
	e.progressGasUsed.Add(2_000_000)
	e.progressGasUsedDuration.Add(1_000_000_000)

	got = e.GetProgress()
	assert.Equal(t, 2, got.TestsPassed)
	assert.Equal(t, int64(3_000_000), got.TotalGasUsed)
	assert.Equal(t, int64(1_500_000_000), got.TotalGasUsedDurationNs)

	// A second ResetProgress (e.g. checkpoint-restore strategy seeding a
	// new logical run) must zero the new counters too.
	e.ResetProgress(10)
	got = e.GetProgress()
	assert.Equal(t, 10, got.TestsTotal)
	assert.Equal(t, int64(0), got.TotalGasUsed)
	assert.Equal(t, int64(0), got.TotalGasUsedDurationNs)
}

func TestExecutor_LiveTests(t *testing.T) {
	e := &executor{}
	e.ResetProgress(0)

	// Empty initially.
	assert.Empty(t, e.GetLiveTests())

	// Record a successful test.
	e.recordTestCompletion("alpha", true, 1_000_000, 500_000_000)
	live := e.GetLiveTests()
	require.Contains(t, live, "alpha")
	assert.True(t, live["alpha"].Passed)
	assert.Equal(t, int64(1_000_000), live["alpha"].GasUsed)
	assert.Equal(t, int64(500_000_000), live["alpha"].GasUsedDurationNs)

	// Failed tests are recorded with zero gas so the heatmap can render
	// fail tiles in real time.
	e.recordTestCompletion("beta", false, 0, 0)
	live = e.GetLiveTests()
	assert.Len(t, live, 2)
	assert.False(t, live["beta"].Passed)
	assert.Equal(t, int64(0), live["beta"].GasUsed)

	// GetLiveTests returns a defensive copy — mutating it must not leak.
	live["beta"] = LiveTestStats{Passed: true}
	assert.False(t, e.GetLiveTests()["beta"].Passed)

	// ResetProgress clears the per-test map alongside the existing
	// atomic counters.
	e.ResetProgress(5)
	assert.Empty(t, e.GetLiveTests())
}
