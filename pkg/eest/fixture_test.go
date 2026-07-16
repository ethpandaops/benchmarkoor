package eest

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFixtureInfo_AggregatedOpcodeCount(t *testing.T) {
	tests := []struct {
		name string
		info *FixtureInfo
		want map[string]int
	}{
		{
			name: "nil info",
			info: nil,
			want: nil,
		},
		{
			name: "no opcode data",
			info: &FixtureInfo{},
			want: nil,
		},
		{
			name: "legacy flat opcode_count only",
			info: &FixtureInfo{
				OpcodeCount: map[string]int{"PUSH1": 3, "ADD": 1},
			},
			want: map[string]int{"PUSH1": 3, "ADD": 1},
		},
		{
			name: "metadata opcode_counts summed across payloads",
			info: &FixtureInfo{
				Metadata: &FixtureMetadata{
					OpcodeCounts: []map[string]int{
						{"PUSH1": 3, "ADD": 1},
						{"PUSH1": 2, "MUL": 4},
					},
				},
			},
			want: map[string]int{"PUSH1": 5, "ADD": 1, "MUL": 4},
		},
		{
			name: "nil entries (unavailable traces) are skipped",
			info: &FixtureInfo{
				Metadata: &FixtureMetadata{
					OpcodeCounts: []map[string]int{
						nil,
						{"PUSH1": 2},
						nil,
					},
				},
			},
			want: map[string]int{"PUSH1": 2},
		},
		{
			name: "metadata opcode_counts preferred over legacy flat",
			info: &FixtureInfo{
				OpcodeCount: map[string]int{"PUSH1": 99},
				Metadata: &FixtureMetadata{
					OpcodeCounts: []map[string]int{{"PUSH1": 2}},
				},
			},
			want: map[string]int{"PUSH1": 2},
		},
		{
			name: "all-nil metadata entries fall back to legacy flat",
			info: &FixtureInfo{
				OpcodeCount: map[string]int{"PUSH1": 3},
				Metadata: &FixtureMetadata{
					OpcodeCounts: []map[string]int{nil, nil},
				},
			},
			want: map[string]int{"PUSH1": 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.info.AggregatedOpcodeCount())
		})
	}
}

func TestFixtureInfo_ParsesMetadataOpcodeCounts(t *testing.T) {
	raw := `{
		"fixture-format": "blockchain_test_stateful_engine",
		"metadata": {
			"opcode_counts": [{"PUSH1": 3, "ADD": 1}, null, {"MUL": 2}]
		}
	}`

	var info FixtureInfo
	require.NoError(t, json.Unmarshal([]byte(raw), &info))
	require.NotNil(t, info.Metadata)
	require.Len(t, info.Metadata.OpcodeCounts, 3)
	assert.Equal(t, map[string]int{"PUSH1": 3, "ADD": 1}, info.Metadata.OpcodeCounts[0])
	assert.Nil(t, info.Metadata.OpcodeCounts[1])
	assert.Equal(t, map[string]int{"MUL": 2}, info.Metadata.OpcodeCounts[2])
	assert.Equal(t, map[string]int{"PUSH1": 3, "ADD": 1, "MUL": 2}, info.AggregatedOpcodeCount())
}
