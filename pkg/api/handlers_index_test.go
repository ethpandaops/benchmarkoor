package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

func newIndexTestServer(t *testing.T) *server {
	t.Helper()

	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	st := indexstore.NewStore(log, &config.APIDatabaseConfig{
		Driver: "sqlite",
		SQLite: config.SQLiteDatabaseConfig{Path: ":memory:"},
	})
	require.NoError(t, st.Start(context.Background()))
	t.Cleanup(func() { _ = st.Stop() })

	return &server{log: log, indexStore: st}
}

func getIndex(t *testing.T, s *server) (*httptest.ResponseRecorder, indexResponse) {
	t.Helper()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/index", nil)
	s.handleIndex(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp indexResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	return rec, resp
}

// TestHandleIndex_SplicesAndCaches covers the RawMessage splicing, the
// generation-keyed cache, and cache invalidation on a new run.
func TestHandleIndex_SplicesAndCaches(t *testing.T) {
	s := newIndexTestServer(t)
	ctx := context.Background()

	require.NoError(t, s.indexStore.UpsertRun(ctx, &indexstore.Run{
		DiscoveryPath: "dp/one",
		RunID:         "run-1",
		Timestamp:     100,
		Status:        "completed",
		Client:        "geth",
		TestsTotal:    3,
		TestsPassed:   2,
		TestsFailed:   1,
		StepsJSON:     `{"import":{"count":2}}`,
		MetadataJSON:  `{"env":"ci"}`,
	}))

	rec1, resp1 := getIndex(t, s)
	require.Len(t, resp1.Entries, 1)

	e := resp1.Entries[0]
	assert.Equal(t, "dp/one", e.DiscoveryPath)
	assert.Equal(t, "run-1", e.RunID)
	assert.Equal(t, "geth", e.Instance.Client)
	assert.Equal(t, 3, e.Tests.TestsTotal)
	// The stored JSON blobs are spliced through verbatim.
	assert.JSONEq(t, `{"import":{"count":2}}`, string(e.Tests.Steps))
	assert.JSONEq(t, `{"env":"ci"}`, string(e.Metadata))

	// The build populated the cache for the current generation.
	gen := s.indexStore.RunsGeneration()
	require.NotNil(t, s.indexCacheBody)
	assert.Equal(t, gen, s.indexCacheGen)

	// A second request with no intervening writes is served from cache and
	// returns byte-for-byte identical output.
	rec2, _ := getIndex(t, s)
	assert.Equal(t, rec1.Body.Bytes(), rec2.Body.Bytes())

	// A new run bumps the generation and invalidates the cache.
	require.NoError(t, s.indexStore.UpsertRun(ctx, &indexstore.Run{
		DiscoveryPath: "dp/two",
		RunID:         "run-2",
		Timestamp:     200,
	}))
	assert.Greater(t, s.indexStore.RunsGeneration(), gen)

	_, resp3 := getIndex(t, s)
	require.Len(t, resp3.Entries, 2)
	// run-2 has the newer timestamp; the store returns rows timestamp DESC and
	// the handler no longer re-sorts, so it must come first.
	assert.Equal(t, "run-2", resp3.Entries[0].RunID)
}

// TestHandleIndex_MissingBlobs verifies the historical defaults: absent steps
// JSON becomes "{}" and absent metadata is omitted.
func TestHandleIndex_MissingBlobs(t *testing.T) {
	s := newIndexTestServer(t)

	require.NoError(t, s.indexStore.UpsertRun(context.Background(), &indexstore.Run{
		DiscoveryPath: "dp",
		RunID:         "r",
		Timestamp:     1,
	}))

	_, resp := getIndex(t, s)
	require.Len(t, resp.Entries, 1)
	assert.JSONEq(t, `{}`, string(resp.Entries[0].Tests.Steps))
	assert.Nil(t, resp.Entries[0].Metadata)
}
