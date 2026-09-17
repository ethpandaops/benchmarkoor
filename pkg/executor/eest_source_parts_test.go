package executor

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

// fileTarGz builds a .tar.gz holding one regular file under the default
// fixtures subdir, padded so it can be split into several non-empty parts.
func fileTarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: config.DefaultEESTFixturesSubdir + "/", Typeflag: tar.TypeDir, Mode: 0o755,
	}))
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     config.DefaultEESTFixturesSubdir + "/" + name,
		Typeflag: tar.TypeReg,
		Mode:     0o644,
		Size:     int64(len(content)),
	}))

	_, err := tw.Write(content)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())

	return buf.Bytes()
}

// A split fixtures tarball is streamed part by part, in order, through one
// gzip reader: the base .tar.gz is never requested and the extracted tree
// matches the unsplit archive.
func TestEESTSource_FixturesURLParts(t *testing.T) {
	content := bytes.Repeat([]byte("benchmarkoor split-part fixture payload\n"), 256)
	tarball := fileTarGz(t, "test.json", content)
	parts := splitBytes(tarball, 3)

	var (
		mu        sync.Mutex
		requested []string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requested = append(requested, r.URL.Path)
		mu.Unlock()

		switch r.URL.Path {
		case "/f.tar.gz.part-000":
			_, _ = w.Write(parts[0])
		case "/f.tar.gz.part-001":
			_, _ = w.Write(parts[1])
		case "/f.tar.gz.part-002":
			_, _ = w.Write(parts[2])
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &config.EESTFixturesSource{
		FixturesURLParts: []string{
			srv.URL + "/f.tar.gz.part-000",
			srv.URL + "/f.tar.gz.part-001",
			srv.URL + "/f.tar.gz.part-002",
		},
	}
	cacheDir := t.TempDir()
	src := NewEESTSource(quietLog(), cfg, cacheDir, nil, "")

	_, err := src.Prepare(context.Background())
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(
		src.fixturesDir, config.DefaultEESTFixturesSubdir, "test.json",
	))
	require.NoError(t, err)
	assert.Equal(t, content, got)

	assert.Equal(t, []string{
		"/f.tar.gz.part-000", "/f.tar.gz.part-001", "/f.tar.gz.part-002",
	}, requested, "parts must be fetched strictly in order and nothing else")

	// The cache key covers the whole part list, so a second Prepare reuses it.
	_, err = NewEESTSource(quietLog(), cfg, cacheDir, nil, "").Prepare(context.Background())
	require.NoError(t, err)
	assert.Len(t, requested, 3, "a complete cache must not re-download the parts")
}

// A missing part fails the download and the error names the part URL.
func TestEESTSource_FixturesURLParts_MissingPart(t *testing.T) {
	tarball := fileTarGz(t, "test.json", bytes.Repeat([]byte("x"), 4096))
	parts := splitBytes(tarball, 2)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/f.tar.gz.part-000" {
			_, _ = w.Write(parts[0])

			return
		}

		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfg := &config.EESTFixturesSource{
		FixturesURLParts: []string{
			srv.URL + "/f.tar.gz.part-000",
			srv.URL + "/f.tar.gz.part-001",
		},
	}

	cacheDir := t.TempDir()
	src := NewEESTSource(quietLog(), cfg, cacheDir, nil, "")

	_, err := src.Prepare(context.Background())
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "/f.tar.gz.part-001"), err.Error())
	assert.Contains(t, err.Error(), "404")

	assert.False(t, exists(filepath.Join(
		cacheDir, "eest-url", hashRepoURL(src.fixturesURLCacheKey()), ".complete",
	)), "a failed download must not be marked complete")
}
