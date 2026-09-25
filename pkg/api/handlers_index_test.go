package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	require.NotNil(t, s.indexCache)
	assert.Equal(t, gen, s.indexCache.gen)
	assert.NotEmpty(t, s.indexCache.etag)
	assert.NotEmpty(t, s.indexCache.gzipped)

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

// TestHandleIndex_ETagRevalidation covers the conditional-request path: the
// handler advertises an ETag, answers a matching If-None-Match with a bodyless
// 304, and issues a new ETag once the generation moves on.
func TestHandleIndex_ETagRevalidation(t *testing.T) {
	s := newIndexTestServer(t)
	ctx := context.Background()

	require.NoError(t, s.indexStore.UpsertRun(ctx, &indexstore.Run{
		DiscoveryPath: "dp", RunID: "run-1", Timestamp: 100,
	}))

	rec, _ := getIndex(t, s)

	etag := rec.Header().Get("ETag")
	require.NotEmpty(t, etag)
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	assert.Contains(t, rec.Header().Values("Vary"), "Accept-Encoding")

	// A poll carrying the current validator gets a 304 with no body.
	notModified := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/index", nil)
	req.Header.Set("If-None-Match", etag)
	s.handleIndex(notModified, req)

	assert.Equal(t, http.StatusNotModified, notModified.Code)
	assert.Empty(t, notModified.Body.Bytes())
	assert.Equal(t, etag, notModified.Header().Get("ETag"))

	// Indexing another run must invalidate the validator, otherwise a client
	// would sit on a stale body forever.
	require.NoError(t, s.indexStore.UpsertRun(ctx, &indexstore.Run{
		DiscoveryPath: "dp", RunID: "run-2", Timestamp: 200,
	}))

	stale := httptest.NewRecorder()
	staleReq := httptest.NewRequest(http.MethodGet, "/api/v1/index", nil)
	staleReq.Header.Set("If-None-Match", etag)
	s.handleIndex(stale, staleReq)

	assert.Equal(t, http.StatusOK, stale.Code)
	assert.NotEqual(t, etag, stale.Header().Get("ETag"))
}

// TestHandleIndex_ServesPrecompressedBody verifies the handler hands back the
// cached gzip copy when the client accepts it, and the plain body otherwise.
func TestHandleIndex_ServesPrecompressedBody(t *testing.T) {
	s := newIndexTestServer(t)

	require.NoError(t, s.indexStore.UpsertRun(context.Background(), &indexstore.Run{
		DiscoveryPath: "dp", RunID: "run-1", Timestamp: 100, Client: "geth",
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/index", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	s.handleIndex(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
	assert.Equal(t, strconv.Itoa(rec.Body.Len()),
		rec.Header().Get("Content-Length"))

	reader, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	require.NoError(t, err)

	decoded, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	var resp indexResponse
	require.NoError(t, json.Unmarshal(decoded, &resp))
	require.Len(t, resp.Entries, 1)
	assert.Equal(t, "run-1", resp.Entries[0].RunID)

	// A client that can't take gzip still gets readable JSON.
	plain, plainResp := getIndex(t, s)
	assert.Empty(t, plain.Header().Get("Content-Encoding"))
	assert.Equal(t, decoded, plain.Body.Bytes())
	require.Len(t, plainResp.Entries, 1)
}

// TestHandleIndex_CompressMiddlewareDoesNotReEncode guards the reason the
// handler compresses at all: it must set Content-Encoding so chi's Compress
// middleware passes the cached bytes through instead of gzipping them again.
func TestHandleIndex_CompressMiddlewareDoesNotReEncode(t *testing.T) {
	s := newIndexTestServer(t)
	s.cfg = &config.APIConfig{}
	s.cfg.Auth.AnonymousRead = true

	require.NoError(t, s.indexStore.UpsertRun(context.Background(), &indexstore.Run{
		DiscoveryPath: "dp", RunID: "run-1", Timestamp: 100,
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/index", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	s.buildRouter().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))

	// One round of decoding must yield JSON. A second encoding layer would
	// leave gzip magic bytes here instead.
	reader, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	require.NoError(t, err)

	decoded, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	var resp indexResponse
	require.NoError(t, json.Unmarshal(decoded, &resp))
	assert.Len(t, resp.Entries, 1)
}

func TestAcceptsGzip(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   bool
	}{
		{name: "absent", header: "", want: false},
		{name: "bare token", header: "gzip", want: true},
		{name: "browser list", header: "gzip, deflate, br, zstd", want: true},
		{name: "mixed case", header: "GZip", want: true},
		{name: "with quality", header: "gzip;q=0.8", want: true},
		{name: "refused", header: "gzip;q=0", want: false},
		{name: "refused with spaces", header: "deflate, gzip; q=0", want: false},
		{name: "other encodings only", header: "deflate, br", want: false},
		{name: "not a prefix match", header: "x-gzip", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set("Accept-Encoding", tt.header)
			}

			assert.Equal(t, tt.want, acceptsGzip(req))
		})
	}
}

func TestETagMatches(t *testing.T) {
	const etag = `"abc123"`

	tests := []struct {
		name        string
		ifNoneMatch string
		want        bool
	}{
		{name: "absent", ifNoneMatch: "", want: false},
		{name: "exact", ifNoneMatch: etag, want: true},
		{name: "weak", ifNoneMatch: `W/"abc123"`, want: true},
		{name: "wildcard", ifNoneMatch: "*", want: true},
		{name: "in list", ifNoneMatch: `"other", "abc123"`, want: true},
		{name: "mismatch", ifNoneMatch: `"other"`, want: false},
		{name: "unquoted", ifNoneMatch: "abc123", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, etagMatches(tt.ifNoneMatch, etag))
		})
	}
}
