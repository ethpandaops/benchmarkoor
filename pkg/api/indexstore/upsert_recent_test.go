package indexstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
)

// A re-index of an existing run must overwrite every field, including fields
// reset to their zero value, and must not create a duplicate row.
func TestUpsertRunUpdatesAllFieldsIncludingZeros(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertRun(ctx, &indexstore.Run{
		DiscoveryPath: "dp", RunID: "run-1",
		Status: "completed", Client: "geth",
		TestsFailed: 5, TestsPassed: 10, TimestampEnd: 999, HasResult: true,
	}))

	require.NoError(t, s.UpsertRun(ctx, &indexstore.Run{
		DiscoveryPath: "dp", RunID: "run-1",
		Status: "failed", Client: "reth",
		TestsFailed: 0, TestsPassed: 7, TimestampEnd: 0, HasResult: false,
	}))

	got, err := s.GetRunByRunID(ctx, "run-1")
	require.NoError(t, err)

	assert.Equal(t, "failed", got.Status)
	assert.Equal(t, "reth", got.Client)
	assert.Equal(t, 7, got.TestsPassed)
	assert.Equal(t, 0, got.TestsFailed, "zero-valued field should persist")
	assert.Equal(t, int64(0), got.TimestampEnd, "zero-valued field should persist")
	assert.False(t, got.HasResult, "zero-valued field should persist")

	runs, err := s.ListRuns(ctx, "dp")
	require.NoError(t, err)
	assert.Len(t, runs, 1, "re-index should update in place, not duplicate")
}

// A re-index must update the run's mutable fields but preserve the original
// indexed_at (first-index time), recording the re-index time only in
// reindexed_at. Overwriting indexed_at would lose the first-index timestamp.
func TestUpsertRunPreservesIndexedAtOnReindex(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	firstIndexed := time.Unix(1000, 0).UTC()
	require.NoError(t, s.UpsertRun(ctx, &indexstore.Run{
		DiscoveryPath: "dp", RunID: "run-1",
		Status: "running", Client: "geth",
		IndexedAt: firstIndexed,
	}))

	reindexed := time.Unix(2000, 0).UTC()
	require.NoError(t, s.UpsertRun(ctx, &indexstore.Run{
		DiscoveryPath: "dp", RunID: "run-1",
		Status: "completed", Client: "geth",
		IndexedAt: reindexed, ReindexedAt: &reindexed,
	}))

	got, err := s.GetRunByRunID(ctx, "run-1")
	require.NoError(t, err)

	assert.Equal(t, "completed", got.Status, "mutable fields should update")
	assert.Equal(t, firstIndexed.Unix(), got.IndexedAt.Unix(),
		"indexed_at must remain the original first-index time")
	require.NotNil(t, got.ReindexedAt, "reindexed_at should be recorded")
	assert.Equal(t, reindexed.Unix(), got.ReindexedAt.Unix(),
		"reindexed_at must record the latest re-index")
}

func TestUpsertRunInsertsNewRun(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertRun(ctx, &indexstore.Run{
		DiscoveryPath: "dp", RunID: "run-1",
		Status: "completed", Client: "geth", HasResult: true,
	}))

	got, err := s.GetRunByRunID(ctx, "run-1")
	require.NoError(t, err)
	assert.Equal(t, "completed", got.Status)
	assert.Equal(t, "geth", got.Client)
	assert.True(t, got.HasResult)
}

func stat(suite, runID, testName, client string, runStart int64) *indexstore.TestStat {
	return &indexstore.TestStat{
		SuiteHash: suite,
		RunID:     runID,
		TestName:  testName,
		Client:    client,
		RunStart:  runStart,
	}
}

func distinctRunIDs(stats []indexstore.TestStat) []string {
	seen := make(map[string]struct{})
	var out []string

	for _, s := range stats {
		if _, ok := seen[s.RunID]; !ok {
			seen[s.RunID] = struct{}{}
			out = append(out, s.RunID)
		}
	}

	return out
}

// A run whose stats carry inconsistent run_start values must count as a single
// run, so it does not evict other recent runs from the per-client window.
func TestListRecentCountsInconsistentRunStartAsOneRun(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()
	suite := "suite-1"

	require.NoError(t, s.BulkUpsertTestStats(ctx, []*indexstore.TestStat{
		stat(suite, "run-A", "t1", "geth", 300),
		stat(suite, "run-A", "t2", "geth", 290), // inconsistent run_start for run-A
		stat(suite, "run-B", "t1", "geth", 200),
	}))

	got, err := s.ListTestStatsBySuiteRecent(ctx, suite, 2)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"run-A", "run-B"}, distinctRunIDs(got),
		"both recent runs should be returned despite run-A's inconsistent run_start")
}

func TestListRecentRespectsPerClientCap(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()
	suite := "suite-2"

	require.NoError(t, s.BulkUpsertTestStats(ctx, []*indexstore.TestStat{
		stat(suite, "run-A", "t1", "geth", 300),
		stat(suite, "run-B", "t1", "geth", 200),
		stat(suite, "run-C", "t1", "geth", 100),
	}))

	got, err := s.ListTestStatsBySuiteRecent(ctx, suite, 2)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"run-A", "run-B"}, distinctRunIDs(got),
		"only the 2 most recent runs should be returned")
}
