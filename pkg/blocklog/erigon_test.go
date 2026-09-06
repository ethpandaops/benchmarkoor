package blocklog

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Taken verbatim from a validated block.
const erigonPayload = `{"level":"warn","msg":"Slow block","block":{"number":1,"hash":"0xb8dadbee815351753287e128c38d4bde06d231afd5662739eede2e39dd08138e","gas_used":12000,"tx_count":1},"timing":{"execution_ms":0.854709,"state_read_ms":0.00125,"state_hash_ms":0.077708,"commit_ms":0.233458,"total_ms":1.165875},"throughput":{"mgas_per_sec":14.04},"state_reads":{"accounts":3,"storage_slots":0,"code":0},"state_writes":{"accounts":2,"storage_slots":1,"code":0},"cache":{"account":{"hits":3,"misses":0,"hit_rate":100},"storage":{"hits":0,"misses":0,"hit_rate":0},"code":{"hits":0,"misses":0,"hit_rate":0}}}`

func erigonJSONLine(t *testing.T, payload string) string {
	t.Helper()

	msg, err := json.Marshal(payload)
	require.NoError(t, err)

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
				assert.Equal(t, float64(1), block["number"])
				assert.Equal(t, "0xb8dadbee815351753287e128c38d4bde06d231afd5662739eede2e39dd08138e", block["hash"])
				assert.Equal(t, float64(12000), block["gas_used"])
				assert.Equal(t, float64(1), block["tx_count"])

				timing := data["timing"].(map[string]any)
				assert.Equal(t, 0.854709, timing["execution_ms"])
				assert.Equal(t, 0.00125, timing["state_read_ms"])
				assert.Equal(t, 0.077708, timing["state_hash_ms"])
				assert.Equal(t, 0.233458, timing["commit_ms"])
				assert.Equal(t, 1.165875, timing["total_ms"])

				throughput := data["throughput"].(map[string]any)
				assert.Equal(t, 14.04, throughput["mgas_per_sec"])

				stateReads := data["state_reads"].(map[string]any)
				assert.Equal(t, float64(3), stateReads["accounts"])
				assert.Equal(t, float64(0), stateReads["storage_slots"])
				assert.Equal(t, float64(0), stateReads["code"])

				stateWrites := data["state_writes"].(map[string]any)
				assert.Equal(t, float64(2), stateWrites["accounts"])

				account := data["cache"].(map[string]any)["account"].(map[string]any)
				assert.Equal(t, float64(3), account["hits"])
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
				assert.Equal(t, float64(1), data["block"].(map[string]any)["number"])
				assert.Equal(t, 0.077708, data["timing"].(map[string]any)["state_hash_ms"])
			},
		},
		{
			name:   "envelope level is not the discriminator (DBUG)",
			line:   `[DBUG] [09-01|22:20:12.372] ` + erigonPayload,
			wantOK: true,
			checkJSON: func(t *testing.T, data map[string]any) {
				t.Helper()

				assert.Equal(t, "Slow block", data["msg"])
			},
		},
		{
			name:   "envelope level is not the discriminator (EROR)",
			line:   `[EROR] [09-01|22:20:12.372] ` + erigonPayload,
			wantOK: true,
			checkJSON: func(t *testing.T, data map[string]any) {
				t.Helper()

				assert.Equal(t, "Slow block", data["msg"])
			},
		},
		{
			name:   "padded level",
			line:   `[WARN ] [09-01|22:20:12.372] ` + erigonPayload,
			wantOK: true,
			checkJSON: func(t *testing.T, data map[string]any) {
				t.Helper()

				assert.Equal(t, "Slow block", data["msg"])
			},
		},
		{
			name:   "--log.json envelope carries the record escaped in msg",
			line:   erigonJSONLine(t, erigonPayload),
			wantOK: true,
			checkJSON: func(t *testing.T, data map[string]any) {
				t.Helper()

				assert.Equal(t, "Slow block", data["msg"])
				assert.Equal(t, float64(12000), data["block"].(map[string]any)["gas_used"])
				assert.Equal(t, 1.165875, data["timing"].(map[string]any)["total_ms"])
			},
		},
		{
			name:   "--log.json envelope carrying an ordinary message",
			line:   erigonJSONLine(t, "Executed blocks"),
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
			name:   "missing timestamp",
			line:   `[WARN] ` + erigonPayload,
			wantOK: false,
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

				tt.checkJSON(t, parsed)
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
