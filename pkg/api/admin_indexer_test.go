package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexer"
	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
)

const testFailureDiscoveryPath = "dp/test"

func seedIndexFailure(t *testing.T, s *server, runID, msg string) {
	t.Helper()

	require.NoError(t, s.indexStore.RecordIndexFailure(
		context.Background(), testFailureDiscoveryPath, runID, msg,
	))
}

func getIndexerFailures(
	t *testing.T, s *server, query string,
) indexerFailuresResponse {
	t.Helper()

	rec := httptest.NewRecorder()
	s.handleIndexerFailures(rec, httptest.NewRequest(
		http.MethodGet, "/api/v1/admin/indexer/failures"+query, nil,
	))
	require.Equal(t, http.StatusOK, rec.Code)

	var resp indexerFailuresResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	return resp
}

func postIndexerFailures(
	t *testing.T,
	s *server,
	handler func(http.ResponseWriter, *http.Request),
	body deleteIndexerFailuresRequest,
) *httptest.ResponseRecorder {
	t.Helper()

	raw, err := json.Marshal(body)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(
		http.MethodPost, "/api/v1/admin/indexer/failures/delete",
		bytes.NewReader(raw),
	))

	return rec
}

// TestHandleIndexerFailures_ListsAndPages covers the admin listing: newest run
// first, the total alongside the page, and the page-size clamp.
func TestHandleIndexerFailures_ListsAndPages(t *testing.T) {
	s := newIndexTestServer(t)

	empty := getIndexerFailures(t, s, "")
	assert.Zero(t, empty.Total)
	assert.Empty(t, empty.Entries)
	assert.Equal(t, defaultIndexerFailureLimit, empty.Limit)

	for _, runID := range []string{"100_a_geth", "300_c_besu", "200_b_reth"} {
		seedIndexFailure(t, s, runID, "config.json not found")
	}

	all := getIndexerFailures(t, s, "")
	require.Equal(t, int64(3), all.Total)
	require.Len(t, all.Entries, 3)

	first := all.Entries[0]
	assert.Equal(t, "300_c_besu", first.RunID)
	assert.Equal(t, testFailureDiscoveryPath, first.DiscoveryPath)
	assert.Equal(t, int64(300), first.RunTimestamp)
	assert.Equal(t, "config.json not found", first.Error)
	assert.Equal(t, 1, first.Attempts)
	assert.NotEmpty(t, first.FirstFailedAt)
	assert.NotEmpty(t, first.LastAttemptAt)
	assert.Empty(t, first.DeletionRequestedAt)

	page := getIndexerFailures(t, s, "?limit=1&offset=1")
	assert.Equal(t, int64(3), page.Total)
	assert.Equal(t, 1, page.Limit)
	assert.Equal(t, 1, page.Offset)
	require.Len(t, page.Entries, 1)
	assert.Equal(t, "200_b_reth", page.Entries[0].RunID)

	// An oversized or nonsense page size is clamped, not honoured.
	assert.Equal(t,
		maxIndexerFailureLimit,
		getIndexerFailures(t, s, "?limit=99999").Limit,
	)
	assert.Equal(t, 1, getIndexerFailures(t, s, "?limit=0").Limit)
	assert.Equal(t,
		defaultIndexerFailureLimit,
		getIndexerFailures(t, s, "?limit=abc").Limit,
	)
	assert.Equal(t, 0, getIndexerFailures(t, s, "?offset=-5").Offset)
}

// TestHandleDeleteIndexerFailures_QueuesSelected covers queueing a chosen set
// and the reporting of run IDs that are not recorded failures.
func TestHandleDeleteIndexerFailures_QueuesSelected(t *testing.T) {
	s := newIndexTestServer(t)
	s.storageDeleter = &fakeDeleter{}
	s.runDeleterKick = make(chan struct{}, 1)

	seedIndexFailure(t, s, "100_a_geth", "config.json not found")
	seedIndexFailure(t, s, "200_b_reth", "config.json not found")

	rec := postIndexerFailures(t, s, s.handleDeleteIndexerFailures,
		deleteIndexerFailuresRequest{
			RunIDs: []string{"100_a_geth", "missing"},
		})
	require.Equal(t, http.StatusAccepted, rec.Code)

	var resp deleteIndexerFailuresResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp.Status)
	assert.Equal(t, int64(1), resp.Queued)
	assert.Equal(t, []string{"missing: not a recorded failure"}, resp.Errors)

	// The deleter is woken, but nothing is removed until it runs.
	select {
	case <-s.runDeleterKick:
	default:
		t.Fatal("expected the run deleter to be kicked")
	}

	assert.Empty(t, s.storageDeleter.(*fakeDeleter).calls())

	queued, err := s.indexStore.ListIndexFailuresPendingDeletion(
		context.Background(),
	)
	require.NoError(t, err)
	require.Len(t, queued, 1)
	assert.Equal(t, "100_a_geth", queued[0].RunID)

	// The listing shows the queued state.
	listed := getIndexerFailures(t, s, "")
	require.Len(t, listed.Entries, 2)

	for _, entry := range listed.Entries {
		if entry.RunID == "100_a_geth" {
			assert.NotEmpty(t, entry.DeletionRequestedAt)

			continue
		}

		assert.Empty(t, entry.DeletionRequestedAt)
	}
}

