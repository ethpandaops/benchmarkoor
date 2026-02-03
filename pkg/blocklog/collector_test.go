package blocklog

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollector_BasicFlow(t *testing.T) {
	downstream := &bytes.Buffer{}
	parser := NewGethParser()
	collector := NewCollector(parser, downstream)

	// Set current test.
	collector.SetCurrentTest("test-1")

	// Write a log line with JSON.
	writer := collector.Writer()
	_, err := writer.Write([]byte(`WARN [02-02|15:03:22.121] {"level":"warn","msg":"Slow block"}` + "\n"))
	require.NoError(t, err)

	// Verify downstream received the data.
	assert.Contains(t, downstream.String(), "Slow block")

	// Clear current test.
	collector.ClearCurrentTest()

	// Get block logs.
	logs := collector.GetBlockLogs()
	require.Len(t, logs, 1)
	assert.Contains(t, string(logs["test-1"]), "Slow block")
}

func TestCollector_MultipleTests(t *testing.T) {
	downstream := &bytes.Buffer{}
	parser := NewGethParser()
	collector := NewCollector(parser, downstream)
	writer := collector.Writer()

	// Test 1.
	collector.SetCurrentTest("test-1")
	_, err := writer.Write([]byte(`WARN [02-02|15:03:22.121] {"test":1}` + "\n"))
	require.NoError(t, err)
	collector.ClearCurrentTest()

	// Test 2.
	collector.SetCurrentTest("test-2")
	_, err = writer.Write([]byte(`WARN [02-02|15:03:22.122] {"test":2}` + "\n"))
	require.NoError(t, err)
	collector.ClearCurrentTest()

	// Get block logs.
	logs := collector.GetBlockLogs()
	require.Len(t, logs, 2)
	assert.JSONEq(t, `{"test":1}`, string(logs["test-1"]))
	assert.JSONEq(t, `{"test":2}`, string(logs["test-2"]))
}

func TestCollector_NoCurrentTest(t *testing.T) {
	downstream := &bytes.Buffer{}
	parser := NewGethParser()
	collector := NewCollector(parser, downstream)
	writer := collector.Writer()

	// Write without setting current test.
	_, err := writer.Write([]byte(`WARN [02-02|15:03:22.121] {"msg":"ignored"}` + "\n"))
	require.NoError(t, err)

	// Verify no logs captured.
	logs := collector.GetBlockLogs()
	assert.Empty(t, logs)

	// Verify downstream still received the data.
	assert.Contains(t, downstream.String(), "ignored")
}

func TestCollector_NonJSONLines(t *testing.T) {
	downstream := &bytes.Buffer{}
	parser := NewGethParser()
	collector := NewCollector(parser, downstream)
	writer := collector.Writer()

	collector.SetCurrentTest("test-1")

	// Write a non-JSON line.
	_, err := writer.Write([]byte("WARN [02-02|15:03:22.121] Some regular log message\n"))
	require.NoError(t, err)

	// Verify no logs captured.
	logs := collector.GetBlockLogs()
	assert.Empty(t, logs)

	// Write a JSON line.
	_, err = writer.Write([]byte(`WARN [02-02|15:03:22.122] {"msg":"captured"}` + "\n"))
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

	collector.SetCurrentTest("test-1")

	// Write partial line.
	line := `WARN [02-02|15:03:22.121] {"msg":"partial"}` + "\n"
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

	collector.SetCurrentTest("test-1")

	// Write first payload.
	_, err := writer.Write([]byte(`WARN [02-02|15:03:22.121] {"version":1}` + "\n"))
	require.NoError(t, err)

	// Write second payload (should overwrite).
	_, err = writer.Write([]byte(`WARN [02-02|15:03:22.122] {"version":2}` + "\n"))
	require.NoError(t, err)

	// Should have the latest value.
	logs := collector.GetBlockLogs()
	require.Len(t, logs, 1)
	assert.JSONEq(t, `{"version":2}`, string(logs["test-1"]))
}

func TestCollector_GetBlockLogsReturnsCopy(t *testing.T) {
	downstream := &bytes.Buffer{}
	parser := NewGethParser()
	collector := NewCollector(parser, downstream)
	writer := collector.Writer()

	collector.SetCurrentTest("test-1")
	_, err := writer.Write([]byte(`WARN [02-02|15:03:22.121] {"msg":"test"}` + "\n"))
	require.NoError(t, err)

	// Get logs and modify the returned map.
	logs1 := collector.GetBlockLogs()
	delete(logs1, "test-1")

	// Original should be unaffected.
	logs2 := collector.GetBlockLogs()
	require.Len(t, logs2, 1)
}
