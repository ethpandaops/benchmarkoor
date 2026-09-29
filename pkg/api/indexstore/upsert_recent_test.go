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

// indexRun records a run and one test stat per test name, the way the indexer
// does: the stats carry the run's client and timestamp.
func indexRun(
	t *testing.T, s indexstore.Store,
	dp, suite, runID, client string, timestamp int64, tests ...string,
) {
	t.Helper()

	ctx := context.Background()

	require.NoError(t, s.UpsertRun(ctx, &indexstore.Run{
		DiscoveryPath: dp, RunID: runID, SuiteHash: suite,
		Client: client, Timestamp: timestamp, HasResult: len(tests) > 0,
	}))

	stats := make([]*indexstore.TestStat, 0, len(tests))
	for _, test := range tests {
		stats = append(stats, stat(suite, runID, test, client, timestamp))
	}

	if len(stats) > 0 {
		require.NoError(t, s.BulkUpsertTestStats(ctx, stats))
	}
}

func TestListRecentRespectsPerClientCap(t *testing.T) {
	s := setupTestStore(t)
	suite := "suite-2"

	indexRun(t, s, "dp", suite, "run-A", "geth", 300, "t1", "t2")
	indexRun(t, s, "dp", suite, "run-B", "geth", 200, "t1", "t2")
	indexRun(t, s, "dp", suite, "run-C", "geth", 100, "t1", "t2")
	indexRun(t, s, "dp", suite, "run-D", "reth", 50, "t1")

	got, err := s.ListTestStatsBySuiteRecent(context.Background(), suite, 2)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"run-A", "run-B", "run-D"},
		distinctRunIDs(got),
		"the 2 most recent runs per client should be returned")
	assert.Len(t, got, 5, "every test stat of a selected run is returned")
}

// A run with no test stats, such as one whose result.json had no step data,
// must not take a per-client slot from a run that has them.
func TestListRecentSkipsRunsWithoutTestStats(t *testing.T) {
	s := setupTestStore(t)
	suite := "suite-3"

	indexRun(t, s, "dp", suite, "run-A", "geth", 300)
	indexRun(t, s, "dp", suite, "run-B", "geth", 200, "t1")
	indexRun(t, s, "dp", suite, "run-C", "geth", 100, "t1")

	got, err := s.ListTestStatsBySuiteRecent(context.Background(), suite, 2)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"run-B", "run-C"}, distinctRunIDs(got))
}

// A run indexed under two discovery paths has two runs rows but one set of
// test stats. It must count as a single run in the per-client window.
func TestListRecentCountsARunInTwoPathsOnce(t *testing.T) {
	s := setupTestStore(t)
	suite := "suite-4"

	indexRun(t, s, "dp/one", suite, "run-A", "geth", 300, "t1")
	indexRun(t, s, "dp/one", suite, "run-B", "geth", 200, "t1")

	// test_stats has no discovery path, so the second path adds a runs row
	// only.
	require.NoError(t, s.UpsertRun(context.Background(), &indexstore.Run{
		DiscoveryPath: "dp/two", RunID: "run-A", SuiteHash: suite,
		Client: "geth", Timestamp: 300, HasResult: true,
	}))

	got, err := s.ListTestStatsBySuiteRecent(context.Background(), suite, 2)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"run-A", "run-B"}, distinctRunIDs(got))
}

// A run ID that also exists in another suite must not pull that suite's
// stats in, nor count toward this suite's window.
func TestListRecentStaysInsideTheSuite(t *testing.T) {
	s := setupTestStore(t)

	indexRun(t, s, "dp", "suite-5", "run-A", "geth", 300, "t1")
	indexRun(t, s, "dp", "suite-other", "run-B", "geth", 400, "t1")

	got, err := s.ListTestStatsBySuiteRecent(context.Background(), "suite-5", 2)
	require.NoError(t, err)

	require.Len(t, got, 1)
	assert.Equal(t, "run-A", got[0].RunID)
	assert.Equal(t, "suite-5", got[0].SuiteHash)
}
