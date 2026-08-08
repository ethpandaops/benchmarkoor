package builder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFixture drops a stateful fixture with the given payload blocks into a
// fresh fixtures dir, plus a non-fixture json file that must be ignored.
func writeFixture(t *testing.T, setup, bench [][2]string) string {
	t.Helper()

	dir := t.TempDir()
	sub := filepath.Join(dir, "blockchain_tests_stateful_engine", "x")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	entry := func(b [2]string) string {
		return `{"newPayloadVersion": "5", "params": [{"blockNumber":"` + b[0] +
			`","blockHash":"` + b[1] + `"}, [], "0x0", []]}`
	}

	parts := func(bs [][2]string) string {
		out := make([]string, 0, len(bs))
		for _, b := range bs {
			out = append(out, entry(b))
		}

		return strings.Join(out, ",")
	}

	fixture := `{"test_x": {
        "network": "Amsterdam",
        "setupEngineNewPayloads": [` + parts(setup) + `],
        "engineNewPayloads": [` + parts(bench) + `]
      }}`

	require.NoError(t, os.WriteFile(filepath.Join(sub, "test_x.json"), []byte(fixture), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "report.json"), []byte(`{"summary": 1}`), 0o644))

	return dir
}

// TestStreamingBundleMatchesBufferedPath is the load-bearing test: the streaming
// writer must produce byte-identical output to the accumulate-sort-write path it
// replaces, or this is a behaviour change rather than a memory fix.
func TestStreamingBundleMatchesBufferedPath(t *testing.T) {
	recorded := []recordedPayload{
		payload("engine_newPayloadV4", "0x1", "0xa1"),
		payload("engine_newPayloadV4", "0x2", "0xa2"),
	}
	fixtures := writeFixture(t,
		[][2]string{{"0x3", "0xa3"}},
		[][2]string{{"0x4", "0xa4"}, {"0x5", "0xa5"}},
	)

	// Old path.
	fixturePayloads, err := extractFixturePayloads(fixtures)
	require.NoError(t, err)
	ordered, err := sortAndDedupPayloads(append(append([]recordedPayload{}, recorded...), fixturePayloads...))
	require.NoError(t, err)
	oldDir := t.TempDir()
	oldPath, err := writeRequestBundle(oldDir, ordered)
	require.NoError(t, err)
	oldData, err := os.ReadFile(oldPath)
	require.NoError(t, err)

	// New path.
	newDir := t.TempDir()
	bw, err := newBundleWriter(newDir)
	require.NoError(t, err)

	for i := range recorded {
		require.NoError(t, bw.emit(recorded[i]))
	}

	require.NoError(t, streamFixtureDir(fixtures, bw.emit))
	require.NoError(t, bw.close())

	newData, err := os.ReadFile(filepath.Join(newDir, config.PreRunBundleSubdir, preRunBundleFile))
	require.NoError(t, err)

	assert.Equal(t, string(oldData), string(newData), "streaming output must match the buffered path byte for byte")
	assert.Equal(t, 5, bw.count)
}

// TestStreamingBundleInfoMatchesSummarize checks the incremental summary against
// summarizeBundle. The streaming writer cannot call it (there is no retained
// slice), so it accumulates the first and last block refs instead; the two must
// agree, or PreRunBundleInfo — and the contiguity check built on it — would
// silently drift.
func TestStreamingBundleInfoMatchesSummarize(t *testing.T) {
	payloads := []recordedPayload{
		parentedPayload("0x5", "0xe5", "0xe4"),
		parentedPayload("0x6", "0xe6", "0xe5"),
		parentedPayload("0x7", "0xe7", "0xe6"),
	}

	want, err := summarizeBundle(payloads)
	require.NoError(t, err)

	bw, err := newBundleWriter(t.TempDir())
	require.NoError(t, err)

	for i := range payloads {
		require.NoError(t, bw.emit(payloads[i]))
	}

	require.NoError(t, bw.close())

	got, err := bw.info()
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.True(t, got.Contiguous())
}

// TestStreamingBundleDropsDuplicateBlocks mirrors sortAndDedupPayloads: a block
// already written (same hash) is skipped rather than emitted twice.
func TestStreamingBundleDropsDuplicateBlocks(t *testing.T) {
	// The fill's setup payloads commonly repeat a block the pre-run already
	// recorded; that must not appear twice in the bundle.
	recorded := []recordedPayload{payload("engine_newPayloadV5", "0x1", "0xa1")}
	fixtures := writeFixture(t,
		[][2]string{{"0x1", "0xa1"}}, // duplicate of the recorded block
		[][2]string{{"0x2", "0xa2"}},
	)

	dir := t.TempDir()
	bw, err := newBundleWriter(dir)
	require.NoError(t, err)
	require.NoError(t, bw.emit(recorded[0]))
	require.NoError(t, streamFixtureDir(fixtures, bw.emit))
	require.NoError(t, bw.close())

	assert.Equal(t, 2, bw.count, "duplicate block dropped")

	data, err := os.ReadFile(filepath.Join(dir, config.PreRunBundleSubdir, preRunBundleFile))
	require.NoError(t, err)
	assert.Len(t, strings.Split(strings.TrimSpace(string(data)), "\n"), 4, "two pairs")
}

// TestStreamingBundleRejectsOutOfOrder pins the invariant the streaming writer
// relies on. It cannot reorder, so it must fail loudly rather than emit a bundle
// whose blocks do not chain.
func TestStreamingBundleRejectsOutOfOrder(t *testing.T) {
	dir := t.TempDir()
	bw, err := newBundleWriter(dir)
	require.NoError(t, err)

	require.NoError(t, bw.emit(payload("engine_newPayloadV5", "0x2", "0xb")))

	err = bw.emit(payload("engine_newPayloadV5", "0x1", "0xa"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of order")
	require.NoError(t, bw.close())
}

// TestStreamFixturePayloadsSkipsNonFixtures keeps extractFixturePayloads'
// tolerance: a .meta report next to the fixtures is not a failure.
func TestStreamFixturePayloadsSkipsNonFixtures(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "report.json"), []byte(`{"summary": 1}`), 0o644))

	seen := 0
	require.NoError(t, streamFixtureDir(dir, func(recordedPayload) error {
		seen++

		return nil
	}))
	assert.Zero(t, seen)
}
