package indexstore_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

func tableRows(t *testing.T, stats *indexstore.DatabaseStats, name string) int64 {
	t.Helper()

	for _, table := range stats.Tables {
		if table.Name == name {
			return table.Rows
		}
	}

	t.Fatalf("table %q missing from the report", name)

	return 0
}

// TestStore_DatabaseStats covers the row counts, the run span and the
// per-suite breakdown an admin uses to decide what to clean up.
func TestStore_DatabaseStats(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	// Two suites of different weight, so the breakdown has something to rank.
	require.NoError(t, s.UpsertSuite(ctx, &indexstore.Suite{
		SuiteHash: "suite-big", DiscoveryPath: "dp/one", Name: "Big suite",
	}))
	require.NoError(t, s.UpsertSuite(ctx, &indexstore.Suite{
		SuiteHash: "suite-small", DiscoveryPath: "dp/one", Name: "Small suite",
	}))

	for i, spec := range []struct {
		runID     string
		suiteHash string
		timestamp int64
	}{
		{runID: "run-1", suiteHash: "suite-big", timestamp: 100},
		{runID: "run-2", suiteHash: "suite-big", timestamp: 300},
		{runID: "run-3", suiteHash: "suite-big", timestamp: 200},
		{runID: "run-4", suiteHash: "suite-small", timestamp: 150},
	} {
		require.NoError(t, s.UpsertRun(ctx, &indexstore.Run{
			DiscoveryPath: "dp/one",
			RunID:         spec.runID,
			SuiteHash:     spec.suiteHash,
			Timestamp:     spec.timestamp,
			Status:        "completed",
		}))

		require.NoError(t, s.UpsertTestStat(ctx, &indexstore.TestStat{
			SuiteHash: spec.suiteHash,
			RunID:     spec.runID,
			TestName:  "test-a",
			Client:    "geth",
			RunStart:  int64(i),
		}))
	}

	// A run with no suite must not become a phantom row in the breakdown.
	require.NoError(t, s.UpsertRun(ctx, &indexstore.Run{
		DiscoveryPath: "dp/one", RunID: "run-orphan", Timestamp: 50,
	}))

	require.NoError(t, s.RecordIndexFailure(ctx, "dp/one", "100_a_geth", "x"))

	stats, err := s.DatabaseStats(ctx)
	require.NoError(t, err)

	assert.Equal(t, "sqlite", stats.Driver)
	assert.Equal(t, int64(5), tableRows(t, stats, "runs"))
	assert.Equal(t, int64(4), tableRows(t, stats, "test_stats"))
	assert.Equal(t, int64(2), tableRows(t, stats, "suites"))
	assert.Equal(t, int64(1), tableRows(t, stats, "index_failures"))
	assert.Equal(t, int64(0), tableRows(t, stats, "test_stats_block_logs"))
	assert.Equal(t, int64(0), tableRows(t, stats, "live_runs"))

	assert.Equal(t, int64(50), stats.OldestRun)
	assert.Equal(t, int64(300), stats.NewestRun)

	require.Len(t, stats.TopSuites, 2, "the suiteless run must not rank")

	biggest := stats.TopSuites[0]
	assert.Equal(t, "suite-big", biggest.SuiteHash)
	assert.Equal(t, "Big suite", biggest.Name)
	assert.Equal(t, "dp/one", biggest.DiscoveryPath)
	assert.Equal(t, int64(3), biggest.Runs)
	assert.Equal(t, int64(3), biggest.TestStats)
	assert.Equal(t, int64(0), biggest.BlockLogs)
	assert.Equal(t, int64(300), biggest.LastRun)

	assert.Equal(t, "suite-small", stats.TopSuites[1].SuiteHash)
	assert.Equal(t, int64(1), stats.TopSuites[1].Runs)
}

// TestStore_DatabaseStats_Empty checks the report holds up with nothing in it,
// which is what a fresh deployment sees.
func TestStore_DatabaseStats_Empty(t *testing.T) {
	s := setupTestStore(t)

	stats, err := s.DatabaseStats(context.Background())
	require.NoError(t, err)

	assert.NotEmpty(t, stats.Tables)
	assert.Empty(t, stats.TopSuites)
	assert.Zero(t, stats.OldestRun)
	assert.Zero(t, stats.NewestRun)

	for _, table := range stats.Tables {
		assert.Zero(t, table.Rows, table.Name)
	}
}

// TestStore_DatabaseStats_FileBacked covers the SQLite file and volume
// figures, which only exist for a database on disk. The in-memory store used
// by the other tests deliberately reports none of them.
func TestStore_DatabaseStats_FileBacked(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	path := filepath.Join(t.TempDir(), "index.db")

	s := indexstore.NewStore(log, &config.APIDatabaseConfig{
		Driver: "sqlite",
		SQLite: config.SQLiteDatabaseConfig{Path: path},
	})
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop() })

	stats, err := s.DatabaseStats(context.Background())
	require.NoError(t, err)

	assert.Equal(t, path, stats.Path)
	assert.Positive(t, stats.FileBytes)
	assert.Positive(t, stats.PageSize)
	assert.Positive(t, stats.PageCount)
	assert.GreaterOrEqual(t, stats.FreePages, int64(0))
	assert.Positive(t, stats.VolumeTotalBytes)
	assert.Positive(t, stats.VolumeFreeBytes)
	assert.Less(t, stats.VolumeFreeBytes, stats.VolumeTotalBytes)
}

// TestStore_DatabaseStats_InMemoryHasNoFile guards the guard: an in-memory
// database must not report a path or invent a file size.
func TestStore_DatabaseStats_InMemoryHasNoFile(t *testing.T) {
	stats, err := setupTestStore(t).DatabaseStats(context.Background())
	require.NoError(t, err)

	assert.Empty(t, stats.Path)
	assert.Zero(t, stats.FileBytes)
	assert.Zero(t, stats.VolumeTotalBytes)
}
