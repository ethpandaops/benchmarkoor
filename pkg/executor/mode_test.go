package executor

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The replay mode writes no "mode" key, so its files stay byte-identical to
// the files written before the mempool mode existed.
func TestReplayFilesHaveNoModeKey(t *testing.T) {
	for name, v := range map[string]any{
		"index entry": &IndexEntry{},
		"suite info":  &SuiteInfo{Source: &SuiteSource{}},
	} {
		data, err := json.Marshal(v)
		require.NoError(t, err, name)
		assert.NotContains(t, string(data), `"mode"`, name)
		assert.NotContains(t, string(data), `"mempool"`, name)
	}
}

func TestIndexEntryCarriesMode(t *testing.T) {
	entry, err := BuildIndexEntryFromData("r1", []byte(`{"timestamp":1,"mode":"mempool",
		"instance":{"id":"geth","client":"geth"}}`), nil)
	require.NoError(t, err)
	assert.Equal(t, "mempool", entry.Mode)

	entry, err = BuildIndexEntryFromData("r2", []byte(`{"timestamp":1,"instance":{"id":"geth"}}`), nil)
	require.NoError(t, err)
	assert.Empty(t, entry.Mode)
}

// bareExecutor has none of the optional capabilities.
type bareExecutor struct{ Executor }

type genesisExecutor struct{ bareExecutor }

func (genesisExecutor) GetGenesisPath(string) string { return "/g.json" }

func TestOptionalCapabilities(t *testing.T) {
	n, err := RunPreRunSteps(context.Background(), bareExecutor{}, &ExecuteOptions{})
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Nil(t, SourceOf(bareExecutor{}))
	assert.Nil(t, GenesisProviderOf(bareExecutor{}))

	gp := GenesisProviderOf(genesisExecutor{})
	require.NotNil(t, gp)
	assert.Equal(t, "/g.json", gp.GetGenesisPath("geth"))

	// The replay executor keeps both capabilities.
	var replay Executor = NewExecutor(logrus.New(), &Config{})
	_, isPreRunner := replay.(PreRunStepRunner)
	_, isSource := replay.(SourceProvider)
	assert.True(t, isPreRunner)
	assert.True(t, isSource)
}
