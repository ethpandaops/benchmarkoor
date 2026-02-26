package indexstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

func setupTestStore(t *testing.T) indexstore.Store {
	t.Helper()

	cfg := &config.APIDatabaseConfig{
		Driver: "sqlite",
		SQLite: config.SQLiteDatabaseConfig{Path: ":memory:"},
	}

	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	s := indexstore.NewStore(log, cfg)
	require.NoError(t, s.Start(context.Background()))

	t.Cleanup(func() { _ = s.Stop() })

	return s
}

func TestStore_UpsertAndListRuns(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	now := time.Now().Unix()

	runA := &indexstore.Run{
		DiscoveryPath: "path/alpha",
		RunID:         "run-1",
		Timestamp:     now,
		Status:        "completed",
		Client:        "geth",
		HasResult:     true,
	}
	runB := &indexstore.Run{
		DiscoveryPath: "path/beta",
		RunID:         "run-2",
		Timestamp:     now + 1,
		Status:        "running",
		Client:        "reth",
		HasResult:     false,
	}

	require.NoError(t, s.UpsertRun(ctx, runA))
	require.NoError(t, s.UpsertRun(ctx, runB))

	// ListRuns filters by discovery path.
	alphaRuns, err := s.ListRuns(ctx, "path/alpha")
	require.NoError(t, err)
	require.Len(t, alphaRuns, 1)
	assert.Equal(t, "run-1", alphaRuns[0].RunID)
	assert.Equal(t, "geth", alphaRuns[0].Client)

	betaRuns, err := s.ListRuns(ctx, "path/beta")
	require.NoError(t, err)
	require.Len(t, betaRuns, 1)
	assert.Equal(t, "run-2", betaRuns[0].RunID)

	// ListAllRuns returns both.
	allRuns, err := s.ListAllRuns(ctx)
	require.NoError(t, err)
	assert.Len(t, allRuns, 2)
}

func TestStore_UpsertRunIdempotent(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	run := &indexstore.Run{
		DiscoveryPath: "dp/test",
		RunID:         "run-idem",
		Status:        "running",
		Client:        "besu",
		HasResult:     true,
		TestsTotal:    5,
		TestsPassed:   3,
		TestsFailed:   2,
	}

	require.NoError(t, s.UpsertRun(ctx, run))

	// Upsert the same composite key again; the call must succeed
	// and must not create a duplicate row.
	duplicate := &indexstore.Run{
		DiscoveryPath: "dp/test",
		RunID:         "run-idem",
		Status:        "completed",
		Client:        "besu",
		HasResult:     true,
		TestsTotal:    10,
		TestsPassed:   8,
		TestsFailed:   2,
	}
	require.NoError(t, s.UpsertRun(ctx, duplicate))

	runs, err := s.ListRuns(ctx, "dp/test")
	require.NoError(t, err)
	require.Len(t, runs, 1, "upsert must not duplicate the row")

	// The original values are preserved (first-write-wins with the
	// current Assign+FirstOrCreate implementation).
	assert.Equal(t, "running", runs[0].Status)
	assert.Equal(t, 5, runs[0].TestsTotal)
}

func TestStore_ListRunIDs(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	runs := []indexstore.Run{
		{DiscoveryPath: "dp/ids", RunID: "aaa", Status: "completed"},
		{DiscoveryPath: "dp/ids", RunID: "bbb", Status: "running"},
		{DiscoveryPath: "dp/other", RunID: "ccc", Status: "completed"},
	}
	for i := range runs {
		require.NoError(t, s.UpsertRun(ctx, &runs[i]))
	}

	ids, err := s.ListRunIDs(ctx, "dp/ids")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"aaa", "bbb"}, ids)

	// Ensure the other discovery path is not included.
	otherIDs, err := s.ListRunIDs(ctx, "dp/other")
	require.NoError(t, err)
	assert.Equal(t, []string{"ccc"}, otherIDs)
}

