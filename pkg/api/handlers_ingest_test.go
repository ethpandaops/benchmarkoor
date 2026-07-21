package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

func newIngestTestServer(t *testing.T) *server {
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

// ingestHandler builds the same middleware chain routes.go wires for the
// ingest endpoint (minus auth, which is orthogonal to body-size limits).
func ingestHandler(s *server) http.Handler {
	return s.gzipRequestBody(http.HandlerFunc(s.handleIngestRun))
}

func validReportBody() string {
	return `{"discovery_path":"dp","run_id":"run-1","status":"running","timestamp":1}`
}

func gzipCompress(t *testing.T, data []byte) []byte {
	t.Helper()

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	_, err := gz.Write(data)
	require.NoError(t, err)
	require.NoError(t, gz.Close())

	return buf.Bytes()
}

func TestIngest_ValidUncompressedReportAccepted(t *testing.T) {
	s := newIngestTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/runs", strings.NewReader(validReportBody()))
	rec := httptest.NewRecorder()

	ingestHandler(s).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestIngest_ValidGzipReportAccepted(t *testing.T) {
	s := newIngestTestServer(t)

	compressed := gzipCompress(t, []byte(validReportBody()))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/runs", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()

	ingestHandler(s).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

// TestIngest_OversizedUncompressedBodyRejected covers a large plain body
// (no gzip involved at all) - the raw-body cap must apply regardless of
// Content-Encoding.
func TestIngest_OversizedUncompressedBodyRejected(t *testing.T) {
	s := newIngestTestServer(t)

	oversized := bytes.Repeat([]byte("a"), maxIngestBodyBytes+1)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/runs", bytes.NewReader(oversized))
	rec := httptest.NewRecorder()

	ingestHandler(s).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

// TestIngest_GzipBombRejected is a regression test for the decompression
// bomb verified live against a running server: a small, highly-compressible
// gzip payload that decompresses far past maxIngestDecompressedBytes must
// be rejected before the full decompressed size is ever held in memory.
func TestIngest_GzipBombRejected(t *testing.T) {
	s := newIngestTestServer(t)

	// Decompresses to well over maxIngestDecompressedBytes, but the
	// compressed form itself is tiny and well under maxIngestBodyBytes -
	// this is exactly the asymmetry that makes decompression bombs work.
	bomb := bytes.Repeat([]byte{0}, maxIngestDecompressedBytes*2)
	compressed := gzipCompress(t, bomb)

	require.Less(t, len(compressed), maxIngestBodyBytes,
		"the compressed payload must stay under the raw-body cap for this test to actually exercise the decompressed-size cap")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/runs", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()

	ingestHandler(s).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

// TestIngest_LargeButUnderLimitGzipReportAccepted guards against an overly
// strict cap or an off-by-one that would reject legitimate large payloads
// (the per-test gas map can be sizable for big suites) even though they're
// comfortably under the limit.
func TestIngest_LargeButUnderLimitGzipReportAccepted(t *testing.T) {
	s := newIngestTestServer(t)

	report := validReportBody()
	padding := maxIngestDecompressedBytes - len(report) - 4096 // comfortably under the cap
	padded := report[:len(report)-1] + `,"instance_id":"` + strings.Repeat("x", padding) + `"}`

	require.Less(t, len(padded), maxIngestDecompressedBytes)

	compressed := gzipCompress(t, []byte(padded))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/runs", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()

	ingestHandler(s).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}
