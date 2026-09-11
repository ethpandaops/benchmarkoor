package blocklog

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Taken verbatim from a validated block that deploys a contract. Erigon omits
// state_reads.code and cache.code rather than reporting zero, because it has no
// CodeDomain read counter; state_writes.code is counted and so is emitted.
// total_ms is end-to-end and exceeds execution_ms + state_hash_ms + commit_ms.
const erigonPayload = `{"level":"warn","msg":"Slow block","block":{"number":2,"hash":"0xda4c162eb46b6163de6ab73a4a56d78c34e82771a875687868d4d006e8ac3b37","gas_used":185130,"tx_count":1},"timing":{"execution_ms":0.408167,"state_read_ms":0.001291,"state_hash_ms":0.06425,"commit_ms":0.179,"total_ms":0.744125},"throughput":{"mgas_per_sec":453.56},"state_reads":{"accounts":4,"storage_slots":0},"state_writes":{"accounts":3,"storage_slots":0,"code":1},"cache":{"account":{"hits":4,"misses":0,"hit_rate":100},"storage":{"hits":0,"misses":0,"hit_rate":0}}}`

func erigonJSONLine(payload string) string {
	msg, _ := json.Marshal(payload)

	return `{"lvl":"warn","t":"2026-09-03T17:21:56.701597+07:00","msg":` + string(msg) + `}`
}

func TestErigonParser_ParseLine(t *testing.T) {
	parser := NewErigonParser()

	tests := []struct {
		name      string
		line      string
		wantOK    bool
		checkJSON func(t *testing.T, data map[string]any)
	}{
		{
			name:   "non-TTY line with all fields",
			line:   `[WARN] [09-01|22:20:12.372] ` + erigonPayload + ` `,
			wantOK: true,
			checkJSON: func(t *testing.T, data map[string]any) {
				t.Helper()

				assert.Equal(t, "warn", data["level"])
				assert.Equal(t, "Slow block", data["msg"])

				block := data["block"].(map[string]any)
				assert.Equal(t, float64(2), block["number"])
				assert.Equal(t, "0xda4c162eb46b6163de6ab73a4a56d78c34e82771a875687868d4d006e8ac3b37", block["hash"])
				assert.Equal(t, float64(185130), block["gas_used"])
				assert.Equal(t, float64(1), block["tx_count"])

				timing := data["timing"].(map[string]any)
				assert.Equal(t, 0.408167, timing["execution_ms"])
				assert.Equal(t, 0.001291, timing["state_read_ms"])
				assert.Equal(t, 0.06425, timing["state_hash_ms"])
				assert.Equal(t, 0.179, timing["commit_ms"])
				assert.Equal(t, 0.744125, timing["total_ms"])

				phases := timing["execution_ms"].(float64) + timing["state_hash_ms"].(float64) + timing["commit_ms"].(float64)
				assert.Greater(t, timing["total_ms"].(float64)-phases, 0.01,
					"total_ms is end-to-end, so it exceeds the phase breakdown by the stages outside it")

				throughput := data["throughput"].(map[string]any)
				assert.Equal(t, 453.56, throughput["mgas_per_sec"])

				stateReads := data["state_reads"].(map[string]any)
				assert.Equal(t, float64(4), stateReads["accounts"])
				assert.Equal(t, float64(0), stateReads["storage_slots"])
				assert.NotContains(t, stateReads, "code",
					"Erigon omits code reads rather than reporting an unmeasured zero")

				stateWrites := data["state_writes"].(map[string]any)
				assert.Equal(t, float64(3), stateWrites["accounts"])
				assert.Equal(t, float64(1), stateWrites["code"],
					"code writes are counted, so the deploy must survive the round trip")

				cache := data["cache"].(map[string]any)
				assert.NotContains(t, cache, "code",
					"Erigon omits the code cache summary rather than reporting an unmeasured zero")

				account := cache["account"].(map[string]any)
				assert.Equal(t, float64(4), account["hits"])
				assert.Equal(t, float64(0), account["misses"])
				assert.Equal(t, float64(100), account["hit_rate"])
			},
		},
		{
			name:   "TTY line with ANSI escape codes",
			line:   "\x1b[33mWARN\x1b[0m[09-01|22:20:12.372] " + erigonPayload + " ",
			wantOK: true,
			checkJSON: func(t *testing.T, data map[string]any) {
				t.Helper()

				assert.Equal(t, "Slow block", data["msg"])
				assert.Equal(t, float64(2), data["block"].(map[string]any)["number"])
				assert.Equal(t, 0.06425, data["timing"].(map[string]any)["state_hash_ms"])
			},
		},
		{
			name:   "envelope level is not the discriminator (DBUG)",
			line:   `[DBUG] [09-01|22:20:12.372] ` + erigonPayload,
			wantOK: true,
		},
		{
			name:   "padded level",
			line:   `[WARN ] [09-01|22:20:12.372] ` + erigonPayload,
			wantOK: true,
		},
		{
			name:   "--log.json envelope carries the record escaped in msg",
			line:   erigonJSONLine(erigonPayload),
			wantOK: true,
			checkJSON: func(t *testing.T, data map[string]any) {
				t.Helper()

				assert.Equal(t, "Slow block", data["msg"])
				assert.Equal(t, float64(185130), data["block"].(map[string]any)["gas_used"])
				assert.Equal(t, 0.744125, data["timing"].(map[string]any)["total_ms"])
			},
		},
		{
			name:   "--log.json envelope carrying an ordinary message",
			line:   erigonJSONLine("Executed blocks"),
			wantOK: false,
		},
		{
			name:   "right message but no timing object",
			line:   `[WARN] [09-01|22:20:12.372] {"level":"warn","msg":"Slow block","block":{"hash":"0xabc"}}`,
			wantOK: false,
		},
		{
			name:   "JSON payload from another message",
			line:   `[WARN] [09-01|22:20:12.372] {"level":"warn","msg":"Something else","block":{"number":1}}`,
			wantOK: false,
		},
		{
			name:   "ordinary erigon log line",
			line:   `[INFO] [09-01|22:20:12.372] [1/6 OtterSync] Downloading                 progress="98.5% 12/13"`,
			wantOK: false,
		},
		{
			name:   "invalid JSON",
			line:   `[WARN] [09-01|22:20:12.372] {not valid json}`,
			wantOK: false,
		},
		{
			name:   "ERIGON_LOG_NO_TIMESTAMPS drops the timestamp group",
			line:   "\x1b[33mWARN\x1b[0m " + erigonPayload + " ",
			wantOK: true,
			checkJSON: func(t *testing.T, data map[string]any) {
				t.Helper()

				assert.Equal(t, "Slow block", data["msg"])
				assert.Equal(t, 0.744125, data["timing"].(map[string]any)["total_ms"])
			},
		},
		{
			name:   "no timestamp, brackets kept",
			line:   `[WARN] ` + erigonPayload,
			wantOK: true,
		},
		{
			name:   "empty line",
			line:   "",
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

				if tt.checkJSON != nil {
					tt.checkJSON(t, parsed)
				}
			} else {
				assert.Nil(t, result)
			}
		})
	}
}

func TestErigonParser_ClientType(t *testing.T) {
	parser := NewErigonParser()
	assert.Equal(t, "erigon", string(parser.ClientType()))
}
