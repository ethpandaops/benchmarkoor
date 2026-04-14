package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// etagServer is a tiny test HTTP server that serves `payload` with a
// configurable ETag. Call SetETag / SetPayload between runs to simulate
// the origin being updated.
type etagServer struct {
	mu         sync.Mutex
	payload    []byte
	etag       string
	getCount   atomic.Int64
	headCount  atomic.Int64
	noETag     bool // when true, server omits ETag from responses
	headStatus int  // 0 = default 200; set to simulate HEAD failures
}

func newEtagServer(t *testing.T, payload []byte, etag string) (*etagServer, *httptest.Server) {
	t.Helper()

	es := &etagServer{payload: payload, etag: etag}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		es.mu.Lock()
		payload := es.payload
		etag := es.etag
		noETag := es.noETag
		headStatus := es.headStatus
		es.mu.Unlock()

		if r.Method == http.MethodHead {
			es.headCount.Add(1)

			if headStatus != 0 {
				w.WriteHeader(headStatus)
				return
			}

			if !noETag {
				w.Header().Set("ETag", etag)
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			w.WriteHeader(http.StatusOK)

			return
		}

		es.getCount.Add(1)

		if !noETag {
			w.Header().Set("ETag", etag)
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))

	return es, srv
}

func (es *etagServer) SetETag(etag string) {
	es.mu.Lock()
	defer es.mu.Unlock()
	es.etag = etag
}

func (es *etagServer) SetPayload(payload []byte) {
	es.mu.Lock()
	defer es.mu.Unlock()
	es.payload = payload
}

func (es *etagServer) DisableETag() {
	es.mu.Lock()
	defer es.mu.Unlock()
	es.noETag = true
}

func (es *etagServer) BreakHEAD() {
	es.mu.Lock()
	defer es.mu.Unlock()
	es.headStatus = http.StatusInternalServerError
}

func TestFetchCached_HitWhenValidatorsMatch(t *testing.T) {
	es, srv := newEtagServer(t, []byte("hello"), `"v1"`)
	defer srv.Close()

	dir := t.TempDir()
	log := logrus.New()
	log.SetLevel(logrus.DebugLevel)

	// First fetch downloads.
	r1, err := fetchCached(context.Background(), log, srv.URL, srv.URL, "", dir, "x")
	require.NoError(t, err)
	assert.True(t, r1.Changed)

	// Second fetch sees the same ETag and should NOT re-download.
	r2, err := fetchCached(context.Background(), log, srv.URL, srv.URL, "", dir, "x")
	require.NoError(t, err)
	assert.False(t, r2.Changed)
	assert.Equal(t, r1.Path, r2.Path)

	assert.Equal(t, int64(1), es.getCount.Load(), "no re-download on matching ETag")
	assert.GreaterOrEqual(t, es.headCount.Load(), int64(1), "second fetch does a HEAD")
}

func TestFetchCached_MissWhenETagChanges(t *testing.T) {
	es, srv := newEtagServer(t, []byte("hello"), `"v1"`)
	defer srv.Close()

	dir := t.TempDir()
	log := logrus.New()

	_, err := fetchCached(context.Background(), log, srv.URL, srv.URL, "", dir, "x")
	require.NoError(t, err)

	// Origin publishes a new revision at the same URL.
	es.SetETag(`"v2"`)
	es.SetPayload([]byte("updated"))

	r2, err := fetchCached(context.Background(), log, srv.URL, srv.URL, "", dir, "x")
	require.NoError(t, err)
	assert.True(t, r2.Changed, "ETag mismatch must trigger re-download")

	got, err := os.ReadFile(r2.Path) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Equal(t, "updated", string(got))
	assert.Equal(t, int64(2), es.getCount.Load())
}

func TestFetchCached_LegacySidecarMissingReDownloads(t *testing.T) {
	_, srv := newEtagServer(t, []byte("hello"), `"v1"`)
	defer srv.Close()

	dir := t.TempDir()
	log := logrus.New()

	// Plant a pre-existing cache entry WITHOUT a .meta sidecar — mimics a
	// cache dir populated by an older benchmarkoor binary.
	stalePath := cachePath(dir, "x", srv.URL)
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(stalePath, []byte("stale"), 0644))

	r, err := fetchCached(context.Background(), log, srv.URL, srv.URL, "", dir, "x")
	require.NoError(t, err)
	assert.True(t, r.Changed)

	got, err := os.ReadFile(r.Path) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Equal(t, "hello", string(got), "re-downloaded from origin, not stale")

	// Meta sidecar must now exist.
	_, err = os.Stat(stalePath + ".meta")
	require.NoError(t, err)
}

func TestFetchCached_NoValidatorsFallsBackToCache(t *testing.T) {
	es, srv := newEtagServer(t, []byte("hello"), `"v1"`)
	defer srv.Close()

	dir := t.TempDir()
	log := logrus.New()

	// First fetch populates cache (with ETag).
	_, err := fetchCached(context.Background(), log, srv.URL, srv.URL, "", dir, "x")
	require.NoError(t, err)
	require.Equal(t, int64(1), es.getCount.Load())

	// Origin stops sending ETag/Last-Modified. The next fetch can't
	// validate, so it should reuse the cache.
	es.DisableETag()

	_, err = fetchCached(context.Background(), log, srv.URL, srv.URL, "", dir, "x")
	require.NoError(t, err)

	assert.Equal(t, int64(1), es.getCount.Load(), "cache reused when origin has no validators")
}

func TestFetchCached_HeadFailureFallsBackToCache(t *testing.T) {
	es, srv := newEtagServer(t, []byte("hello"), `"v1"`)
	defer srv.Close()

	dir := t.TempDir()
	log := logrus.New()

	_, err := fetchCached(context.Background(), log, srv.URL, srv.URL, "", dir, "x")
	require.NoError(t, err)

	es.BreakHEAD()

	_, err = fetchCached(context.Background(), log, srv.URL, srv.URL, "", dir, "x")
	require.NoError(t, err)

	assert.Equal(t, int64(1), es.getCount.Load(), "HEAD failure must not trigger a re-download")
}

func TestFetchCached_CacheMissCoexistsWithOtherPrefixes(t *testing.T) {
	_, srv := newEtagServer(t, []byte("hello"), `"v1"`)
	defer srv.Close()

	dir := t.TempDir()
	log := logrus.New()

	ra, err := fetchCached(context.Background(), log, srv.URL, srv.URL, "", dir, "archive")
	require.NoError(t, err)

	rb, err := fetchCached(context.Background(), log, srv.URL, srv.URL, "", dir, "opcode")
	require.NoError(t, err)

	// Same URL but different prefix must land in distinct cache files so
	// the two subsystems don't clobber each other.
	assert.NotEqual(t, ra.Path, rb.Path)
	assert.Equal(t, filepath.Dir(ra.Path), dir)
}