func TestStore_ListIncompleteRunIDs(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	dp := "dp/incomplete"

	tests := []struct {
		name       string
		run        indexstore.Run
		wantInList bool
	}{
		{
			name: "running without result is incomplete",
			run: indexstore.Run{
				DiscoveryPath: dp, RunID: "r-running",
				Status: "running", HasResult: false,
			},
			wantInList: true,
		},
		{
			name: "pending without result is incomplete",
			run: indexstore.Run{
				DiscoveryPath: dp, RunID: "r-pending",
				Status: "pending", HasResult: false,
			},
			wantInList: true,
		},
		{
			name: "completed without result is terminal - excluded",
			run: indexstore.Run{
				DiscoveryPath: dp, RunID: "r-completed-noresult",
				Status: "completed", HasResult: false,
			},
			wantInList: false,
		},
		{
			name: "cancelled without result is terminal - excluded",
			run: indexstore.Run{
				DiscoveryPath: dp, RunID: "r-cancelled",
				Status: "cancelled", HasResult: false,
			},
			wantInList: false,
		},
		{
			name: "container_died without result is terminal - excluded",
			run: indexstore.Run{
				DiscoveryPath: dp, RunID: "r-died",
				Status: "container_died", HasResult: false,
			},
			wantInList: false,
		},
		{
			name: "running with result already indexed - excluded",
			run: indexstore.Run{
				DiscoveryPath: dp, RunID: "r-running-hasresult",
				Status: "running", HasResult: true,
			},
			wantInList: false,
		},
	}

	wantIDs := make([]string, 0, len(tests))

	for _, tt := range tests {
		run := tt.run
		require.NoError(t, s.UpsertRun(ctx, &run), tt.name)

		if tt.wantInList {
			wantIDs = append(wantIDs, tt.run.RunID)
		}
	}

	ids, err := s.ListIncompleteRunIDs(ctx, dp)
	require.NoError(t, err)
	assert.ElementsMatch(t, wantIDs, ids)
}

func TestStore_TestDurationCRUD(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	suiteHash := "suite-abc123"
	runID1 := "run-td-1"
	runID2 := "run-td-2"

	// Upsert several test durations across two runs.
	durations := []indexstore.TestDuration{
		{
			SuiteHash: suiteHash, TestName: "TestA",
			RunID: runID1, Client: "geth",
			GasUsed: 21000, TimeNs: 500000,
		},
		{
			SuiteHash: suiteHash, TestName: "TestB",
			RunID: runID1, Client: "geth",
			GasUsed: 42000, TimeNs: 750000,
		},
		{
			SuiteHash: suiteHash, TestName: "TestA",
			RunID: runID2, Client: "reth",
			GasUsed: 21000, TimeNs: 400000,
		},
	}

	for i := range durations {
		require.NoError(t, s.UpsertTestDuration(ctx, &durations[i]))
	}

	// List by suite hash returns all three.
	listed, err := s.ListTestDurationsBySuite(ctx, suiteHash)
	require.NoError(t, err)
	assert.Len(t, listed, 3)

	// Upsert the same composite key again; must not create a duplicate.
	updatedDuration := &indexstore.TestDuration{
		SuiteHash: suiteHash, TestName: "TestA",
		RunID: runID1, Client: "geth",
		GasUsed: 63000, TimeNs: 600000,
	}
	require.NoError(t, s.UpsertTestDuration(ctx, updatedDuration))

	listed, err = s.ListTestDurationsBySuite(ctx, suiteHash)
	require.NoError(t, err)
	assert.Len(t, listed, 3, "upsert must not duplicate the row")

	// Delete test durations for runID1.
	require.NoError(t, s.DeleteTestDurationsForRun(ctx, runID1))

	// Only runID2 entries remain.
	remaining, err := s.ListTestDurationsBySuite(ctx, suiteHash)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	assert.Equal(t, runID2, remaining[0].RunID)
	assert.Equal(t, "TestA", remaining[0].TestName)
}
