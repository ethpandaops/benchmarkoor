package indexstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
)

func TestStore_MarkRunForDeletion(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	for _, id := range []string{"run-a", "run-b"} {
		require.NoError(t, s.UpsertRun(ctx, &indexstore.Run{
			DiscoveryPath: "dp/del", RunID: id,
			Timestamp: time.Now().Unix(), Status: "completed",
		}))
	}

	// Nothing queued yet.
	queued, err := s.ListRunsPendingDeletion(ctx)
	require.NoError(t, err)
	assert.Empty(t, queued)

	// Marking bumps the generation so /index refreshes.
	before := s.RunsGeneration()
	require.NoError(t, s.MarkRunForDeletion(ctx, "run-a"))
	assert.Greater(t, s.RunsGeneration(), before)

	queued, err = s.ListRunsPendingDeletion(ctx)
	require.NoError(t, err)
	require.Len(t, queued, 1)
	assert.Equal(t, "run-a", queued[0].RunID)
	require.NotNil(t, queued[0].DeletionRequestedAt)

	// Marking again is idempotent and keeps the original timestamp.
	firstStamp := *queued[0].DeletionRequestedAt
	gen := s.RunsGeneration()
	require.NoError(t, s.MarkRunForDeletion(ctx, "run-a"))
	assert.Equal(t, gen, s.RunsGeneration())

	queued, err = s.ListRunsPendingDeletion(ctx)
	require.NoError(t, err)
	require.Len(t, queued, 1)
	assert.True(t, queued[0].DeletionRequestedAt.Equal(firstStamp))

	// Unknown runs surface a sentinel error.
	err = s.MarkRunForDeletion(ctx, "nope")
	require.ErrorIs(t, err, indexstore.ErrRunNotFound)

	// Running runs are refused and stay out of the queue.
	require.NoError(t, s.UpsertRun(ctx, &indexstore.Run{
		DiscoveryPath: "dp/del", RunID: "run-live",
		Timestamp: time.Now().Unix(), Status: indexstore.RunStatusRunning,
	}))
	require.ErrorIs(t, s.MarkRunForDeletion(ctx, "run-live"), indexstore.ErrRunInProgress)

	queued, err = s.ListRunsPendingDeletion(ctx)
	require.NoError(t, err)
	require.Len(t, queued, 1)
	assert.Equal(t, "run-a", queued[0].RunID)

	// The mark survives a re-index of the run.
	require.NoError(t, s.UpsertRun(ctx, &indexstore.Run{
		DiscoveryPath: "dp/del", RunID: "run-a",
		Timestamp: time.Now().Unix(), Status: "completed",
		TestsTotal: 5,
	}))

	got, err := s.GetRunByRunID(ctx, "run-a")
	require.NoError(t, err)
	require.NotNil(t, got.DeletionRequestedAt)
	assert.Equal(t, 5, got.TestsTotal)
}

func TestStore_ListRunsPendingDeletion_Order(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	ids := []string{"run-3", "run-1", "run-2"}
	for _, id := range ids {
		require.NoError(t, s.UpsertRun(ctx, &indexstore.Run{
			DiscoveryPath: "dp/order", RunID: id,
			Timestamp: time.Now().Unix(), Status: "completed",
		}))
	}

	// Queue in a different order than insertion and with distinct
	// timestamps so the queue order is unambiguous.
	for _, id := range []string{"run-2", "run-3", "run-1"} {
		require.NoError(t, s.MarkRunForDeletion(ctx, id))
		time.Sleep(2 * time.Millisecond)
	}

	queued, err := s.ListRunsPendingDeletion(ctx)
	require.NoError(t, err)
	require.Len(t, queued, 3)

	gotIDs := make([]string, 0, len(queued))
	for i := range queued {
		gotIDs = append(gotIDs, queued[i].RunID)
	}

	assert.Equal(t, []string{"run-2", "run-3", "run-1"}, gotIDs)

	// Deleting the head of the queue leaves the rest in order.
	require.NoError(t, s.DeleteRunCascade(ctx, "run-2"))

	queued, err = s.ListRunsPendingDeletion(ctx)
	require.NoError(t, err)
	require.Len(t, queued, 2)
	assert.Equal(t, "run-3", queued[0].RunID)
	assert.Equal(t, "run-1", queued[1].RunID)
}

func TestStore_SetRunDeletionError(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertRun(ctx, &indexstore.Run{
		DiscoveryPath: "dp/err", RunID: "run-err",
		Timestamp: time.Now().Unix(), Status: "completed",
	}))
	require.NoError(t, s.MarkRunForDeletion(ctx, "run-err"))

	before := s.RunsGeneration()
	require.NoError(t, s.SetRunDeletionError(ctx, "run-err", "boom"))
	assert.Greater(t, s.RunsGeneration(), before)

	queued, err := s.ListRunsPendingDeletion(ctx)
	require.NoError(t, err)
	require.Len(t, queued, 1)
	assert.Equal(t, "boom", queued[0].DeletionError)
}

