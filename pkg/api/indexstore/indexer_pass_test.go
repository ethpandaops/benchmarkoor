package indexstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
)

// TestStore_RecordAndListIndexerPasses covers the round trip and the order:
// the newest pass comes back first, because that is the one the admin page
// puts a number on.
func TestStore_RecordAndListIndexerPasses(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	start := time.Now().UTC().Truncate(time.Second)

	for i := range 3 {
		pass := &indexstore.IndexerPass{
			StartedAt:      start.Add(time.Duration(i) * time.Minute),
			FinishedAt:     start.Add(time.Duration(i)*time.Minute + time.Second),
			DurationMs:     int64(1000 + i),
			Trigger:        indexstore.IndexerPassTriggerSchedule,
			Status:         indexstore.IndexerPassStatusCompleted,
			DiscoveryPaths: 2,
			StorageRuns:    100 + i,
			IndexedRuns:    90 + i,
			RunsIndexed:    i,
			RunsReindexed:  1,
			RunsFailed:     2,
		}
		require.NoError(t, s.RecordIndexerPass(ctx, pass))
	}

	passes, err := s.ListIndexerPasses(ctx, 10)
	require.NoError(t, err)
	require.Len(t, passes, 3)

	assert.Equal(t, int64(1002), passes[0].DurationMs, "newest first")
	assert.Equal(t, int64(1000), passes[2].DurationMs)

	newest := passes[0]
	assert.Equal(t, indexstore.IndexerPassTriggerSchedule, newest.Trigger)
	assert.Equal(t, indexstore.IndexerPassStatusCompleted, newest.Status)
	assert.Equal(t, 2, newest.DiscoveryPaths)
	assert.Equal(t, 102, newest.StorageRuns)
	assert.Equal(t, 92, newest.IndexedRuns)
	assert.Equal(t, 2, newest.RunsIndexed)
	assert.Equal(t, 1, newest.RunsReindexed)
	assert.Equal(t, 2, newest.RunsFailed)
	assert.Empty(t, newest.Error)

	// The limit bounds the page, still newest first.
	passes, err = s.ListIndexerPasses(ctx, 1)
	require.NoError(t, err)
	require.Len(t, passes, 1)
	assert.Equal(t, int64(1002), passes[0].DurationMs)
}

// TestStore_ListIndexerPassesEmpty covers a fresh deployment: no passes yet is
// an empty list, not an error.
func TestStore_ListIndexerPassesEmpty(t *testing.T) {
	s := setupTestStore(t)

	passes, err := s.ListIndexerPasses(context.Background(), 10)
	require.NoError(t, err)
	assert.Empty(t, passes)
}

// TestStore_PrunesIndexerPasses covers the retention limit. A deployment on a
// one-minute interval writes a row a minute, so the table has to bound itself.
func TestStore_PrunesIndexerPasses(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	// 520 passes against a retention limit of 500.
	const total = 520

	start := time.Now().UTC()

	for i := range total {
		require.NoError(t, s.RecordIndexerPass(ctx, &indexstore.IndexerPass{
			StartedAt:  start.Add(time.Duration(i) * time.Second),
			FinishedAt: start.Add(time.Duration(i) * time.Second),
			DurationMs: int64(i),
			Trigger:    indexstore.IndexerPassTriggerSchedule,
			Status:     indexstore.IndexerPassStatusCompleted,
		}))
	}

	// Asking for more than the limit returns only what survives.
	passes, err := s.ListIndexerPasses(ctx, total)
	require.NoError(t, err)
	assert.Len(t, passes, 500)

	assert.Equal(t, int64(total-1), passes[0].DurationMs, "newest kept")
	assert.Equal(
		t, int64(total-500), passes[499].DurationMs, "oldest survivor",
	)
}
