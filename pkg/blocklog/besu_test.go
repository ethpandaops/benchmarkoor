package blocklog

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBesuParser_ParseLine(t *testing.T) {
	parser := NewBesuParser()

	tests := []struct {
		name      string
		line      string
		wantOK    bool
		checkJSON func(t *testing.T, data map[string]any)
	}{
		{
			name:   "valid SlowBlock line with all fields",
			line:   `2026-03-20 14:48:33.568+0000 | vert.x-worker-thread-0 | WARN  | SlowBlock | {"level":"warn","msg":"Slow block","block":{"number":1,"hash":"0x5c6519e89d3b01dc9846d2b67a07202efd45fcd35d380beada32f7be406fd22d","gas_used":100000000,"tx_count":6},"timing":{"execution_ms":970.803696,"state_read_ms":1.365365,"state_hash_ms":1.78546,"commit_ms":0.93028,"total_ms":973.519436},"throughput":{"mgas_per_sec":103.01},"state_reads":{"accounts":9,"storage_slots":11,"code":5,"code_bytes":25672},"state_writes":{"accounts":20,"storage_slots":20,"code":0,"code_bytes":0,"eip7702_delegations_set":0,"eip7702_delegations_cleared":0},"cache":{"account":{"hits":44,"misses":9,"hit_rate":83.02},"storage":{"hits":10,"misses":11,"hit_rate":47.62},"code":{"hits":5,"misses":0,"hit_rate":100.0}},"unique":{"accounts":3,"storage_slots":0,"contracts":2},"evm":{"sload":0,"sstore":0,"calls":372615,"creates":0}}`,
			wantOK: true,
			checkJSON: func(t *testing.T, data map[string]any) {
				t.Helper()

				assert.Equal(t, "warn", data["level"])
				assert.Equal(t, "Slow block", data["msg"])

				block := data["block"].(map[string]any)
				assert.Equal(t, float64(1), block["number"])
				assert.Equal(t, "0x5c6519e89d3b01dc9846d2b67a07202efd45fcd35d380beada32f7be406fd22d", block["hash"])
				assert.Equal(t, float64(100000000), block["gas_used"])
				assert.Equal(t, float64(6), block["tx_count"])

				timing := data["timing"].(map[string]any)
				assert.Equal(t, 970.803696, timing["execution_ms"])
				assert.Equal(t, 1.365365, timing["state_read_ms"])
				assert.Equal(t, 1.78546, timing["state_hash_ms"])
				assert.Equal(t, 0.93028, timing["commit_ms"])
				assert.Equal(t, 973.519436, timing["total_ms"])

				throughput := data["throughput"].(map[string]any)
				assert.Equal(t, 103.01, throughput["mgas_per_sec"])

				stateReads := data["state_reads"].(map[string]any)
				assert.Equal(t, float64(9), stateReads["accounts"])
				assert.Equal(t, float64(11), stateReads["storage_slots"])
				assert.Equal(t, float64(5), stateReads["code"])
				assert.Equal(t, float64(25672), stateReads["code_bytes"])

				stateWrites := data["state_writes"].(map[string]any)
				assert.Equal(t, float64(20), stateWrites["accounts"])
				assert.Equal(t, float64(20), stateWrites["storage_slots"])
				assert.Equal(t, float64(0), stateWrites["code"])

				cache := data["cache"].(map[string]any)
				account := cache["account"].(map[string]any)
				assert.Equal(t, float64(44), account["hits"])
				assert.Equal(t, float64(9), account["misses"])
				assert.Equal(t, 83.02, account["hit_rate"])

				storage := cache["storage"].(map[string]any)
				assert.Equal(t, float64(10), storage["hits"])
				assert.Equal(t, float64(11), storage["misses"])
				assert.Equal(t, 47.62, storage["hit_rate"])

				code := cache["code"].(map[string]any)
				assert.Equal(t, float64(5), code["hits"])
				assert.Equal(t, float64(0), code["misses"])
				assert.Equal(t, 100.0, code["hit_rate"])
			},
		},
		{
			name:   "line with ANSI escape codes",
			line:   "\x1b[m\x1b[2m2026-03-20 14:48:33.568+0000\x1b[m\x1b[2m | \x1b[m\x1b[2mvert.x-worker-thread-0\x1b[m\x1b[2m | \x1b[m\x1b[33mWARN \x1b[m\x1b[2m | \x1b[m\x1b[2mSlowBlock\x1b[m\x1b[2m | \x1b[m\x1b[33m{\"level\":\"warn\",\"msg\":\"Slow block\",\"block\":{\"number\":1,\"hash\":\"0xabc\",\"gas_used\":100000000,\"tx_count\":6},\"timing\":{\"execution_ms\":970.8,\"state_read_ms\":1.3,\"state_hash_ms\":1.7,\"commit_ms\":0.9,\"total_ms\":973.5},\"throughput\":{\"mgas_per_sec\":103.01}}\x1b[m",
			wantOK: true,
			checkJSON: func(t *testing.T, data map[string]any) {
				t.Helper()

				assert.Equal(t, "warn", data["level"])
				assert.Equal(t, "Slow block", data["msg"])

				block := data["block"].(map[string]any)
				assert.Equal(t, float64(1), block["number"])
				assert.Equal(t, "0xabc", block["hash"])

				timing := data["timing"].(map[string]any)
				assert.Equal(t, 970.8, timing["execution_ms"])

				throughput := data["throughput"].(map[string]any)
				assert.Equal(t, 103.01, throughput["mgas_per_sec"])
			},
		},
		{
			name:   "non-SlowBlock besu log line",
			line:   `2026-03-20 14:48:33.568+0000 | vert.x-worker-thread-0 | INFO  | BlockManager | Block imported #1`,
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
			name:   "invalid JSON after SlowBlock prefix",
			line:   `2026-03-20 14:48:33.568+0000 | vert.x-worker-thread-0 | WARN  | SlowBlock | {not valid json}`,
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

func TestBesuParser_ClientType(t *testing.T) {
	parser := NewBesuParser()
	assert.Equal(t, "besu", string(parser.ClientType()))
}
