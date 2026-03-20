package blocklog

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNethermindParser_ParseLine(t *testing.T) {
	parser := NewNethermindParser()

	tests := []struct {
		name      string
		line      string
		wantOK    bool
		checkJSON func(t *testing.T, data map[string]any)
	}{
		{
			name:   "valid Slow block line with all fields",
			line:   ` 20 Mar 18:44:40 | {"level":"warn","msg":"Slow block","block":{"number":1,"hash":"0x3fe6bd8e331c411dcb32e75054b2bfd3f8deb177414650782883af6f5982a014","gas_used":100000000,"gas_limit":1000000000000,"tx_count":6,"blob_count":0},"timing":{"execution_ms":654.08,"evm_ms":653.989,"blooms_ms":0.055,"receipts_root_ms":0.036,"commit_ms":0.002,"storage_merkle_ms":0.052,"state_root_ms":0.141,"state_hash_ms":0.194,"total_ms":654.275},"throughput":{"mgas_per_sec":152.84},"state_reads":{"accounts":8,"storage_slots":10,"code":0,"code_bytes":0},"state_writes":{"accounts":14,"accounts_deleted":0,"storage_slots":2,"storage_slots_deleted":0,"code":0,"code_bytes":0,"eip7702_delegations_set":0,"eip7702_delegations_cleared":0},"cache":{"account":{"hits":26,"misses":8,"hit_rate":76.47},"storage":{"hits":10,"misses":1,"hit_rate":90.91},"code":{"hits":9,"misses":0,"hit_rate":100}},"evm":{"opcodes":910,"sload":8,"sstore":10,"calls":89,"empty_calls":0,"creates":0,"self_destructs":0,"contracts_analyzed":0,"cached_contracts_used":9}}`,
			wantOK: true,
			checkJSON: func(t *testing.T, data map[string]any) {
				t.Helper()

				assert.Equal(t, "warn", data["level"])
				assert.Equal(t, "Slow block", data["msg"])

				block := data["block"].(map[string]any)
				assert.Equal(t, float64(1), block["number"])
				assert.Equal(t, "0x3fe6bd8e331c411dcb32e75054b2bfd3f8deb177414650782883af6f5982a014", block["hash"])
				assert.Equal(t, float64(100000000), block["gas_used"])
				assert.Equal(t, float64(1000000000000), block["gas_limit"])
				assert.Equal(t, float64(6), block["tx_count"])
				assert.Equal(t, float64(0), block["blob_count"])

				timing := data["timing"].(map[string]any)
				assert.Equal(t, 654.08, timing["execution_ms"])
				assert.Equal(t, 653.989, timing["evm_ms"])
				assert.Equal(t, 0.055, timing["blooms_ms"])
				assert.Equal(t, 0.036, timing["receipts_root_ms"])
				assert.Equal(t, 0.002, timing["commit_ms"])
				assert.Equal(t, 0.052, timing["storage_merkle_ms"])
				assert.Equal(t, 0.141, timing["state_root_ms"])
				assert.Equal(t, 0.194, timing["state_hash_ms"])
				assert.Equal(t, 654.275, timing["total_ms"])

				throughput := data["throughput"].(map[string]any)
				assert.Equal(t, 152.84, throughput["mgas_per_sec"])

				stateReads := data["state_reads"].(map[string]any)
				assert.Equal(t, float64(8), stateReads["accounts"])
				assert.Equal(t, float64(10), stateReads["storage_slots"])
				assert.Equal(t, float64(0), stateReads["code"])
				assert.Equal(t, float64(0), stateReads["code_bytes"])

				stateWrites := data["state_writes"].(map[string]any)
				assert.Equal(t, float64(14), stateWrites["accounts"])
				assert.Equal(t, float64(0), stateWrites["accounts_deleted"])
				assert.Equal(t, float64(2), stateWrites["storage_slots"])
				assert.Equal(t, float64(0), stateWrites["storage_slots_deleted"])

				cache := data["cache"].(map[string]any)
				account := cache["account"].(map[string]any)
				assert.Equal(t, float64(26), account["hits"])
				assert.Equal(t, float64(8), account["misses"])
				assert.Equal(t, 76.47, account["hit_rate"])

				storage := cache["storage"].(map[string]any)
				assert.Equal(t, float64(10), storage["hits"])
				assert.Equal(t, float64(1), storage["misses"])
				assert.Equal(t, 90.91, storage["hit_rate"])

				code := cache["code"].(map[string]any)
				assert.Equal(t, float64(9), code["hits"])
				assert.Equal(t, float64(0), code["misses"])
				assert.Equal(t, float64(100), code["hit_rate"])

				evm := data["evm"].(map[string]any)
				assert.Equal(t, float64(910), evm["opcodes"])
				assert.Equal(t, float64(8), evm["sload"])
				assert.Equal(t, float64(10), evm["sstore"])
				assert.Equal(t, float64(89), evm["calls"])
			},
		},
		{
			name:   "line with ANSI escape codes",
			line:   "\x1b[33m 20 Mar 18:44:40\x1b[m | \x1b[33m{\"level\":\"warn\",\"msg\":\"Slow block\",\"block\":{\"number\":1,\"hash\":\"0xabc\",\"gas_used\":100000000,\"tx_count\":6},\"timing\":{\"execution_ms\":654.08,\"total_ms\":654.275},\"throughput\":{\"mgas_per_sec\":152.84}}\x1b[m",
			wantOK: true,
			checkJSON: func(t *testing.T, data map[string]any) {
				t.Helper()

				assert.Equal(t, "warn", data["level"])
				assert.Equal(t, "Slow block", data["msg"])

				block := data["block"].(map[string]any)
				assert.Equal(t, float64(1), block["number"])
				assert.Equal(t, "0xabc", block["hash"])

				timing := data["timing"].(map[string]any)
				assert.Equal(t, 654.08, timing["execution_ms"])

				throughput := data["throughput"].(map[string]any)
				assert.Equal(t, 152.84, throughput["mgas_per_sec"])
			},
		},
		{
			name:   "non-Slow block log line",
			line:   ` 20 Mar 18:44:40 | {"level":"info","msg":"Block processed","block":{"number":1}}`,
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
			name:   "invalid JSON after timestamp prefix",
			line:   ` 20 Mar 18:44:40 | {not valid json}`,
			wantOK: false,
		},
		{
			name:   "valid JSON but not Slow block message",
			line:   ` 20 Mar 18:44:40 | {"level":"info","msg":"Something else","data":"value"}`,
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

func TestNethermindParser_ClientType(t *testing.T) {
	parser := NewNethermindParser()
	assert.Equal(t, "nethermind", string(parser.ClientType()))
}
