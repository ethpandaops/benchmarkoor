package indexstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
)

func TestRunIDTimestamp(t *testing.T) {
	tests := []struct {
		name  string
		runID string
		want  int64
	}{
		{
			name:  "runner convention",
			runID: "1790344061_c0f24cc0_reth-bal-full",
			want:  1790344061,
		},
		{name: "timestamp only", runID: "1790344061_x", want: 1790344061},
		{name: "no separator", runID: "1790344061", want: 0},
		{name: "not a number", runID: "abc_def_ghi", want: 0},
		{name: "zero", runID: "0_abc_def", want: 0},
		{name: "negative", runID: "-5_abc_def", want: 0},
		{name: "empty", runID: "", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, indexstore.RunIDTimestamp(tt.runID))
		})
	}
}

// TestStore_RecordIndexFailure covers the upsert: a repeat failure bumps the
// attempt count and refreshes the error without losing when it first broke.
func TestStore_RecordIndexFailure(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	const (
		dp    = "dp/fail"
		runID = "1790344061_c0f24cc0_reth-bal-full"
	)

	require.NoError(t, s.RecordIndexFailure(
		ctx, dp, runID, "config.json not found",
	))

	failures, err := s.ListIndexFailures(ctx, 10, 0)
	require.NoError(t, err)
	require.Len(t, failures, 1)

	first := failures[0]
	assert.Equal(t, dp, first.DiscoveryPath)
	assert.Equal(t, runID, first.RunID)
	assert.Equal(t, int64(1790344061), first.RunTimestamp)
	assert.Equal(t, "config.json not found", first.LastError)
	assert.Equal(t, 1, first.Attempts)
	assert.False(t, first.FirstFailedAt.IsZero())

	// A second failure is the same run, not a new record.
	require.NoError(t, s.RecordIndexFailure(ctx, dp, runID, "still broken"))

	failures, err = s.ListIndexFailures(ctx, 10, 0)
	require.NoError(t, err)
	require.Len(t, failures, 1)

	second := failures[0]
	assert.Equal(t, 2, second.Attempts)
	assert.Equal(t, "still broken", second.LastError)
	assert.Equal(t,
		first.FirstFailedAt.Unix(), second.FirstFailedAt.Unix(),
		"first_failed_at must survive a repeat",
	)

	// A run that heals loses its record.
	require.NoError(t, s.ClearIndexFailure(ctx, dp, runID))

	failures, err = s.ListIndexFailures(ctx, 10, 0)
	require.NoError(t, err)
	assert.Empty(t, failures)

	// Clearing a record that is not there is a no-op, not an error.
	require.NoError(t, s.ClearIndexFailure(ctx, dp, runID))
}

// TestStore_ListIndexFailures covers the ordering and the paging the admin
// listing relies on.
func TestStore_ListIndexFailures(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	for _, runID := range []string{
		"100_a_geth", "300_c_besu", "200_b_reth", "malformed-run",
	} {
		require.NoError(t, s.RecordIndexFailure(
			ctx, "dp/fail", runID, "config.json not found",
		))
	}

	total, err := s.CountIndexFailures(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(4), total)

	// Newest run first; the ID with no timestamp sorts last.
	all, err := s.ListIndexFailures(ctx, 10, 0)
	require.NoError(t, err)
	require.Len(t, all, 4)
	assert.Equal(t, "300_c_besu", all[0].RunID)
	assert.Equal(t, "200_b_reth", all[1].RunID)
	assert.Equal(t, "100_a_geth", all[2].RunID)
	assert.Equal(t, "malformed-run", all[3].RunID)
	assert.Zero(t, all[3].RunTimestamp)

	page, err := s.ListIndexFailures(ctx, 2, 1)
	require.NoError(t, err)
	require.Len(t, page, 2)
	assert.Equal(t, "200_b_reth", page[0].RunID)
	assert.Equal(t, "100_a_geth", page[1].RunID)
}