func TestStore_ListIncompleteRunIDs_SkipsQueued(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	dp := "dp/incomplete-queued"

	// "pending" is incomplete (non-terminal) but not running, so it can
	// still be queued for deletion.
	for _, id := range []string{"r-keep", "r-queued"} {
		require.NoError(t, s.UpsertRun(ctx, &indexstore.Run{
			DiscoveryPath: dp, RunID: id,
			Status: "pending", HasResult: false,
		}))
	}

	require.NoError(t, s.MarkRunForDeletion(ctx, "r-queued"))

	ids, err := s.ListIncompleteRunIDs(ctx, dp)
	require.NoError(t, err)
	assert.Equal(t, []string{"r-keep"}, ids)
}

func TestStore_UnmarkRunForDeletion(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertRun(ctx, &indexstore.Run{
		DiscoveryPath: "dp/unmark", RunID: "run-u",
		Timestamp: time.Now().Unix(), Status: "completed",
	}))
	require.NoError(t, s.MarkRunForDeletion(ctx, "run-u"))
	require.NoError(t, s.SetRunDeletionError(ctx, "run-u", "boom"))

	before := s.RunsGeneration()
	require.NoError(t, s.UnmarkRunForDeletion(ctx, "run-u"))
	assert.Greater(t, s.RunsGeneration(), before)

	queued, err := s.ListRunsPendingDeletion(ctx)
	require.NoError(t, err)
	assert.Empty(t, queued)

	got, err := s.GetRunByRunID(ctx, "run-u")
	require.NoError(t, err)
	assert.Nil(t, got.DeletionRequestedAt)
	assert.Empty(t, got.DeletionError)

	// Not queued: no-op, no generation bump. Unknown: sentinel error.
	gen := s.RunsGeneration()
	require.NoError(t, s.UnmarkRunForDeletion(ctx, "run-u"))
	assert.Equal(t, gen, s.RunsGeneration())
	require.ErrorIs(t, s.UnmarkRunForDeletion(ctx, "nope"), indexstore.ErrRunNotFound)
}

// TestStore_MarkRunsForDeletionBySuite covers queueing a whole suite: every
// idle run goes, a running one is left alone, and other suites are untouched.
func TestStore_MarkRunsForDeletionBySuite(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	seed := func(runID, suiteHash, status string) {
		require.NoError(t, s.UpsertRun(ctx, &indexstore.Run{
			DiscoveryPath: "dp/suite", RunID: runID,
			SuiteHash: suiteHash, Timestamp: time.Now().Unix(),
			Status: status,
		}))
	}

	seed("run-a", "suite-1", "completed")
	seed("run-b", "suite-1", "failed")
	seed("run-c", "suite-1", indexstore.RunStatusRunning)
	seed("run-other", "suite-2", "completed")

	before := s.RunsGeneration()

	result, err := s.MarkRunsForDeletionBySuite(ctx, "suite-1")
	require.NoError(t, err)
	assert.Equal(t, int64(2), result.Queued)
	assert.Equal(t, int64(1), result.Skipped, "the running run is left alone")
	assert.Greater(t, s.RunsGeneration(), before, "/index must refresh")

	queued, err := s.ListRunsPendingDeletion(ctx)
	require.NoError(t, err)
	require.Len(t, queued, 2)

	for _, run := range queued {
		assert.Equal(t, "suite-1", run.SuiteHash)
		assert.NotEqual(t, "run-c", run.RunID)
	}

	// Repeating it queues nothing new, and still reports the running run.
	gen := s.RunsGeneration()

	result, err = s.MarkRunsForDeletionBySuite(ctx, "suite-1")
	require.NoError(t, err)
	assert.Zero(t, result.Queued)
	assert.Equal(t, int64(1), result.Skipped)
	assert.Equal(t, gen, s.RunsGeneration(), "a no-op must not bump the gen")

	// The other suite is untouched.
	other, err := s.GetRunByRunID(ctx, "run-other")
	require.NoError(t, err)
	assert.Nil(t, other.DeletionRequestedAt)

	// A suite with no runs is reported, not silently accepted.
	_, err = s.MarkRunsForDeletionBySuite(ctx, "suite-missing")
	assert.ErrorIs(t, err, indexstore.ErrRunNotFound)

	_, err = s.MarkRunsForDeletionBySuite(ctx, "")
	assert.ErrorIs(t, err, indexstore.ErrRunNotFound)
}
