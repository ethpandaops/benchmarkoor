package runner

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

func TestExecutorModeInRunConfig(t *testing.T) {
	replay := &config.Config{}
	assert.Empty(t, executorMode(replay))
	assert.Empty(t, executorMode(nil))

	data, err := json.Marshal(&RunConfig{Mode: executorMode(replay)})
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"mode"`, "replay config.json stays as before")

	mempool := &config.Config{}
	mempool.Runner.Benchmark.Mode = config.ExecutorModeMempool
	assert.Equal(t, config.ExecutorModeMempool, executorMode(mempool))

	s := mempoolSettings(mempool)
	assert.Equal(t, uint64(config.DefaultMempoolGasLimit), s.GasLimit)
	assert.Equal(t, config.DefaultMempoolAccountSlots, s.Limits.AccountSlots)
}
