package blocklog

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEthrexParser_ParseLine(t *testing.T) {
	parser := NewEthrexParser()

	tests := []struct {
		name   string
		line   string
		wantOK bool
		// checkJSON is called when wantOK is true to verify the parsed output.
		checkJSON func(t *testing.T, data map[string]any)
	}{
		{
			name:   "pipeline header with hash",
			line:   `2026-05-27T14:00:00.123456Z  INFO [METRIC] BLOCK 24358000 0xc957abc123 | 1.234 Ggas/s | 567.00 ms | 150 txs | 700 Mgas (93%)`,
			wantOK: true,
			checkJSON: func(t *testing.T, data map[string]any) {
				t.Helper()

				assert.Equal(t, "info", data["level"])
				assert.Equal(t, "Block execution throughput", data["msg"])

				block := data["block"].(map[string]any)
				assert.Equal(t, float64(24358000), block["number"])
				assert.Equal(t, "0xc957abc123", block["hash"])
				assert.Equal(t, float64(700), block["gas_used_mgas"])
				assert.Equal(t, float64(93), block["gas_used_pct"])
				assert.Equal(t, float64(150), block["tx_count"])

				throughput := data["throughput"].(map[string]any)
				assert.Equal(t, 1.234, throughput["ggas_per_sec"])

				timing := data["timing"].(map[string]any)
				assert.Equal(t, float64(567), timing["total_ms"])
			},
		},
		{
			name:   "current ethrex header without hash still parses (but lacks block.hash)",
			line:   `2026-05-27T14:00:00.123456Z  INFO [METRIC] BLOCK 24358000 | 1.234 Ggas/s | 567.00 ms | 150 txs | 700 Mgas (93%)`,
			wantOK: true,
			checkJSON: func(t *testing.T, data map[string]any) {
				t.Helper()

				block := data["block"].(map[string]any)
				assert.Equal(t, float64(24358000), block["number"])
				_, hasHash := block["hash"]
				assert.False(t, hasHash, "no hash should be set when the log omits it")
			},
		},
		{
			name:   "zero-gas block",
			line:   `2026-05-27T14:00:00.123456Z  INFO [METRIC] BLOCK 100 0xdeadbeef | 0.000 Ggas/s | 12.00 ms | 0 txs | 0 Mgas (0%)`,
			wantOK: true,
			checkJSON: func(t *testing.T, data map[string]any) {
				t.Helper()

				block := data["block"].(map[string]any)
				assert.Equal(t, float64(100), block["number"])
				assert.Equal(t, "0xdeadbeef", block["hash"])
				assert.Equal(t, float64(0), block["tx_count"])

				timing := data["timing"].(map[string]any)
				assert.Equal(t, float64(12), timing["total_ms"])
			},
		},
		{
			name:   "header with ANSI escape codes",
			line:   "\x1b[2m2026-05-27T14:00:00.123456Z\x1b[0m \x1b[32m INFO\x1b[0m [METRIC] BLOCK 24358001 0x9f566dc9f8beb533 | 2.500 Ggas/s | 400.00 ms | 200 txs | 1000 Mgas (50%)",
			wantOK: true,
			checkJSON: func(t *testing.T, data map[string]any) {
				t.Helper()

				block := data["block"].(map[string]any)
				assert.Equal(t, float64(24358001), block["number"])
				assert.Equal(t, "0x9f566dc9f8beb533", block["hash"])
				assert.Equal(t, float64(1000), block["gas_used_mgas"])

				throughput := data["throughput"].(map[string]any)
				assert.Equal(t, 2.5, throughput["ggas_per_sec"])
			},
		},
		{
			name:   "phase sub-line is not the header",
			line:   `2026-05-27T14:00:00.123456Z  INFO   |- exec:     450.00 ms  (80%) << BOTTLENECK`,
			wantOK: false,
		},
		{
			name:   "non-pipeline import throughput line is not matched",
			line:   `2026-05-27T14:00:00.123456Z  INFO [METRIC] BLOCK EXECUTION THROUGHPUT (24358000): 1.234 Ggas/s TIME SPENT: 567 ms. Gas Used: 0.700 (93%), #Txs: 150.`,
			wantOK: false,
		},
		{
			name:   "block building throughput is not block execution",
			line:   `2026-05-27T14:00:00.123456Z  INFO [METRIC] BLOCK BUILDING THROUGHPUT: 1.234 Gigagas/s TIME SPENT: 567 msecs`,
			wantOK: false,
		},
		{
			name:   "unrelated ethrex info log",
			line:   `2026-05-27T14:00:00.123456Z  INFO Initiating blockchain with levm`,
			wantOK: false,
		},
		{
			name:   "empty line",
			line:   "",
			wantOK: false,
		},
		{
			name:   "random text",
			line:   "some random log output that does not match",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := parser.ParseLine(tt.line)

			assert.Equal(t, tt.wantOK, ok)

			if tt.wantOK {
				require.NotNil(t, result)

				var parsed map[string]any
				err := json.Unmarshal(result, &parsed)
				require.NoError(t, err)

				tt.checkJSON(t, parsed)
			} else {
				assert.Nil(t, result)
			}
		})
	}
}

func TestEthrexParser_ClientType(t *testing.T) {
	parser := NewEthrexParser()
	assert.Equal(t, "ethrex", string(parser.ClientType()))
}
