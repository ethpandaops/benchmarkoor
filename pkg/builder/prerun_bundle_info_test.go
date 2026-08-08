package builder

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// writeBundleFile lays down a .request bundle with the given payload blocks,
// padded so the last one is far enough from the end to exercise the backwards
// scan rather than landing in the first window by luck.
func writeBundleFile(t *testing.T, dir string, blocks [][3]string, pad int) string {
	t.Helper()

	bundleDir := filepath.Join(dir, config.PreRunBundleSubdir)
	require.NoError(t, os.MkdirAll(bundleDir, 0o755))

	var sb strings.Builder

	for _, b := range blocks {
		ep, err := json.Marshal(map[string]string{
			"blockNumber": b[0], "blockHash": b[1], "parentHash": b[2],
			"padding": strings.Repeat("a", pad),
		})
		require.NoError(t, err)

		sb.WriteString(`{"method":"engine_newPayloadV5","params":[` + string(ep) + "]}\n")
		sb.WriteString(`{"method":"engine_forkchoiceUpdatedV3","params":[{}]}` + "\n")
	}

	path := filepath.Join(bundleDir, "pre-run.request")
	require.NoError(t, os.WriteFile(path, []byte(sb.String()), 0o644))

	return path
}

func TestReadPreRunBundleInfoDerivesEndBlock(t *testing.T) {
	// An older artifact: bundle present, no sidecar.
	dir := t.TempDir()
	writeBundleFile(t, dir, [][3]string{
		{"0x64", "0xaa", "0xhead"},
		{"0x65", "0xbb", "0xaa"},
		{"0x66", "0xcc", "0xbb"},
	}, 0)

	info, err := ReadPreRunBundleInfo(dir)
	require.NoError(t, err)
	require.NotNil(t, info)

	assert.True(t, info.Derived(), "no sidecar means only the end block is known")
	assert.Equal(t, uint64(0x66), info.EndBlockNumber)
	assert.Equal(t, "0xcc", info.EndBlockHash)
	assert.Zero(t, info.StartBlockNumber)
}

func TestReadPreRunBundleInfoPrefersSidecar(t *testing.T) {
	dir := t.TempDir()
	writeBundleFile(t, dir, [][3]string{{"0x64", "0xaa", "0xhead"}}, 0)

	want := &PreRunBundleInfo{
		Payloads: 81, StartBlockNumber: 100, StartBlockHash: "0xstart",
		EndBlockNumber: 181, EndBlockHash: "0xend",
	}
	require.NoError(t, writeBundleMeta(dir, want))

	info, err := ReadPreRunBundleInfo(dir)
	require.NoError(t, err)
	assert.Equal(t, want, info, "the sidecar wins over deriving")
	assert.False(t, info.Derived())
}

// A single payload line carries a whole block, so the last one can sit further
// back than the initial tail window; the scan must widen rather than give up.
func TestReadPreRunBundleInfoDerivesAcrossLargeLines(t *testing.T) {
	dir := t.TempDir()
	writeBundleFile(t, dir, [][3]string{
		{"0x1", "0xa1", "0xh"},
		{"0x2", "0xa2", "0xa1"},
	}, 6<<20) // 6 MiB of padding per payload

	info, err := ReadPreRunBundleInfo(dir)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, uint64(2), info.EndBlockNumber)
	assert.Equal(t, "0xa2", info.EndBlockHash)
}

func TestReadPreRunBundleInfoNoBundleAtAll(t *testing.T) {
	// A replay-only target: neither sidecar nor bundle. Not an error.
	info, err := ReadPreRunBundleInfo(t.TempDir())
	require.NoError(t, err)
	assert.Nil(t, info)
}
