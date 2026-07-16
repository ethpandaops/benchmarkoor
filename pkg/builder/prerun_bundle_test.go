package builder

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// payload builds a recordedPayload whose execution payload has the given block
// number and hash (the only fields sort/dedup and replay inspect).
func payload(method string, number, hash string) recordedPayload {
	ep, _ := json.Marshal(map[string]string{"blockNumber": number, "blockHash": hash})
	return recordedPayload{Method: method, Params: []json.RawMessage{ep, []byte(`[]`), []byte(`"0x0"`), []byte(`[]`)}}
}

func TestSortAndDedupPayloads(t *testing.T) {
	in := []recordedPayload{
		payload("engine_newPayloadV5", "0x3", "0xc"),
		payload("engine_newPayloadV5", "0x1", "0xa"),
		payload("engine_newPayloadV5", "0x2", "0xb"),
		payload("engine_newPayloadV5", "0x2", "0xb"), // dup of block 2
	}

	got, err := sortAndDedupPayloads(in)
	require.NoError(t, err)
	require.Len(t, got, 3, "duplicate block dropped")

	hashes := make([]string, len(got))
	for i, p := range got {
		h, err := payloadBlockHash(p.Params[0])
		require.NoError(t, err)
		hashes[i] = h
	}
	assert.Equal(t, []string{"0xa", "0xb", "0xc"}, hashes, "sorted ascending by block number")
}

func TestWriteRequestBundle(t *testing.T) {
	dir := t.TempDir()
	in := []recordedPayload{
		payload("engine_newPayloadV5", "0x1", "0xaa"),
		payload("engine_newPayloadV4", "0x2", "0xbb"),
	}

	path, err := writeRequestBundle(dir, in)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, config.PreRunBundleSubdir, preRunBundleFile), path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	// Two lines (newPayload + forkchoiceUpdated) per block.
	require.Len(t, lines, 4)

	// Line 0: newPayloadV5 request.
	var np map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &np))
	assert.Equal(t, "engine_newPayloadV5", np["method"])

	// Line 1: forkchoiceUpdated to the block's hash.
	var fcu struct {
		Method string `json:"method"`
		Params []struct {
			HeadBlockHash string `json:"headBlockHash"`
		} `json:"params"`
	}
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &fcu))
	assert.Equal(t, "engine_forkchoiceUpdatedV3", fcu.Method)
	assert.Equal(t, "0xaa", fcu.Params[0].HeadBlockHash)
}

func TestExtractFixturePayloads(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "blockchain_tests_stateful_engine", "x")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	// A stateful fixture with 1 setup + 2 engine payloads; newPayloadVersion is a
	// string, as EEST writes it.
	fixture := `{
      "test_x": {
        "setupEngineNewPayloads": [
          {"newPayloadVersion": "5", "params": [{"blockNumber":"0x1","blockHash":"0xa"}, [], "0x0", []]}
        ],
        "engineNewPayloads": [
          {"newPayloadVersion": "5", "params": [{"blockNumber":"0x2","blockHash":"0xb"}, [], "0x0", []]},
          {"newPayloadVersion": "5", "params": [{"blockNumber":"0x3","blockHash":"0xc"}, [], "0x0", []]}
        ]
      }
    }`
	require.NoError(t, os.WriteFile(filepath.Join(sub, "test_x.json"), []byte(fixture), 0o644))
	// A non-fixture json file is skipped, not fatal.
	require.NoError(t, os.WriteFile(filepath.Join(sub, "report.json"), []byte(`{"summary": 1}`), 0o644))

	got, err := extractFixturePayloads(dir)
	require.NoError(t, err)
	require.Len(t, got, 3)
	for _, p := range got {
		assert.Equal(t, "engine_newPayloadV5", p.Method)
	}
}