// TestHandleDeleteIndexerFailures_QueuesAll covers the bulk path, which is the
// point of the feature on a deployment holding thousands of records.
func TestHandleDeleteIndexerFailures_QueuesAll(t *testing.T) {
	s := newIndexTestServer(t)
	s.storageDeleter = &fakeDeleter{}
	s.runDeleterKick = make(chan struct{}, 1)

	for _, runID := range []string{"100_a_geth", "200_b_reth", "300_c_besu"} {
		seedIndexFailure(t, s, runID, "config.json not found")
	}

	rec := postIndexerFailures(t, s, s.handleDeleteIndexerFailures,
		deleteIndexerFailuresRequest{All: true})
	require.Equal(t, http.StatusAccepted, rec.Code)

	var resp deleteIndexerFailuresResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, int64(3), resp.Queued)

	queued, err := s.indexStore.ListIndexFailuresPendingDeletion(
		context.Background(),
	)
	require.NoError(t, err)
	assert.Len(t, queued, 3)

	// Cancelling everything empties the queue and leaves the records.
	cancelRec := postIndexerFailures(t, s,
		s.handleCancelDeleteIndexerFailures,
		deleteIndexerFailuresRequest{All: true})
	require.Equal(t, http.StatusOK, cancelRec.Code)

	var cancelResp cancelIndexerFailuresResponse
	require.NoError(t, json.Unmarshal(cancelRec.Body.Bytes(), &cancelResp))
	assert.Equal(t, int64(3), cancelResp.Cancelled)

	queued, err = s.indexStore.ListIndexFailuresPendingDeletion(
		context.Background(),
	)
	require.NoError(t, err)
	assert.Empty(t, queued)
	assert.Equal(t, int64(3), getIndexerFailures(t, s, "").Total)
}

// TestHandleDeleteIndexerFailures_Rejections covers the guards on the request.
func TestHandleDeleteIndexerFailures_Rejections(t *testing.T) {
	s := newIndexTestServer(t)
	s.runDeleterKick = make(chan struct{}, 1)

	// Without a storage backend that deletes, the request cannot be honoured.
	rec := postIndexerFailures(t, s, s.handleDeleteIndexerFailures,
		deleteIndexerFailuresRequest{All: true})
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	s.storageDeleter = &fakeDeleter{}

	// Neither a list nor "all" is not a request.
	rec = postIndexerFailures(t, s, s.handleDeleteIndexerFailures,
		deleteIndexerFailuresRequest{})
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	bad := httptest.NewRecorder()
	s.handleDeleteIndexerFailures(bad, httptest.NewRequest(
		http.MethodPost, "/api/v1/admin/indexer/failures/delete",
		bytes.NewReader([]byte("not json")),
	))
	assert.Equal(t, http.StatusBadRequest, bad.Code)
}

// TestDrainIndexFailureQueue_DeletesStorageThenRecord covers the worker: the
// record only goes once the stored data is gone, and a storage failure keeps
// the record queued with the reason attached.
func TestDrainIndexFailureQueue_DeletesStorageThenRecord(t *testing.T) {
	s := newIndexTestServer(t)

	deleter := &fakeDeleter{failing: map[string]bool{"200_b_reth": true}}
	s.storageDeleter = deleter
	s.runDeleterKick = make(chan struct{}, 1)

	seedIndexFailure(t, s, "100_a_geth", "config.json not found")
	seedIndexFailure(t, s, "200_b_reth", "config.json not found")

	rec := postIndexerFailures(t, s, s.handleDeleteIndexerFailures,
		deleteIndexerFailuresRequest{All: true})
	require.Equal(t, http.StatusAccepted, rec.Code)

	s.drainIndexFailureQueue(context.Background())

	assert.Equal(t, []string{"100_a_geth"}, deleter.calls())

	// The deleted run lost its record; the failed one kept it, queued, with
	// the reason recorded so the UI can show it.
	listed := getIndexerFailures(t, s, "")
	require.Equal(t, int64(1), listed.Total)
	require.Len(t, listed.Entries, 1)
	assert.Equal(t, "200_b_reth", listed.Entries[0].RunID)
	assert.NotEmpty(t, listed.Entries[0].DeletionRequestedAt)
	assert.Contains(t,
		listed.Entries[0].DeletionError, "storage delete failed",
	)

	// The next pass retries it, and it succeeds once storage recovers.
	deleter.failing = nil
	s.drainIndexFailureQueue(context.Background())

	assert.Equal(t, []string{"100_a_geth", "200_b_reth"}, deleter.calls())
	assert.Zero(t, getIndexerFailures(t, s, "").Total)
}

