package indexer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/executor"
)

// A payload the client must reject has no gas. Its time to reject goes to
// the step's rejected time, apart from the step time that is the MGas/s
// denominator. total_time_ns adds it, while total_mgas_s stays on the
// gas-reporting time.
func TestIndexTestStats_RejectedTest(t *testing.T) {
	f := newIndexerFixture(t, Options{})
	ctx := context.Background()

	// invalid: a rejected payload in the test step.
	// setup_rejected: a rejected payload in the setup step (a non-last
	// payload of a plain blockchain_test_engine fixture).
	// mixed: a valid and a rejected payload in the same test step.
	result := []byte(`{"tests": {"invalid": {"dir": "1", "steps": {
		"setup": {"aggregated": {"time_total": 30, "gas_used_total": 10000000,
			"gas_used_time_total": 20000000, "success": 1, "fail": 0, "msg_count": 2}},
		"test": {"aggregated": {"time_total": 47000000, "gas_used_total": 0,
			"gas_used_time_total": 0, "rejected": 1, "rejected_time_total": 47000000,
			"success": 1, "fail": 0, "msg_count": 1}}}},
		"setup_rejected": {"dir": "2", "steps": {
		"setup": {"aggregated": {"time_total": 33000000, "gas_used_total": 10000000,
			"gas_used_time_total": 20000000, "rejected": 1, "rejected_time_total": 13000000,
			"success": 3, "fail": 0, "msg_count": 3}},
		"test": {"aggregated": {"time_total": 10000000, "gas_used_total": 10000000,
			"gas_used_time_total": 10000000, "success": 2, "fail": 0, "msg_count": 2}}}},
		"mixed": {"dir": "3", "steps": {
		"test": {"aggregated": {"time_total": 30000000, "gas_used_total": 20000000,
			"gas_used_time_total": 10000000, "rejected": 1, "rejected_time_total": 20000000,
			"success": 3, "fail": 0, "msg_count": 3}}}},
		"valid": {"dir": "4", "steps": {
		"test": {"aggregated": {"time_total": 10000000, "gas_used_total": 10000000,
			"gas_used_time_total": 10000000, "success": 1, "fail": 0, "msg_count": 2}}}}}}`)

	entry := &executor.IndexEntry{
		Instance:  &executor.IndexInstance{Client: "geth"},
		Timestamp: 100,
	}
	require.NoError(t, f.idx.indexTestStats(ctx, "suite", "run-1", entry, result))

	rows, err := f.store.ListTestStatsBySuite(ctx, "suite")
	require.NoError(t, err)
	require.Len(t, rows, 4)

	byName := make(map[string]int, len(rows))
	for i := range rows {
		byName[rows[i].TestName] = i
	}

	invalid := rows[byName["invalid"]]
	assert.Equal(t, int64(47_000_000), invalid.TestRejectedTimeNs)
	assert.Zero(t, invalid.SetupRejectedTimeNs)
	assert.Equal(t, uint64(0), invalid.TestGasUsed)
	assert.Zero(t, invalid.TestTimeNs)
	assert.Zero(t, invalid.TestMGasS)
	assert.Equal(t, int64(67_000_000), invalid.TotalTimeNs)
	// 10M gas over the 20ms setup gas time, not over the 67ms total.
	assert.InDelta(t, 500.0, invalid.TotalMGasS, 0.001)

	setupRejected := rows[byName["setup_rejected"]]
	assert.Equal(t, int64(13_000_000), setupRejected.SetupRejectedTimeNs)
	assert.Equal(t, int64(20_000_000), setupRejected.SetupTimeNs)
	assert.Zero(t, setupRejected.TestRejectedTimeNs)
	assert.Equal(t, int64(43_000_000), setupRejected.TotalTimeNs)

	// The gas time stays the denominator of test_mgas_s.
	mixed := rows[byName["mixed"]]
	assert.Equal(t, int64(20_000_000), mixed.TestRejectedTimeNs)
	assert.Equal(t, int64(10_000_000), mixed.TestTimeNs)
	assert.InDelta(t, 2000.0, mixed.TestMGasS, 0.001)
	assert.Equal(t, int64(30_000_000), mixed.TotalTimeNs)

	valid := rows[byName["valid"]]
	assert.Zero(t, valid.TestRejectedTimeNs)
	assert.Equal(t, int64(10_000_000), valid.TestTimeNs)
	assert.Equal(t, int64(10_000_000), valid.TotalTimeNs)
}
