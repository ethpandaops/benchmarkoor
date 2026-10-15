package indexer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/executor"
)

// A test whose payload the client must reject has no gas. Its row keeps the
// time to reject in test_time_ns and adds it to total_time_ns, while
// total_mgas_s stays on the gas-reporting (setup) time.
func TestIndexTestStats_RejectedTest(t *testing.T) {
	f := newIndexerFixture(t, Options{})
	ctx := context.Background()

	result := []byte(`{"tests": {"invalid": {"dir": "1", "steps": {
		"setup": {"aggregated": {"time_total": 30, "gas_used_total": 10000000,
			"gas_used_time_total": 20000000, "success": 1, "fail": 0, "msg_count": 2}},
		"test": {"aggregated": {"time_total": 47000000, "gas_used_total": 0,
			"gas_used_time_total": 0, "rejected": 1, "rejected_time_total": 47000000,
			"success": 1, "fail": 0, "msg_count": 1}}}},
		"valid": {"dir": "2", "steps": {
		"test": {"aggregated": {"time_total": 10000000, "gas_used_total": 10000000,
			"gas_used_time_total": 10000000, "success": 1, "fail": 0, "msg_count": 2}}}}}}`)

	entry := &executor.IndexEntry{
		Instance:  &executor.IndexInstance{Client: "geth"},
		Timestamp: 100,
	}
	require.NoError(t, f.idx.indexTestStats(ctx, "suite", "run-1", entry, result))

	rows, err := f.store.ListTestStatsBySuite(ctx, "suite")
	require.NoError(t, err)
	require.Len(t, rows, 2)

	byName := map[string]int{rows[0].TestName: 0, rows[1].TestName: 1}

	invalid := rows[byName["invalid"]]
	assert.True(t, invalid.TestRejected)
	assert.Equal(t, uint64(0), invalid.TestGasUsed)
	assert.Zero(t, invalid.TestMGasS)
	assert.Equal(t, int64(47_000_000), invalid.TestTimeNs)
	assert.Equal(t, int64(67_000_000), invalid.TotalTimeNs)
	// 10M gas over the 20ms setup gas time, not over the 67ms total.
	assert.InDelta(t, 500.0, invalid.TotalMGasS, 0.001)

	valid := rows[byName["valid"]]
	assert.False(t, valid.TestRejected)
	assert.Equal(t, int64(10_000_000), valid.TestTimeNs)
	assert.Equal(t, int64(10_000_000), valid.TotalTimeNs)
}
