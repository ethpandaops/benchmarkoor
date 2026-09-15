package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
)

// fakeDeleter records the run IDs it was asked to delete, in order, and
// fails the runs listed in failing.
type fakeDeleter struct {
	mu      sync.Mutex
	deleted []string
	failing map[string]bool
}

func (f *fakeDeleter) DeleteRun(_ context.Context, _, runID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failing[runID] {
		return errors.New("storage unavailable")
	}

	f.deleted = append(f.deleted, runID)

	return nil
}

func (f *fakeDeleter) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.deleted...)
}

func seedRun(t *testing.T, s *server, runID string) {
	t.Helper()

	require.NoError(t, s.indexStore.UpsertRun(context.Background(), &indexstore.Run{
		DiscoveryPath: "dp/test",
		RunID:         runID,
		Timestamp:     time.Now().Unix(),
		Status:        "completed",
		SuiteHash:     "suite-" + runID,
	}))
}

func postDeleteRuns(t *testing.T, s *server, ids []string) (int, deleteRunsResponse) {
	t.Helper()

	body, err := json.Marshal(deleteRunsRequest{RunIDs: ids})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost, "/api/v1/admin/runs/delete", bytes.NewReader(body),
	)
	s.handleDeleteRuns(rec, req)

	var resp deleteRunsResponse
	if rec.Code == http.StatusAccepted {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	}

	return rec.Code, resp
}

func TestHandleDeleteRuns_QueuesAndReportsMissing(t *testing.T) {
	s := newIndexTestServer(t)
	s.storageDeleter = &fakeDeleter{}
	s.runDeleterKick = make(chan struct{}, 1)

	seedRun(t, s, "run-1")
	seedRun(t, s, "run-2")

	code, resp := postDeleteRuns(t, s, []string{"run-1", "missing", "run-2"})
	require.Equal(t, http.StatusAccepted, code)
	assert.Equal(t, "ok", resp.Status)
	assert.Equal(t, 2, resp.Queued)
	assert.Equal(t, []string{"missing: not found in index"}, resp.Errors)

	// The deleter was woken up but nothing is deleted until it runs.
	select {
	case <-s.runDeleterKick:
	default:
		t.Fatal("expected the run deleter to be kicked")
	}

	queued, err := s.indexStore.ListRunsPendingDeletion(context.Background())
	require.NoError(t, err)
	require.Len(t, queued, 2)
	assert.Equal(t, "run-1", queued[0].RunID)
	assert.Equal(t, "run-2", queued[1].RunID)

	// /index exposes the mark.
	_, idx := getIndex(t, s)
	require.Len(t, idx.Entries, 2)

	for _, e := range idx.Entries {
		assert.NotZero(t, e.DeletionRequestedAt, e.RunID)
	}

	// The queue endpoint lists them in order.
	rec := httptest.NewRecorder()
	s.handleDeletionQueue(rec, httptest.NewRequest(
		http.MethodGet, "/api/v1/admin/runs/deletion-queue", nil,
	))
	require.Equal(t, http.StatusOK, rec.Code)

	var entries []deletionQueueEntry
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &entries))
	require.Len(t, entries, 2)
	assert.Equal(t, "run-1", entries[0].RunID)
	assert.Equal(t, "dp/test", entries[0].DiscoveryPath)
	assert.NotEmpty(t, entries[0].RequestedAt)
}

func TestHandleCancelDeleteRuns_RemovesFromQueue(t *testing.T) {
	s := newIndexTestServer(t)
	del := &fakeDeleter{}
	s.storageDeleter = del

	ctx := context.Background()

	seedRun(t, s, "run-1")
	seedRun(t, s, "run-2")
	require.NoError(t, s.indexStore.MarkRunForDeletion(ctx, "run-1"))
	require.NoError(t, s.indexStore.MarkRunForDeletion(ctx, "run-2"))

	body, err := json.Marshal(deleteRunsRequest{RunIDs: []string{"run-1", "missing"}})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	s.handleCancelDeleteRuns(rec, httptest.NewRequest(
		http.MethodPost, "/api/v1/admin/runs/delete/cancel", bytes.NewReader(body),
	))
	require.Equal(t, http.StatusOK, rec.Code)

	var resp cancelDeleteRunsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Cancelled)
	assert.Equal(t, []string{"missing: not found in index"}, resp.Errors)

	// Only run-2 is still queued and only run-2 gets deleted.
	s.drainDeletionQueue(ctx)
	assert.Equal(t, []string{"run-2"}, del.calls())

	runs, err := s.indexStore.ListAllRuns(ctx)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "run-1", runs[0].RunID)
	assert.Nil(t, runs[0].DeletionRequestedAt)
}