// stubIndexer stands in for the real indexer in the stats handler test: the
// handler only asks it whether a pass is running and how often they are
// scheduled.
type stubIndexer struct {
	state indexer.State
}

var _ indexer.Indexer = (*stubIndexer)(nil)

func (s *stubIndexer) Start(_ context.Context) error { return nil }
func (s *stubIndexer) Stop() error                   { return nil }
func (s *stubIndexer) RunNow() bool                  { return true }
func (s *stubIndexer) State() indexer.State          { return s.state }

func getIndexerStats(
	t *testing.T, s *server, query string,
) (*httptest.ResponseRecorder, indexerStatsResponse) {
	t.Helper()

	rec := httptest.NewRecorder()
	s.handleIndexerStats(rec, httptest.NewRequest(
		http.MethodGet, "/api/v1/admin/indexer/stats"+query, nil,
	))

	var resp indexerStatsResponse
	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	}

	return rec, resp
}

// TestHandleIndexerStats covers the pass history the admin page charts: the
// newest pass first, the limit clamped, and the indexer's own state alongside.
func TestHandleIndexerStats(t *testing.T) {
	s := newIndexTestServer(t)
	s.indexer = &stubIndexer{state: indexer.State{
		Running:   true,
		StartedAt: time.Now().UTC().Add(-90 * time.Second),
		Trigger:   indexstore.IndexerPassTriggerManual,
		Interval:  5 * time.Minute,
	}}

	ctx := context.Background()
	started := time.Now().UTC().Truncate(time.Second)

	for i := range 3 {
		require.NoError(t, s.indexStore.RecordIndexerPass(
			ctx, &indexstore.IndexerPass{
				StartedAt:      started.Add(time.Duration(i) * time.Minute),
				FinishedAt:     started.Add(time.Duration(i) * time.Minute),
				DurationMs:     int64(100 + i),
				Trigger:        indexstore.IndexerPassTriggerSchedule,
				Status:         indexstore.IndexerPassStatusCompleted,
				DiscoveryPaths: 1,
				StorageRuns:    10,
				IndexedRuns:    9,
				RunsIndexed:    1,
				RunsFailed:     2,
			},
		))
	}

	rec, resp := getIndexerStats(t, s, "")
	require.Equal(t, http.StatusOK, rec.Code)

	assert.True(t, resp.Running)
	assert.Equal(t, "5m0s", resp.Interval)
	require.NotNil(t, resp.CurrentPass, "a running pass is reported")
	assert.Equal(t, indexstore.IndexerPassTriggerManual, resp.CurrentPass.Trigger)
	assert.NotEmpty(t, resp.CurrentPass.StartedAt)
	assert.GreaterOrEqual(t, resp.CurrentPass.ElapsedMs, int64(90_000))
	assert.Equal(t, defaultIndexerPassLimit, resp.Limit)
	require.Len(t, resp.Passes, 3)

	newest := resp.Passes[0]
	assert.Equal(t, int64(102), newest.DurationMs, "newest first")
	assert.Equal(t, indexstore.IndexerPassTriggerSchedule, newest.Trigger)
	assert.Equal(t, indexstore.IndexerPassStatusCompleted, newest.Status)
	assert.Equal(t, 1, newest.DiscoveryPaths)
	assert.Equal(t, 10, newest.StorageRuns)
	assert.Equal(t, 9, newest.IndexedRuns)
	assert.Equal(t, 1, newest.RunsIndexed)
	assert.Equal(t, 2, newest.RunsFailed)
	assert.NotEmpty(t, newest.StartedAt)
	assert.NotEmpty(t, newest.FinishedAt)

	// An explicit limit pages the history.
	_, limited := getIndexerStats(t, s, "?limit=1")
	assert.Equal(t, 1, limited.Limit)
	assert.Len(t, limited.Passes, 1)

	// An ask above the cap is clamped rather than refused.
	_, clamped := getIndexerStats(t, s, "?limit=99999")
	assert.Equal(t, maxIndexerPassLimit, clamped.Limit)
}

// TestHandleIndexerStats_IdleIndexer covers the other half of the running
// state: an idle indexer reports no pass at all, which is what leaves the
// "Run Indexer" button usable.
func TestHandleIndexerStats_IdleIndexer(t *testing.T) {
	s := newIndexTestServer(t)
	s.indexer = &stubIndexer{state: indexer.State{Interval: time.Minute}}

	rec, resp := getIndexerStats(t, s, "")
	require.Equal(t, http.StatusOK, rec.Code)

	assert.False(t, resp.Running)
	assert.Nil(t, resp.CurrentPass)
	assert.Empty(t, resp.Passes)
}

// TestHandleIndexerStats_IndexingDisabled covers a deployment with no indexer:
// there is no history to report and no state to report it from.
func TestHandleIndexerStats_IndexingDisabled(t *testing.T) {
	s := newIndexTestServer(t)

	rec, _ := getIndexerStats(t, s, "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
