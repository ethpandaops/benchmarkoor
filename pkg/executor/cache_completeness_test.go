package executor

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/sirupsen/logrus"
)

func quietLog() logrus.FieldLogger {
	l := logrus.New()
	l.SetOutput(io.Discard)

	return l
}

func exists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

const (
	rangeTotal = 30 * 1024 * 1024 // large enough to use the parallel range path
	fillByte   = 0xAB
	testETag   = `"etag-v1"`
)

// rangeServer serves HEAD with range support and serves ranged GETs in full,
// except the chunk starting at offset 0 is truncated when shortFirst is set, to
// emulate a server that ends a 206 body early at a clean EOF.
func rangeServer(t *testing.T, shortFirst bool, gets *int64) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", testETag)

		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(rangeTotal))
			w.WriteHeader(http.StatusOK)

			return
		}

		if gets != nil {
			atomic.AddInt64(gets, 1)
		}

		spec := strings.TrimPrefix(r.Header.Get("Range"), "bytes=")
		parts := strings.SplitN(spec, "-", 2)
		start, _ := strconv.ParseInt(parts[0], 10, 64)
		end, _ := strconv.ParseInt(parts[1], 10, 64)

		sendLen := end - start + 1
		if shortFirst && start == 0 {
			sendLen /= 2
		}

		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, rangeTotal))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(bytes.Repeat([]byte{fillByte}, int(sendLen)))
	}))
}

// A range download that is missing bytes must fail loudly rather than caching a
// file with a zero hole.
func TestParallelDownloadRejectsShortChunk(t *testing.T) {
	srv := rangeServer(t, true, nil)
	defer srv.Close()

	cacheDir := t.TempDir()

	_, err := fetchCached(context.Background(), quietLog(), srv.URL, srv.URL, "", cacheDir, "short")
	if err == nil {
		t.Fatal("expected an error for a truncated chunk, got nil")
	}

	if !strings.Contains(err.Error(), "short chunk") {
		t.Fatalf("expected a short-chunk error, got: %v", err)
	}

	// No file should be promoted into the cache on failure.
	if path := cachePath(cacheDir, "short", srv.URL); exists(path) {
		t.Fatalf("a corrupt file was left in the cache at %s", path)
	}
}

// A fully delivered range download still works and yields the exact bytes.
func TestParallelDownloadAcceptsCompleteFile(t *testing.T) {
	srv := rangeServer(t, false, nil)
	defer srv.Close()

	res, err := fetchCached(context.Background(), quietLog(), srv.URL, srv.URL, "", t.TempDir(), "ok")
	if err != nil {
		t.Fatalf("complete download errored: %v", err)
	}

	data, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatalf("reading cached file: %v", err)
	}

	if len(data) != rangeTotal {
		t.Fatalf("size = %d, want %d", len(data), rangeTotal)
	}

	for i, b := range data {
		if b != fillByte {
			t.Fatalf("unexpected byte 0x%02x at offset %d", b, i)
		}
	}
}

// A non-range download whose body is shorter than the advertised size must be
// rejected, not cached.
func TestSequentialDownloadRejectsShortBody(t *testing.T) {
	const total = 1024 * 1024

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No Accept-Ranges: forces the single-GET path.
		w.Header().Set("ETag", testETag)

		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(total))
			w.WriteHeader(http.StatusOK)

			return
		}

		w.Header().Set("Content-Length", strconv.Itoa(total/2))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte{fillByte}, total/2))
	}))
	defer srv.Close()

	cacheDir := t.TempDir()

	_, err := fetchCached(context.Background(), quietLog(), srv.URL, srv.URL, "", cacheDir, "seq")
	if err == nil {
		t.Fatal("expected an error for a truncated body, got nil")
	}

	if path := cachePath(cacheDir, "seq", srv.URL); exists(path) {
		t.Fatalf("a corrupt file was left in the cache at %s", path)
	}
}

// dirTarGz builds a gzipped tar containing the given directory entries.
func dirTarGz(t *testing.T, dirs ...string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for _, d := range dirs {
		if err := tw.WriteHeader(&tar.Header{
			Name:     d + "/",
			Typeflag: tar.TypeDir,
			Mode:     0o755,
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

// An interrupted extraction (genesis fails after fixtures succeed) must not be
// treated as a complete cache: the next run re-downloads, and once both halves
// are present the cache is reused without downloading again.
func TestEESTCacheReDownloadsUntilComplete(t *testing.T) {
	fixturesTar := dirTarGz(t, config.DefaultEESTFixturesSubdir)
	genesisTar := dirTarGz(t)

	var genesisServed int64
	var failGenesis atomic.Bool
	failGenesis.Store(true)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "genesis") {
			if failGenesis.Load() {
				http.Error(w, "interrupted", http.StatusInternalServerError)

				return
			}

			atomic.AddInt64(&genesisServed, 1)
			_, _ = w.Write(genesisTar)

			return
		}

		_, _ = w.Write(fixturesTar)
	}))
	defer srv.Close()

	cacheDir := t.TempDir()
	cfg := &config.EESTFixturesSource{
		GitHubRepo:    "owner/repo",
		GitHubRelease: "v1",
		FixturesURL:   srv.URL + "/fixtures.tar.gz",
		GenesisURL:    srv.URL + "/genesis.tar.gz",
	}

	marker := filepath.Join(cacheDir, "eest", hashRepoURL(cfg.GitHubRepo), cfg.GitHubRelease, ".complete")

	newSource := func() *EESTSource {
		return NewEESTSource(quietLog(), cfg, cacheDir, nil, "")
	}

	// First run: genesis fails, so preparation fails and nothing is marked done.
	if _, err := newSource().Prepare(context.Background()); err == nil {
		t.Fatal("expected first Prepare to fail on genesis")
	}

	if exists(marker) {
		t.Fatal("completion marker must not exist after an interrupted run")
	}

	// Second run: genesis works now. The partial cache must be re-downloaded.
	failGenesis.Store(false)

	if _, err := newSource().Prepare(context.Background()); err != nil {
		t.Fatalf("second Prepare failed: %v", err)
	}

	if !exists(marker) {
		t.Fatal("completion marker should exist after a successful run")
	}

	if got := atomic.LoadInt64(&genesisServed); got != 1 {
		t.Fatalf("genesis should have been downloaded once, served %d", got)
	}

	// Third run: a complete cache is reused without downloading again.
	if _, err := newSource().Prepare(context.Background()); err != nil {
		t.Fatalf("third Prepare failed: %v", err)
	}

	if got := atomic.LoadInt64(&genesisServed); got != 1 {
		t.Fatalf("complete cache was re-downloaded; genesis served %d times", got)
	}
}
