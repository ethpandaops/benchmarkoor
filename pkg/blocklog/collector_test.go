package blocklog

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollector_LogAfterRegistration(t *testing.T) {
	downstream := &bytes.Buffer{}
	parser := NewGethParser()
	collector := NewCollector(parser, downstream)

	// Register blockHash first.
	collector.RegisterBlockHash("test-1", "0xabc123")

	// Write a log line with matching blockHash.
	writer := collector.Writer()
	_, err := writer.Write([]byte(`WARN [02-02|15:03:22.121] {"level":"warn","msg":"Slow block","block":{"hash":"0xabc123"}}` + "\n"))
	require.NoError(t, err)

	// Verify downstream received the data.
	assert.Contains(t, downstream.String(), "Slow block")

	// Get block logs.
	logs := collector.GetBlockLogs()
	require.Len(t, logs, 1)
	assert.Contains(t, string(logs["test-1"]), "Slow block")
	assert.Contains(t, string(logs["test-1"]), "0xabc123")
}

func TestCollector_LogBeforeRegistration(t *testing.T) {
	downstream := &bytes.Buffer{}
	parser := NewGethParser()
	collector := NewCollector(parser, downstream)
	writer := collector.Writer()

	// Write a log line BEFORE registration (late registration scenario).
	_, err := writer.Write([]byte(`WARN [02-02|15:03:22.121] {"msg":"early","block":{"hash":"0xdef456"}}` + "\n"))
	require.NoError(t, err)

	// No logs yet since no registration.
	logs := collector.GetBlockLogs()
	assert.Empty(t, logs)

	// Now register the blockHash (late registration).
	collector.RegisterBlockHash("test-1", "0xdef456")

	// Now we should have the log.
	logs = collector.GetBlockLogs()
	require.Len(t, logs, 1)
	assert.Contains(t, string(logs["test-1"]), "early")
}

func TestCollector_MultipleTests(t *testing.T) {
	downstream := &bytes.Buffer{}
	parser := NewGethParser()
	collector := NewCollector(parser, downstream)
	writer := collector.Writer()

	// Test 1: register and write.
	collector.RegisterBlockHash("test-1", "0xhash1")
	_, err := writer.Write([]byte(`WARN [02-02|15:03:22.121] {"test":1,"block":{"hash":"0xhash1"}}` + "\n"))
	require.NoError(t, err)

	// Test 2: register and write.
	collector.RegisterBlockHash("test-2", "0xhash2")
	_, err = writer.Write([]byte(`WARN [02-02|15:03:22.122] {"test":2,"block":{"hash":"0xhash2"}}` + "\n"))
	require.NoError(t, err)

	// Get block logs.
	logs := collector.GetBlockLogs()
	require.Len(t, logs, 2)
	assert.Contains(t, string(logs["test-1"]), `"test":1`)
	assert.Contains(t, string(logs["test-2"]), `"test":2`)
}

func TestCollector_NoRegistration(t *testing.T) {
	downstream := &bytes.Buffer{}
	parser := NewGethParser()
	collector := NewCollector(parser, downstream)
	writer := collector.Writer()

	// Write without registering blockHash (buffered in unmatched).
	_, err := writer.Write([]byte(`WARN [02-02|15:03:22.121] {"msg":"orphan","block":{"hash":"0xorphan"}}` + "\n"))
	require.NoError(t, err)

	// Verify no logs captured (no registration).
	logs := collector.GetBlockLogs()
	assert.Empty(t, logs)

	// Verify downstream still received the data.
	assert.Contains(t, downstream.String(), "orphan")
}

func TestCollector_NoBlockHash(t *testing.T) {
	downstream := &bytes.Buffer{}
	parser := NewGethParser()
	collector := NewCollector(parser, downstream)
	writer := collector.Writer()

	collector.RegisterBlockHash("test-1", "0xexpected")

	// Write a JSON line without block.hash field.
	_, err := writer.Write([]byte(`WARN [02-02|15:03:22.121] {"msg":"no-hash"}` + "\n"))
	require.NoError(t, err)

	// Verify no logs captured (no blockHash in payload).
	logs := collector.GetBlockLogs()
	assert.Empty(t, logs)
}

func TestCollector_NonJSONLines(t *testing.T) {
	downstream := &bytes.Buffer{}
	parser := NewGethParser()
	collector := NewCollector(parser, downstream)
	writer := collector.Writer()

	collector.RegisterBlockHash("test-1", "0xabc")

	// Write a non-JSON line.
	_, err := writer.Write([]byte("WARN [02-02|15:03:22.121] Some regular log message\n"))
	require.NoError(t, err)

	// Verify no logs captured.
	logs := collector.GetBlockLogs()
	assert.Empty(t, logs)

	// Write a JSON line with matching hash.
	_, err = writer.Write([]byte(`WARN [02-02|15:03:22.122] {"msg":"captured","block":{"hash":"0xabc"}}` + "\n"))
	require.NoError(t, err)

	// Now we should have one log.
	logs = collector.GetBlockLogs()
	require.Len(t, logs, 1)
}

