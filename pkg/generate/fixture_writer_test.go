package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFixtureWriterInit(t *testing.T) {
	dir := t.TempDir()
	log := logrus.New()
	w := NewFixtureWriter(log, dir)

	require.NoError(t, w.Init())

	// Verify directories were created.
	for _, subdir := range []string{"setup", "testing", "cleanup"} {
		info, err := os.Stat(filepath.Join(dir, subdir))
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	}
}

func TestFixtureWriterWriteTestBlock(t *testing.T) {
	dir := t.TempDir()
	log := logrus.New()
	w := NewFixtureWriter(log, dir)
	require.NoError(t, w.Init())

	testID := "tests/test_add.py::test_add[fork_Prague]"
	scenario := "test_add.py__test_add[fork_Prague]"

	block := &CapturedBlock{
		NewPayloadRequest: `{"jsonrpc":"2.0","method":"engine_newPayloadV4","params":[]}`,
		FCURequest:        `{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3","params":[]}`,
		BlockHash:         "0xabc",
		BlockNumber:       1,
	}

	// Write first testing block.
	require.NoError(t, w.WriteTestBlock(testID, "testing", block))

	// Verify testing file exists.
	testingContent, err := os.ReadFile(filepath.Join(dir, "testing", scenario+".txt"))
	require.NoError(t, err)
	assert.Contains(t, string(testingContent), "engine_newPayloadV4")
	assert.Contains(t, string(testingContent), "engine_forkchoiceUpdatedV3")

	// Write second testing block (should migrate first to setup).
	block2 := &CapturedBlock{
		NewPayloadRequest: `{"jsonrpc":"2.0","method":"engine_newPayloadV4","params":["second"]}`,
		FCURequest:        `{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3","params":["second"]}`,
		BlockHash:         "0xdef",
		BlockNumber:       2,
	}

	require.NoError(t, w.WriteTestBlock(testID, "testing", block2))

	// Verify testing now has second block.
	testingContent2, err := os.ReadFile(filepath.Join(dir, "testing", scenario+".txt"))
	require.NoError(t, err)
	assert.Contains(t, string(testingContent2), `"second"`)

	// Verify setup has first block.
	setupContent, err := os.ReadFile(filepath.Join(dir, "setup", scenario+".txt"))
	require.NoError(t, err)
	assert.Contains(t, string(setupContent), "engine_newPayloadV4")
	assert.NotContains(t, string(setupContent), `"second"`)
}

func TestFixtureWriterSetupPhase(t *testing.T) {
	dir := t.TempDir()
	log := logrus.New()
	w := NewFixtureWriter(log, dir)
	require.NoError(t, w.Init())

	testID := "tests/test_add.py::test_add[fork_Prague]"
	scenario := "test_add.py__test_add[fork_Prague]"

	block := &CapturedBlock{
		NewPayloadRequest: `{"jsonrpc":"2.0","method":"engine_newPayloadV4"}`,
		FCURequest:        `{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3"}`,
	}

	require.NoError(t, w.WriteTestBlock(testID, "setup", block))

	content, err := os.ReadFile(filepath.Join(dir, "setup", scenario+".txt"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "engine_newPayloadV4")
}

func TestFixtureWriterGasBump(t *testing.T) {
	dir := t.TempDir()
	log := logrus.New()
	w := NewFixtureWriter(log, dir)
	require.NoError(t, w.Init())

	block := &CapturedBlock{
		NewPayloadRequest: `{"jsonrpc":"2.0","method":"engine_newPayloadV4"}`,
		FCURequest:        `{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3"}`,
	}

	require.NoError(t, w.WriteGasBumpBlock(block))

	content, err := os.ReadFile(filepath.Join(dir, "gas-bump.txt"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "engine_newPayloadV4")
}