// TestStore_ListIndexFailuresByPath checks the per-path read the indexer uses
// to build its skip set.
func TestStore_ListIndexFailuresByPath(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.RecordIndexFailure(ctx, "dp/one", "100_a_geth", "x"))
	require.NoError(t, s.RecordIndexFailure(ctx, "dp/two", "200_b_reth", "x"))

	one, err := s.ListIndexFailuresByPath(ctx, "dp/one")
	require.NoError(t, err)
	require.Len(t, one, 1)
	assert.Equal(t, "100_a_geth", one[0].RunID)
	assert.False(t, one[0].LastAttemptAt.IsZero())

	none, err := s.ListIndexFailuresByPath(ctx, "dp/missing")
	require.NoError(t, err)
	assert.Empty(t, none)
}

// TestStore_IndexFailureDeletionQueue covers queueing one record, queueing
// every record, and taking them back out.
func TestStore_IndexFailureDeletionQueue(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	for _, runID := range []string{"100_a_geth", "200_b_reth", "300_c_besu"} {
		require.NoError(t, s.RecordIndexFailure(ctx, "dp/fail", runID, "x"))
	}

	queued, err := s.ListIndexFailuresPendingDeletion(ctx)
	require.NoError(t, err)
	assert.Empty(t, queued)

	// An unknown run is reported, not silently accepted.
	assert.ErrorIs(t,
		s.MarkIndexFailureForDeletion(ctx, "nope"),
		indexstore.ErrIndexFailureNotFound,
	)

	require.NoError(t, s.MarkIndexFailureForDeletion(ctx, "100_a_geth"))

	queued, err = s.ListIndexFailuresPendingDeletion(ctx)
	require.NoError(t, err)
	require.Len(t, queued, 1)
	require.NotNil(t, queued[0].DeletionRequestedAt)

	// Marking again keeps the original queue position.
	stamp := *queued[0].DeletionRequestedAt
	require.NoError(t, s.MarkIndexFailureForDeletion(ctx, "100_a_geth"))

	queued, err = s.ListIndexFailuresPendingDeletion(ctx)
	require.NoError(t, err)
	require.Len(t, queued, 1)
	assert.Equal(t, stamp.Unix(), queued[0].DeletionRequestedAt.Unix())

	// A failed attempt is recorded and the record stays queued.
	require.NoError(t, s.SetIndexFailureDeletionError(
		ctx, "dp/fail", "100_a_geth", "storage delete failed: boom",
	))

	queued, err = s.ListIndexFailuresPendingDeletion(ctx)
	require.NoError(t, err)
	require.Len(t, queued, 1)
	assert.Equal(t, "storage delete failed: boom", queued[0].DeletionError)

	// Queueing the rest only touches what is not queued yet.
	added, err := s.MarkAllIndexFailuresForDeletion(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), added)

	queued, err = s.ListIndexFailuresPendingDeletion(ctx)
	require.NoError(t, err)
	assert.Len(t, queued, 3)

	// Cancelling one clears its recorded error too.
	require.NoError(t, s.UnmarkIndexFailureForDeletion(ctx, "100_a_geth"))

	remaining, err := s.ListIndexFailures(ctx, 10, 0)
	require.NoError(t, err)

	for _, failure := range remaining {
		if failure.RunID == "100_a_geth" {
			assert.Nil(t, failure.DeletionRequestedAt)
			assert.Empty(t, failure.DeletionError)
		}
	}

	cancelled, err := s.UnmarkAllIndexFailuresForDeletion(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), cancelled)

	queued, err = s.ListIndexFailuresPendingDeletion(ctx)
	require.NoError(t, err)
	assert.Empty(t, queued)
}

// TestStore_IndexFailureQueueOrder checks the queue drains oldest request
// first, which is what makes a bulk delete predictable.
func TestStore_IndexFailureQueueOrder(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	for _, runID := range []string{"100_a_geth", "200_b_reth"} {
		require.NoError(t, s.RecordIndexFailure(ctx, "dp/fail", runID, "x"))
		require.NoError(t, s.MarkIndexFailureForDeletion(ctx, runID))

		// The stamps have second resolution in SQLite, so separate them.
		time.Sleep(1100 * time.Millisecond)
	}

	queued, err := s.ListIndexFailuresPendingDeletion(ctx)
	require.NoError(t, err)
	require.Len(t, queued, 2)
	assert.Equal(t, "100_a_geth", queued[0].RunID)
	assert.Equal(t, "200_b_reth", queued[1].RunID)
}
