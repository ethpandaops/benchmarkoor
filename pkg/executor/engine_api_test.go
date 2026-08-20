package executor

import (
	"encoding/json"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/jsonrpc"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractServerTiming(t *testing.T) {
	timing := extractServerTiming("reth_newPayload", `{
  "jsonrpc":"2.0","id":1,
  "result":{"status":"VALID","latency_us":123,"persistence_wait_us":7,
    "execution_cache_wait_us":3,"sparse_trie_wait_us":5}
}`)
	require.NotNil(t, timing)
	assert.Equal(t, int64(123_000), timing.ExecutionNS)
	assert.Equal(t, int64(7_000), timing.PersistenceWaitNS)
	require.NotNil(t, timing.ExecutionCacheWaitNS)
	assert.Equal(t, int64(3_000), *timing.ExecutionCacheWaitNS)
	require.NotNil(t, timing.SparseTrieWaitNS)
	assert.Equal(t, int64(5_000), *timing.SparseTrieWaitNS)
}

func TestRethPayloadSizeAndMetadataTxCount(t *testing.T) {
	line := `{"jsonrpc":"2.0","id":1,"method":"reth_newPayload","params":[{"block":"0xc0","bal":"0xc1"},true,true]}`
	sizes := ComputePayloadSizeBuckets(logrus.New(), "tempo", []string{line})
	assert.Equal(t, []uint64{1}, sizes.RLPFull)
	assert.Equal(t, []uint64{1}, sizes.RLPBAL)
	require.Len(t, sizes.RLPFullSnappy, 1)

	count := uint64(7)
	assert.Equal(t, []uint64{7}, transactionCountsFromMetadata([]*RequestMetadata{
		{TransactionCount: &count},
		nil,
	}))
}

func TestValidateExpectedInvalidPayload(t *testing.T) {
	e := &executor{validator: jsonrpc.DefaultValidator()}
	resp, err := jsonrpc.Parse(`{
  "jsonrpc":"2.0","id":1,
  "result":{"status":"INVALID","validationError":"invalid state root"}
}`)
	require.NoError(t, err)

	err = e.validateEngineResponse("reth_newPayload", resp, &RequestMetadata{
		ExpectedStatus:          "INVALID",
		ValidationErrorContains: "state root",
	})
	require.NoError(t, err)
}

func TestResultUsesManifestGasAndServerExecutionTime(t *testing.T) {
	gas := uint64(42_000_000)
	r := NewTestResult("tempo")
	r.AddResultWithMetadata(
		"reth_newPayload", `{}`, `{}`, 2_000_000, true, nil,
		&RequestMetadata{GasUsed: &gas},
		&ServerTiming{ExecutionNS: 1_000_000},
	)

	stats := r.CalculateStats()
	assert.Equal(t, gas, stats.GasUsedTotal)
	assert.Equal(t, int64(1_000_000), stats.GasUsedTimeTotal)
	assert.Equal(t, "server_execution", stats.GasUsedTimeSource)
	assert.InDelta(t, 42_000.0, r.MGasPerSec[0], 0.001)
}

func TestResultIncludesEmptyPayloadTimeInExecutionDenominator(t *testing.T) {
	gas := uint64(0)
	r := NewTestResult("tempo-empty")
	r.AddResultWithMetadata(
		"reth_newPayload", `{}`, `{}`, 2_000_000, true, nil,
		&RequestMetadata{GasUsed: &gas},
		&ServerTiming{ExecutionNS: 1_000_000},
	)

	stats := r.CalculateStats()
	assert.Zero(t, stats.GasUsedTotal)
	assert.Equal(t, int64(1_000_000), stats.GasUsedTimeTotal)
	assert.Equal(t, "server_execution", stats.GasUsedTimeSource)
}

func TestSuiteStatsScoresOnlyMeasuredPhase(t *testing.T) {
	result := RunResult{Tests: map[string]*TestEntry{
		"tempo": {Steps: &StepsResult{
			Setup: &StepResult{Aggregated: &AggregatedStats{
				GasUsedTotal: 10, GasUsedTimeTotal: 100, GasUsedTimeSource: "client_http",
			}},
			Test: &StepResult{Aggregated: &AggregatedStats{
				GasUsedTotal: 20, GasUsedTimeTotal: 200, GasUsedTimeSource: "server_execution",
			}},
			Cleanup: &StepResult{Aggregated: &AggregatedStats{
				GasUsedTotal: 30, GasUsedTimeTotal: 300, GasUsedTimeSource: "client_http",
			}},
		}},
	}}
	encoded, err := json.Marshal(result)
	require.NoError(t, err)

	stats := SuiteStats{}
	AccumulateRunResult(&stats, encoded, RunInfo{RunID: "run", Client: "tempo"})
	duration := stats["tempo"].Durations[0]
	assert.Equal(t, uint64(20), duration.GasUsed)
	assert.Equal(t, int64(200), duration.Time)
	assert.Equal(t, "server_execution", duration.TimeSource)
	assert.Equal(t, uint64(10), duration.Steps.Setup.GasUsed)
	assert.Equal(t, uint64(30), duration.Steps.Cleanup.GasUsed)
}