func TestHandleDeleteRuns_RejectsWithoutDeleter(t *testing.T) {
	s := newIndexTestServer(t)
	seedRun(t, s, "run-1")

	code, _ := postDeleteRuns(t, s, []string{"run-1"})
	assert.Equal(t, http.StatusServiceUnavailable, code)

	s.storageDeleter = &fakeDeleter{}

	code, _ = postDeleteRuns(t, s, nil)
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestDrainDeletionQueue_DeletesInOrderAndKeepsFailures(t *testing.T) {
	s := newIndexTestServer(t)
	del := &fakeDeleter{failing: map[string]bool{"run-2": true}}
	s.storageDeleter = del

	ctx := context.Background()

	for _, id := range []string{"run-1", "run-2", "run-3"} {
		seedRun(t, s, id)
		require.NoError(t, s.indexStore.MarkRunForDeletion(ctx, id))
		time.Sleep(2 * time.Millisecond)
	}

	s.drainDeletionQueue(ctx)

	// Storage was asked in queue order; the failing run was skipped.
	assert.Equal(t, []string{"run-1", "run-3"}, del.calls())

	// Deleted runs are gone from the index, the failed one stays queued
	// with its error recorded.
	runs, err := s.indexStore.ListAllRuns(ctx)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "run-2", runs[0].RunID)
	assert.Contains(t, runs[0].DeletionError, "storage delete failed")
	require.NotNil(t, runs[0].DeletionRequestedAt)

	// Once storage recovers the next pass finishes the job.
	del.failing = nil
	s.drainDeletionQueue(ctx)

	runs, err = s.indexStore.ListAllRuns(ctx)
	require.NoError(t, err)
	assert.Empty(t, runs)
	assert.Equal(t, []string{"run-1", "run-3", "run-2"}, del.calls())
}

func TestDrainDeletionQueue_StopsOnCancel(t *testing.T) {
	s := newIndexTestServer(t)
	del := &fakeDeleter{}
	s.storageDeleter = del

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	seedRun(t, s, "run-1")
	require.NoError(t, s.indexStore.MarkRunForDeletion(context.Background(), "run-1"))

	s.drainDeletionQueue(ctx)

	assert.Empty(t, del.calls())

	runs, err := s.indexStore.ListAllRuns(context.Background())
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Empty(t, runs[0].DeletionError)
}

func TestStartRunDeleter_DrainsOnKickAndStops(t *testing.T) {
	s := newIndexTestServer(t)
	del := &fakeDeleter{}
	s.storageDeleter = del
	s.runDeleterKick = make(chan struct{}, 1)
	s.done = make(chan struct{})

	ctx := context.Background()

	// Queued before start: drained by the first pass.
	seedRun(t, s, "run-1")
	require.NoError(t, s.indexStore.MarkRunForDeletion(ctx, "run-1"))

	s.startRunDeleter(ctx)

	require.Eventually(t, func() bool {
		return len(del.calls()) == 1
	}, 5*time.Second, 10*time.Millisecond)

	// Queued after start: drained on kick.
	seedRun(t, s, "run-2")
	require.NoError(t, s.indexStore.MarkRunForDeletion(ctx, "run-2"))
	s.kickRunDeleter()

	require.Eventually(t, func() bool {
		return len(del.calls()) == 2
	}, 5*time.Second, 10*time.Millisecond)

	assert.Equal(t, []string{"run-1", "run-2"}, del.calls())

	close(s.done)
	s.wg.Wait()
}
