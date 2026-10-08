package snapshot

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func headBlock(number string) []byte {
	return []byte(`{"jsonrpc":"2.0","id":1,"result":{"number":"` + number + `","hash":"0xabc","transactions":[]}}`)
}

func TestCheckHeadBlock(t *testing.T) {
	require.NoError(t, checkHeadBlock(headBlock("0x18f9b52"), 26188626))
	require.ErrorContains(t, checkHeadBlock(headBlock("0x18f9b52"), 26188082), "head block is 26188626")
	require.ErrorContains(t, checkHeadBlock([]byte(`{"number":"0x1","hash":"0xabc"}`), 1), "no result.hash")
	require.ErrorContains(t, checkHeadBlock([]byte(`{"result":null}`), 1), "no result.hash")
	require.Error(t, checkHeadBlock([]byte(`not json`), 1))
}

func TestBuildMetadata(t *testing.T) {
	var extra map[string]any
	require.NoError(t, json.Unmarshal([]byte(`{
		"docker_image": "ethereum/client-go:v1.17.7",
		"static": {"extra_args": ""},
		"shadowfork": {"amsterdam_time": 1791364586, "head": 26188082}
	}`), &extra))

	raw, err := buildMetadata(1437642000467, extra)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.InDelta(t, 1437642000467, got["data_size_bytes"], 0)
	assert.Equal(t, "ethereum/client-go:v1.17.7", got["docker_image"])
	assert.InDelta(t, 1791364586, got["shadowfork"].(map[string]any)["amsterdam_time"], 0)

	// The metadata file wins, objects merged key by key.
	raw, err = buildMetadata(1, map[string]any{"data_size_bytes": 2})
	require.NoError(t, err)
	assert.JSONEq(t, `{"data_size_bytes": 2}`, string(raw))

	assert.Equal(t, map[string]any{"a": map[string]any{"x": 1, "y": 2}},
		merge(map[string]any{"a": map[string]any{"x": 1}}, map[string]any{"a": map[string]any{"y": 2}}))
}

func TestCheckedReader(t *testing.T) {
	ok := &checkedReader{r: strings.NewReader("data"), wait: func() error { return nil }}
	b, err := io.ReadAll(ok)
	require.NoError(t, err)
	assert.Equal(t, "data", string(b))

	failed := &checkedReader{r: strings.NewReader("trunc"), wait: func() error { return errors.New("tar: exit status 2") }}
	_, err = io.ReadAll(failed)
	require.ErrorContains(t, err, "packing: tar: exit status 2", "a failed producer is a read error, never EOF")
	_, err = failed.Read(make([]byte, 1))
	require.Error(t, err, "and stays one")
}