func TestCollector_PartialWrites(t *testing.T) {
	downstream := &bytes.Buffer{}
	parser := NewGethParser()
	collector := NewCollector(parser, downstream)
	writer := collector.Writer()

	collector.RegisterBlockHash("test-1", "0xpartial")

	// Write partial line.
	line := `WARN [02-02|15:03:22.121] {"msg":"partial","block":{"hash":"0xpartial"}}` + "\n"
	half := len(line) / 2

	_, err := writer.Write([]byte(line[:half]))
	require.NoError(t, err)

	// No complete line yet.
	logs := collector.GetBlockLogs()
	assert.Empty(t, logs)

	// Write the rest.
	_, err = writer.Write([]byte(line[half:]))
	require.NoError(t, err)

	// Now we should have the log.
	logs = collector.GetBlockLogs()
	require.Len(t, logs, 1)
	assert.Contains(t, string(logs["test-1"]), "partial")
}

func TestCollector_OverwritesOnSameTest(t *testing.T) {
	downstream := &bytes.Buffer{}
	parser := NewGethParser()
	collector := NewCollector(parser, downstream)
	writer := collector.Writer()

	// Register two different hashes for the same test (simulates multiple payloads per test).
	collector.RegisterBlockHash("test-1", "0xfirst")
	collector.RegisterBlockHash("test-1", "0xsecond")

	// Write first payload.
	_, err := writer.Write([]byte(`WARN [02-02|15:03:22.121] {"version":1,"block":{"hash":"0xfirst"}}` + "\n"))
	require.NoError(t, err)

	// Write second payload (should overwrite).
	_, err = writer.Write([]byte(`WARN [02-02|15:03:22.122] {"version":2,"block":{"hash":"0xsecond"}}` + "\n"))
	require.NoError(t, err)

	// Should have the latest value.
	logs := collector.GetBlockLogs()
	require.Len(t, logs, 1)
	assert.Contains(t, string(logs["test-1"]), `"version":2`)
}

func TestCollector_GetBlockLogsReturnsCopy(t *testing.T) {
	downstream := &bytes.Buffer{}
	parser := NewGethParser()
	collector := NewCollector(parser, downstream)
	writer := collector.Writer()

	collector.RegisterBlockHash("test-1", "0xcopy")
	_, err := writer.Write([]byte(`WARN [02-02|15:03:22.121] {"msg":"test","block":{"hash":"0xcopy"}}` + "\n"))
	require.NoError(t, err)

	// Get logs and modify the returned map.
	logs1 := collector.GetBlockLogs()
	delete(logs1, "test-1")

	// Original should be unaffected.
	logs2 := collector.GetBlockLogs()
	require.Len(t, logs2, 1)
}

func TestCollector_MismatchedHash(t *testing.T) {
	downstream := &bytes.Buffer{}
	parser := NewGethParser()
	collector := NewCollector(parser, downstream)
	writer := collector.Writer()

	// Register one hash.
	collector.RegisterBlockHash("test-1", "0xexpected")

	// Write a log with different hash.
	_, err := writer.Write([]byte(`WARN [02-02|15:03:22.121] {"msg":"different","block":{"hash":"0xdifferent"}}` + "\n"))
	require.NoError(t, err)

	// Should not match test-1.
	logs := collector.GetBlockLogs()
	assert.Empty(t, logs)
}

func TestExtractBlockHashFromPayload(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		wantHash string
		wantOK   bool
	}{
		{
			name:     "valid payload",
			payload:  `{"block":{"hash":"0xabc123"}}`,
			wantHash: "0xabc123",
			wantOK:   true,
		},
		{
			name:     "payload with extra fields",
			payload:  `{"msg":"test","block":{"hash":"0xdef456","number":123}}`,
			wantHash: "0xdef456",
			wantOK:   true,
		},
		{
			name:     "missing block field",
			payload:  `{"msg":"test"}`,
			wantHash: "",
			wantOK:   false,
		},
		{
			name:     "empty hash",
			payload:  `{"block":{"hash":""}}`,
			wantHash: "",
			wantOK:   false,
		},
		{
			name:     "invalid json",
			payload:  `not json`,
			wantHash: "",
			wantOK:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, ok := extractBlockHashFromPayload([]byte(tt.payload))
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantHash, hash)
		})
	}
}
