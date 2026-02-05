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
