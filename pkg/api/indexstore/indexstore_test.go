package indexstore_test

import (
	"context"
	"encoding/json"
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
			name: "failed without result is terminal - excluded",
			run: indexstore.Run{
				DiscoveryPath: dp, RunID: "r-failed",
				Status: "failed", HasResult: false,
			},
			wantInList: false,
		},
		{
			name: "empty status without result is abandoned - excluded",
			run: indexstore.Run{
				DiscoveryPath: dp, RunID: "r-empty-status",
				Status: "", HasResult: false,
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

func TestStore_TestStatCRUD(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	suiteHash := "suite-abc123"
	runID1 := "run-td-1"
	runID2 := "run-td-2"

	// Upsert several test stats across two runs.
	stats := []indexstore.TestStat{
		{
			SuiteHash: suiteHash, TestName: "TestA",
			RunID: runID1, Client: "geth",
			TotalGasUsed: 21000, TotalTimeNs: 500000,
		},
		{
			SuiteHash: suiteHash, TestName: "TestB",
			RunID: runID1, Client: "geth",
			TotalGasUsed: 42000, TotalTimeNs: 750000,
		},
		{
			SuiteHash: suiteHash, TestName: "TestA",
			RunID: runID2, Client: "reth",
			TotalGasUsed: 21000, TotalTimeNs: 400000,
		},
	}

	for i := range stats {
		require.NoError(t, s.UpsertTestStat(ctx, &stats[i]))
	}

	// List by suite hash returns all three.
	listed, err := s.ListTestStatsBySuite(ctx, suiteHash)
	require.NoError(t, err)
	assert.Len(t, listed, 3)

	// Upsert the same composite key again; must not create a duplicate.
	updatedStat := &indexstore.TestStat{
		SuiteHash: suiteHash, TestName: "TestA",
		RunID: runID1, Client: "geth",
		TotalGasUsed: 63000, TotalTimeNs: 600000,
	}
	require.NoError(t, s.UpsertTestStat(ctx, updatedStat))

	listed, err = s.ListTestStatsBySuite(ctx, suiteHash)
	require.NoError(t, err)
	assert.Len(t, listed, 3, "upsert must not duplicate the row")

	// Delete test stats for runID1.
	require.NoError(t, s.DeleteTestStatsForRun(ctx, runID1))

	// Only runID2 entries remain.
	remaining, err := s.ListTestStatsBySuite(ctx, suiteHash)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	assert.Equal(t, runID2, remaining[0].RunID)
	assert.Equal(t, "TestA", remaining[0].TestName)
}

func TestStore_UpsertSuiteUpdatesName(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	original := &indexstore.Suite{
		SuiteHash:     "hash-abc",
		DiscoveryPath: "dp/test",
		Name:          "old-name",
		TestsTotal:    5,
		IndexedAt:     time.Now().UTC(),
	}
	require.NoError(t, s.UpsertSuite(ctx, original))

	// Upsert the same hash with a different name.
	updated := &indexstore.Suite{
		SuiteHash:     "hash-abc",
		DiscoveryPath: "dp/test",
		Name:          "new-name",
		TestsTotal:    5,
		IndexedAt:     time.Now().UTC(),
	}
	require.NoError(t, s.UpsertSuite(ctx, updated))

	// The struct should reflect the updated name.
	assert.Equal(t, "new-name", updated.Name)
	assert.NotZero(t, updated.ID, "ID should be populated after upsert")
	assert.Equal(t, original.ID, updated.ID, "upsert must not create a duplicate")
}

func TestStore_UpsertLiveRun_ConfigRoundtrip(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	rawCfg := `{"timestamp":1000,"instance":{"client":"geth"},"system":{"hostname":"h"}}`

	require.NoError(t, s.UpsertLiveRun(ctx, &indexstore.LiveRunReport{
		DiscoveryPath: "dp",
		RunID:         "run-cfg",
		Timestamp:     1000,
		Status:        "running",
		Config:        []byte(rawCfg),
	}))

	live, err := s.ListLiveRuns(ctx)
	require.NoError(t, err)
	require.Len(t, live, 1)
	assert.Equal(t, rawCfg, live[0].ConfigJSON, "ConfigJSON must round-trip exactly")

	// Second report without Config must preserve the previously stored value.
	require.NoError(t, s.UpsertLiveRun(ctx, &indexstore.LiveRunReport{
		DiscoveryPath: "dp",
		RunID:         "run-cfg",
		Status:        "running",
		TestsPassed:   5,
	}))
	live, err = s.ListLiveRuns(ctx)
	require.NoError(t, err)
	require.Len(t, live, 1)
	assert.Equal(t, rawCfg, live[0].ConfigJSON, "config_json must not be wiped by a report without Config")
}

func TestStore_UpsertLiveRun_InsertAndUpdate(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	report := &indexstore.LiveRunReport{
		DiscoveryPath: "dp/live",
		RunID:         "run-1",
		Timestamp:     1000,
		Status:        "running",
		Client:        "geth",
		Image:         "ethereum/client-go:latest",
		TestsTotal:    10,
		TestsPassed:   3,
		Metadata:      map[string]string{"env": "ci"},
	}
	require.NoError(t, s.UpsertLiveRun(ctx, report))

	live, err := s.ListLiveRuns(ctx)
	require.NoError(t, err)
	require.Len(t, live, 1)
	assert.Equal(t, "running", live[0].Status)
	assert.Equal(t, 3, live[0].TestsPassed)
	assert.Contains(t, live[0].MetadataJSON, `"env":"ci"`)
	first := live[0]

	// Update with later report.
	report.TestsPassed = 7
	report.TestsFailed = 1
	require.NoError(t, s.UpsertLiveRun(ctx, report))

	live, err = s.ListLiveRuns(ctx)
	require.NoError(t, err)
	require.Len(t, live, 1, "second report must not create a duplicate row")
	assert.Equal(t, first.ID, live[0].ID)
	assert.Equal(t, 7, live[0].TestsPassed)
	assert.Equal(t, 1, live[0].TestsFailed)
}

func TestStore_UpsertLiveRun_GasAndTestsRoundTrip(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	// First report carries running gas totals plus a per-test gas map
	// (one passed, one failed). All of it lives on the live_runs row;
	// the canonical Run / TestStat tables stay untouched.
	require.NoError(t, s.UpsertLiveRun(ctx, &indexstore.LiveRunReport{
		DiscoveryPath:          "dp/live",
		RunID:                  "run-1",
		Timestamp:              1000,
		Status:                 "running",
		TotalGasUsed:           1_500_000_000,
		TotalGasUsedDurationNs: 500_000_000,
		Tests: map[string]indexstore.LiveTestStats{
			"alpha": {Passed: true, GasUsed: 1_000, GasUsedDurationNs: 500_000},
			"beta":  {Passed: false},
		},
	}))

	live, err := s.ListLiveRuns(ctx)
	require.NoError(t, err)
	require.Len(t, live, 1)
	got := live[0]
	assert.Equal(t, int64(1_500_000_000), got.TotalGasUsed)
	assert.Equal(t, int64(500_000_000), got.TotalGasUsedDurationNs)
	require.NotEmpty(t, got.TestsJSON, "tests_json should round-trip")

	var decoded map[string]indexstore.LiveTestStats
	require.NoError(t, json.Unmarshal([]byte(got.TestsJSON), &decoded))
	require.Len(t, decoded, 2)
	assert.True(t, decoded["alpha"].Passed)
	assert.Equal(t, int64(1_000), decoded["alpha"].GasUsed)
	assert.False(t, decoded["beta"].Passed)

	// A nil Tests map means "no update" — the previously-stored map
	// stays put rather than being wiped to "{}".
	require.NoError(t, s.UpsertLiveRun(ctx, &indexstore.LiveRunReport{
		DiscoveryPath:          "dp/live",
		RunID:                  "run-1",
		Timestamp:              1000,
		Status:                 "running",
		TotalGasUsed:           3_000_000_000,
		TotalGasUsedDurationNs: 1_000_000_000,
	}))

	live, err = s.ListLiveRuns(ctx)
	require.NoError(t, err)
	require.Len(t, live, 1)
	got = live[0]
	assert.Equal(t, int64(3_000_000_000), got.TotalGasUsed, "gas totals should update")
	assert.NotEmpty(t, got.TestsJSON, "nil Tests should not wipe stored value")
}

func TestStore_LiveRun_DoesNotInterfereWithRun(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	// A canonical Run that the on-disk indexer wrote.
	run := &indexstore.Run{
		DiscoveryPath: "dp/x",
		RunID:         "shared-id",
		Timestamp:     1000,
		TimestampEnd:  2000,
		Status:        "completed",
		Client:        "geth",
		HasResult:     true,
		TestsTotal:    10,
		TestsPassed:   10,
		StepsJSON:     `{"setup":{"count":10}}`,
		IndexedAt:     time.Now().UTC(),
	}
	require.NoError(t, s.UpsertRun(ctx, run))

	// A live report claiming the same run is still running.
	require.NoError(t, s.UpsertLiveRun(ctx, &indexstore.LiveRunReport{
		DiscoveryPath: "dp/x",
		RunID:         "shared-id",
		Status:        "running",
		TestsPassed:   3,
	}))

	// The Run table entry must be untouched.
	got, err := s.GetRunByRunID(ctx, "shared-id")
	require.NoError(t, err)
	assert.Equal(t, "completed", got.Status)
	assert.Equal(t, 10, got.TestsPassed)
	assert.Equal(t, `{"setup":{"count":10}}`, got.StepsJSON)
}

func TestStore_DeleteLiveRun(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertLiveRun(ctx, &indexstore.LiveRunReport{
		DiscoveryPath: "dp/x",
		RunID:         "doomed",
		Status:        "running",
	}))

	live, err := s.ListLiveRuns(ctx)
	require.NoError(t, err)
	require.Len(t, live, 1)

	require.NoError(t, s.DeleteLiveRun(ctx, "dp/x", "doomed"))

	live, err = s.ListLiveRuns(ctx)
	require.NoError(t, err)
	assert.Empty(t, live)
}

func TestStore_DeleteStaleLiveRuns(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC()

	require.NoError(t, s.UpsertLiveRun(ctx, &indexstore.LiveRunReport{
		DiscoveryPath: "dp/x",
		RunID:         "fresh-1",
		Status:        "running",
	}))
	require.NoError(t, s.UpsertLiveRun(ctx, &indexstore.LiveRunReport{
		DiscoveryPath: "dp/x",
		RunID:         "fresh-2",
		Status:        "running",
	}))

	// A threshold in the past leaves freshly-reported rows alone.
	count, err := s.DeleteStaleLiveRuns(ctx, now.Add(-time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)

	live, err := s.ListLiveRuns(ctx)
	require.NoError(t, err)
	assert.Len(t, live, 2)

	// A threshold in the future deletes everything.
	count, err = s.DeleteStaleLiveRuns(ctx, now.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	live, err = s.ListLiveRuns(ctx)
	require.NoError(t, err)
	assert.Empty(t, live)
}
