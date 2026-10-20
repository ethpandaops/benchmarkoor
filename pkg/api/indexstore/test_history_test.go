package indexstore_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
)

func historyRunIDs(entries []indexstore.TestHistoryEntry) []string {
	out := make([]string, 0, len(entries))
	for i := range entries {
		out = append(out, entries[i].RunID)
	}

	return out
}

// The history holds only the asked test, newest first, with the N most recent
// runs per client.
func TestListTestHistoryRespectsPerClientCap(t *testing.T) {
	s := setupTestStore(t)
	suite := "suite-h1"

	indexRun(t, s, "dp", suite, "run-A", "geth", 400, "t1", "t2")
	indexRun(t, s, "dp", suite, "run-B", "geth", 300, "t1")
	indexRun(t, s, "dp", suite, "run-C", "geth", 200, "t1")
	indexRun(t, s, "dp", suite, "run-D", "reth", 100, "t1", "t2")

	got, err := s.ListTestHistory(context.Background(), suite, "t1", 2)
	require.NoError(t, err)

	assert.Equal(t, []string{"run-A", "run-B", "run-D"}, historyRunIDs(got))

	for i := range got {
		assert.Equal(t, "t1", got[i].TestName)
	}
}

// A run that did not run the test must not take a per-client slot.
func TestListTestHistorySkipsRunsWithoutTheTest(t *testing.T) {
	s := setupTestStore(t)
	suite := "suite-h2"

	indexRun(t, s, "dp", suite, "run-A", "geth", 300, "t2")
	indexRun(t, s, "dp", suite, "run-B", "geth", 200, "t1")
	indexRun(t, s, "dp", suite, "run-C", "geth", 100, "t1")

	got, err := s.ListTestHistory(context.Background(), suite, "t1", 2)
	require.NoError(t, err)

	assert.Equal(t, []string{"run-B", "run-C"}, historyRunIDs(got))
}

// Each entry carries the client image of its run, and a run indexed under two
// discovery paths shows once.
func TestListTestHistoryCarriesTheImage(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()
	suite := "suite-h3"

	for _, dp := range []string{"dp/one", "dp/two"} {
		require.NoError(t, s.UpsertRun(ctx, &indexstore.Run{
			DiscoveryPath: dp, RunID: "run-A", SuiteHash: suite,
			Client: "geth", Image: "geth:v1", Timestamp: 100, HasResult: true,
		}))
	}

	require.NoError(t, s.BulkUpsertTestStats(ctx, []*indexstore.TestStat{
		stat(suite, "run-A", "t1", "geth", 100),
	}))

	got, err := s.ListTestHistory(ctx, suite, "t1", 5)
	require.NoError(t, err)

	require.Len(t, got, 1)
	assert.Equal(t, "geth:v1", got[0].Image)
}

// The walk crosses batch boundaries and keeps the newest runs.
func TestListTestHistoryCrossesBatches(t *testing.T) {
	s := setupTestStore(t)
	suite := "suite-h4"

	const runs = 1200
	for i := range runs {
		indexRun(t, s, "dp", suite, fmt.Sprintf("run-%04d", i), "geth", int64(i), "t1")
	}

	got, err := s.ListTestHistory(context.Background(), suite, "t1", 700)
	require.NoError(t, err)

	require.Len(t, got, 700)
	assert.Equal(t, "run-1199", got[0].RunID)
	assert.Equal(t, "run-0500", got[699].RunID)
}

// An unknown suite or test gives an empty history, not an error.
func TestListTestHistoryEmpty(t *testing.T) {
	s := setupTestStore(t)

	indexRun(t, s, "dp", "suite-h5", "run-A", "geth", 100, "t1")

	got, err := s.ListTestHistory(context.Background(), "suite-h5", "missing", 5)
	require.NoError(t, err)
	assert.Empty(t, got)

	got, err = s.ListTestHistory(context.Background(), "missing", "t1", 5)
	require.NoError(t, err)
	assert.Empty(t, got)
}
