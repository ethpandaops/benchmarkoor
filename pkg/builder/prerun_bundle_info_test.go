package builder

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

// parentedPayload extends the package's payload helper with the parentHash that
// PreRunBundleInfo needs to identify the block a bundle attaches to.
func parentedPayload(number, hash, parent string) recordedPayload {
	ep, err := json.Marshal(map[string]string{
		"blockNumber": number,
		"blockHash":   hash,
		"parentHash":  parent,
	})
	if err != nil {
		panic(err)
	}

	return recordedPayload{Method: "engine_newPayloadV5", Params: []json.RawMessage{ep}}
}

func TestSummarizeBundle(t *testing.T) {
	t.Run("start block is the parent of the first payload", func(t *testing.T) {
		ordered := []recordedPayload{
			parentedPayload("0x65", "0xaa", "0xhead"),
			parentedPayload("0x66", "0xbb", "0xaa"),
			parentedPayload("0x67", "0xcc", "0xbb"),
		}

		info, err := summarizeBundle(ordered)
		require.NoError(t, err)

		// The snapshot head the bundle replays onto — one below its first payload.
		assert.Equal(t, uint64(100), info.StartBlockNumber)
		assert.Equal(t, "0xhead", info.StartBlockHash)
		assert.Equal(t, uint64(103), info.EndBlockNumber)
		assert.Equal(t, "0xcc", info.EndBlockHash)
		assert.Equal(t, 3, info.Payloads)
		assert.True(t, info.Contiguous())
	})

	t.Run("payload count is counted, not derived", func(t *testing.T) {
		// A gap at 102: 2 payloads spanning 3 blocks.
		info, err := summarizeBundle([]recordedPayload{
			parentedPayload("0x65", "0xaa", "0xhead"),
			parentedPayload("0x67", "0xcc", "0xbb"),
		})
		require.NoError(t, err)
		assert.Equal(t, 2, info.Payloads)
		assert.Equal(t, uint64(100), info.StartBlockNumber)
		assert.Equal(t, uint64(103), info.EndBlockNumber)
		assert.False(t, info.Contiguous(), "a gap must be visible, not averaged away")
	})

	t.Run("empty bundle rejected", func(t *testing.T) {
		_, err := summarizeBundle(nil)
		require.ErrorContains(t, err, "no payloads")
	})
}

func TestPreRunBundleInfoRoundTrip(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, config.PreRunBundleSubdir), 0o755))

	want := &PreRunBundleInfo{
		Payloads: 81, StartBlockNumber: 24402727, StartBlockHash: "0xhead",
		EndBlockNumber: 24402808, EndBlockHash: "0xtip",
	}
	require.NoError(t, writeBundleMeta(dir, want))

	got, err := ReadPreRunBundleInfo(dir)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestReadPreRunBundleInfoAbsent(t *testing.T) {
	// A replay-only target records no bundle; that is not an error.
	got, err := ReadPreRunBundleInfo(t.TempDir())
	require.NoError(t, err)
	assert.Nil(t, got)

	got, err = ReadPreRunBundleInfo("")
	require.NoError(t, err)
	assert.Nil(t, got)
}
