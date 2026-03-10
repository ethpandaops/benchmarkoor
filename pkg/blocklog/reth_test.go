package blocklog

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRethParser_ParseLine(t *testing.T) {
	parser := NewRethParser()

	tests := []struct {
		name   string
		line   string
		wantOK bool
		// checkJSON is called when wantOK is true to verify the parsed output.
		checkJSON func(t *testing.T, data map[string]any)
	}{
		{
			name:   "valid slow_block line with all fields",
			line:   `2026-03-10T10:29:20.098444Z  WARN reth::slow_block: Slow block block.number=1 block.hash=0xc957abc123 block.gas_used=100000000 block.tx_count=6 timing.execution_ms=91 timing.state_read_ms=5 timing.state_hash_ms=10 timing.commit_ms=3 timing.total_ms=109 throughput.mgas_per_sec="1091.46" state_reads.accounts=8 state_reads.storage_slots=12 state_reads.code=2 state_reads.code_bytes=1024 state_writes.accounts=4 state_writes.accounts_deleted=0 state_writes.storage_slots=6 state_writes.code=1 cache.account.hits=1 cache.account.misses=7 cache.storage.hits=3 cache.storage.misses=9 cache.code.hits=0 cache.code.misses=2`,
			wantOK: true,
			checkJSON: func(t *testing.T, data map[string]any) {
				t.Helper()

				assert.Equal(t, "warn", data["level"])
				assert.Equal(t, "Slow block", data["msg"])

				block := data["block"].(map[string]any)
				assert.Equal(t, float64(1), block["number"])
				assert.Equal(t, "0xc957abc123", block["hash"])
				assert.Equal(t, float64(100000000), block["gas_used"])
				assert.Equal(t, float64(6), block["tx_count"])

				timing := data["timing"].(map[string]any)
				assert.Equal(t, float64(91), timing["execution_ms"])
				assert.Equal(t, float64(5), timing["state_read_ms"])
				assert.Equal(t, float64(10), timing["state_hash_ms"])
				assert.Equal(t, float64(3), timing["commit_ms"])
				assert.Equal(t, float64(109), timing["total_ms"])

				throughput := data["throughput"].(map[string]any)
				assert.Equal(t, 1091.46, throughput["mgas_per_sec"])

				stateReads := data["state_reads"].(map[string]any)
				assert.Equal(t, float64(8), stateReads["accounts"])
				assert.Equal(t, float64(12), stateReads["storage_slots"])
				assert.Equal(t, float64(2), stateReads["code"])
				assert.Equal(t, float64(1024), stateReads["code_bytes"])

				stateWrites := data["state_writes"].(map[string]any)
				assert.Equal(t, float64(4), stateWrites["accounts"])
				assert.Equal(t, float64(0), stateWrites["accounts_deleted"])
				assert.Equal(t, float64(6), stateWrites["storage_slots"])
				assert.Equal(t, float64(1), stateWrites["code"])

				cache := data["cache"].(map[string]any)
				account := cache["account"].(map[string]any)
				assert.Equal(t, float64(1), account["hits"])
				assert.Equal(t, float64(7), account["misses"])

				storage := cache["storage"].(map[string]any)
				assert.Equal(t, float64(3), storage["hits"])
				assert.Equal(t, float64(9), storage["misses"])

				code := cache["code"].(map[string]any)
				assert.Equal(t, float64(0), code["hits"])
				assert.Equal(t, float64(2), code["misses"])
			},
		},
		{
			name:   "quoted float values parsed correctly",
			line:   `2026-03-10T10:29:20.098444Z  WARN reth::slow_block: Slow block throughput.mgas_per_sec="12.50" timing.execution_ms="91.3"`,
			wantOK: true,
			checkJSON: func(t *testing.T, data map[string]any) {
				t.Helper()

				throughput := data["throughput"].(map[string]any)
				assert.Equal(t, 12.50, throughput["mgas_per_sec"])

				timing := data["timing"].(map[string]any)
				assert.Equal(t, 91.3, timing["execution_ms"])
			},
		},
		{
			name:   "non-slow-block reth log line",
			line:   `2026-03-10T10:29:20.098444Z  INFO reth::engine: Block received block.number=1`,
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
		{
			name:   "line with ANSI escape codes",
			line:   "\x1b[2m2026-03-10T10:50:19.731231Z\x1b[0m \x1b[33m WARN\x1b[0m \x1b[2mreth::slow_block\x1b[0m\x1b[2m:\x1b[0m Slow block \x1b[3mblock.number\x1b[0m\x1b[2m=\x1b[0m1 \x1b[3mblock.hash\x1b[0m\x1b[2m=\x1b[0m0x9f566dc9f8beb533db8611872f4ed57847d147224b59586d2c86e1bf957b8809 \x1b[3mblock.gas_used\x1b[0m\x1b[2m=\x1b[0m184074778176 \x1b[3mblock.tx_count\x1b[0m\x1b[2m=\x1b[0m12339 \x1b[3mtiming.execution_ms\x1b[0m\x1b[2m=\x1b[0m2783 \x1b[3mthroughput.mgas_per_sec\x1b[0m\x1b[2m=\x1b[0m\"66126.09\"",
			wantOK: true,
			checkJSON: func(t *testing.T, data map[string]any) {
				t.Helper()

				assert.Equal(t, "warn", data["level"])
				assert.Equal(t, "Slow block", data["msg"])

				block := data["block"].(map[string]any)
				assert.Equal(t, float64(1), block["number"])
				assert.Equal(t, "0x9f566dc9f8beb533db8611872f4ed57847d147224b59586d2c86e1bf957b8809", block["hash"])
				assert.Equal(t, float64(184074778176), block["gas_used"])
				assert.Equal(t, float64(12339), block["tx_count"])

				timing := data["timing"].(map[string]any)
				assert.Equal(t, float64(2783), timing["execution_ms"])

				throughput := data["throughput"].(map[string]any)
				assert.Equal(t, 66126.09, throughput["mgas_per_sec"])
			},
		},
		{
			name:   "extra unknown fields are preserved",
			line:   `2026-03-10T10:29:20.098444Z  WARN reth::slow_block: Slow block block.number=42 eip7702_delegations.set=5`,
			wantOK: true,
			checkJSON: func(t *testing.T, data map[string]any) {
				t.Helper()

				block := data["block"].(map[string]any)
				assert.Equal(t, float64(42), block["number"])

				eip := data["eip7702_delegations"].(map[string]any)
				assert.Equal(t, float64(5), eip["set"])
			},
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

func TestRethParser_ClientType(t *testing.T) {
	parser := NewRethParser()
	assert.Equal(t, "reth", string(parser.ClientType()))
}
