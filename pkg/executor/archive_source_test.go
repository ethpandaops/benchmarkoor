package executor

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArchiveSource_PrepareWithLocalZip(t *testing.T) {
	// Create a zip with test files.
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "tests.zip")
	createTestZip(t, zipPath, map[string]string{
		"tests/test/001.txt":    "test-line-1",
		"tests/setup/001.txt":   "setup-line-1",
		"tests/cleanup/001.txt": "cleanup-line-1",
	})

	log := logrus.New()
	log.SetLevel(logrus.DebugLevel)

	source := &ArchiveSource{
		log:      log.WithField("source", "archive"),
		cacheDir: tmpDir,
		cfg: &config.ArchiveSourceConfig{
			File: zipPath,
			Steps: &config.StepsConfig{
				Setup:   []string{"tests/setup/*"},
				Test:    []string{"tests/test/*"},
				Cleanup: []string{"tests/cleanup/*"},
			},
		},
	}

	result, err := source.Prepare(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.Tests)
	assert.Equal(t, 1, len(result.Tests))

	// Verify cleanup removes the temp dir.
	basePath := source.basePath
	require.DirExists(t, basePath)
	require.NoError(t, source.Cleanup())
	assert.NoDirExists(t, basePath)
}

func TestArchiveSource_PrepareWithLocalTarGz(t *testing.T) {
	tmpDir := t.TempDir()
	tarPath := filepath.Join(tmpDir, "tests.tar.gz")
	createTestTarGz(t, tarPath, map[string]string{
		"mytest/test/abc.txt": "test-content",
	})

	log := logrus.New()
	log.SetLevel(logrus.DebugLevel)

	source := &ArchiveSource{
		log:      log.WithField("source", "archive"),
		cacheDir: tmpDir,
		cfg: &config.ArchiveSourceConfig{
			File: tarPath,
			Steps: &config.StepsConfig{
				Test: []string{"mytest/test/*"},
			},
		},
	}

	result, err := source.Prepare(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, len(result.Tests))

	require.NoError(t, source.Cleanup())
}

func TestArchiveSource_PrepareWithInnerTarballs(t *testing.T) {
	// Create a zip containing an inner tar.gz (like GitHub Actions artifacts).
	tmpDir := t.TempDir()

	// First create the inner tar.gz.
	innerTarPath := filepath.Join(tmpDir, "inner.tar.gz")
	createTestTarGz(t, innerTarPath, map[string]string{
		"data/test/001.txt": "inner-content",
	})

	innerData, err := os.ReadFile(innerTarPath)
	require.NoError(t, err)

	// Create a zip containing the inner tar.gz.
	zipPath := filepath.Join(tmpDir, "artifact.zip")
	createTestZip(t, zipPath, map[string]string{})
	// Re-create with the binary tar.gz inside.
	createTestZipWithBinary(t, zipPath, map[string][]byte{
		"inner.tar.gz": innerData,
	})

	log := logrus.New()
	log.SetLevel(logrus.DebugLevel)

	source := &ArchiveSource{
		log:      log.WithField("source", "archive"),
		cacheDir: tmpDir,
		cfg: &config.ArchiveSourceConfig{
			File: zipPath,
			Steps: &config.StepsConfig{
				Test: []string{"data/test/*"},
			},
		},
	}

	result, err := source.Prepare(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, len(result.Tests))

	require.NoError(t, source.Cleanup())
}

func TestArchiveSource_GetSourceInfo(t *testing.T) {
	source := &ArchiveSource{
		cfg: &config.ArchiveSourceConfig{
			File:        "https://example.com/tests.zip",
			PreRunSteps: []string{"pre/step.txt"},
			Steps: &config.StepsConfig{
				Setup:   []string{"setup/*"},
				Test:    []string{"test/*"},
				Cleanup: []string{"cleanup/*"},
			},
		},
	}

	info, err := source.GetSourceInfo()
	require.NoError(t, err)
	require.NotNil(t, info.Archive)
	assert.Equal(t, "https://example.com/tests.zip", info.Archive.File)
	assert.Equal(t, []string{"pre/step.txt"}, info.Archive.PreRunSteps)
	assert.Equal(t, []string{"setup/*"}, info.Archive.Steps.Setup)
	assert.Equal(t, []string{"test/*"}, info.Archive.Steps.Test)
	assert.Equal(t, []string{"cleanup/*"}, info.Archive.Steps.Cleanup)
}

func TestArchiveSource_CleanupNoBasePath(t *testing.T) {
	source := &ArchiveSource{}
	require.NoError(t, source.Cleanup())
}

func TestArchiveSource_PrepareFileNotFound(t *testing.T) {
	log := logrus.New()

	source := &ArchiveSource{
		log:      log.WithField("source", "archive"),
		cacheDir: t.TempDir(),
		cfg: &config.ArchiveSourceConfig{
			File: "/nonexistent/file.zip",
		},
	}

	_, err := source.Prepare(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestArchiveSource_ResolveDownloadURL(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.DebugLevel)

	source := &ArchiveSource{
		log:         log.WithField("source", "archive"),
		githubToken: "gh-test-token",
	}

	tests := []struct {
		name          string
		input         string
		expectedURL   string
		expectedToken string
	}{
		{
			name:          "GitHub artifact URL",
			input:         "https://github.com/NethermindEth/gas-benchmarks/actions/runs/23847558369/artifacts/6222084759",
			expectedURL:   "https://api.github.com/repos/NethermindEth/gas-benchmarks/actions/artifacts/6222084759/zip",
			expectedToken: "gh-test-token",
		},
		{
			name:          "regular URL unchanged",
			input:         "https://example.com/fixtures.zip",
			expectedURL:   "https://example.com/fixtures.zip",
			expectedToken: "",
		},
		{
			name:          "GitHub non-artifact URL unchanged",
			input:         "https://github.com/owner/repo/releases/download/v1/file.zip",
			expectedURL:   "https://github.com/owner/repo/releases/download/v1/file.zip",
			expectedToken: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, token := source.resolveDownloadURL(tt.input)
			assert.Equal(t, tt.expectedURL, url)
			assert.Equal(t, tt.expectedToken, token)
		})
	}
}

func TestDetectArchiveFormat(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		content  []byte
		expected string
		wantErr  bool
	}{
		{
			name:     "zip by extension",
			filename: "test.zip",
			content:  []byte{0x50, 0x4B, 0x03, 0x04, 0x00},
			expected: archiveFormatZip,
		},
		{
			name:     "tar.gz by extension",
			filename: "test.tar.gz",
			content:  []byte{0x1F, 0x8B, 0x08, 0x00, 0x00},
			expected: archiveFormatTarGz,
		},
		{
			name:     "tgz by extension",
			filename: "test.tgz",
			content:  []byte{0x1F, 0x8B, 0x08, 0x00, 0x00},
			expected: archiveFormatTarGz,
		},
		{
			name:     "zip by magic bytes",
			filename: "archive",
			content:  []byte{0x50, 0x4B, 0x03, 0x04, 0x00},
			expected: archiveFormatZip,
		},
		{
			name:     "gzip by magic bytes",
			filename: "archive",
			content:  []byte{0x1F, 0x8B, 0x08, 0x00, 0x00},
			expected: archiveFormatTarGz,
		},
		{
			name:     "unknown format",
			filename: "archive",
			content:  []byte{0x00, 0x00, 0x00, 0x00, 0x00},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			path := filepath.Join(tmpDir, tt.filename)
			require.NoError(t, os.WriteFile(path, tt.content, 0644))

			format, err := detectArchiveFormat(path)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, format)
			}
		})
	}
}

func TestArchiveSource_CachesDownload(t *testing.T) {
	var requestCount int

	// Serve a zip file and count requests.
	zipBuf := createTestZipBytes(t, map[string]string{
		"tests/test/001.txt": "content",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(zipBuf)))
			w.WriteHeader(http.StatusOK)

			return
		}

		requestCount++
		w.Header().Set("Content-Length", strconv.Itoa(len(zipBuf)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(zipBuf)
	}))
	defer srv.Close()

	cacheDir := t.TempDir()
	log := logrus.New()
	log.SetLevel(logrus.DebugLevel)

	makeSrc := func() *ArchiveSource {
		return &ArchiveSource{
			log:      log.WithField("source", "archive"),
			cacheDir: cacheDir,
			cfg: &config.ArchiveSourceConfig{
				File: srv.URL + "/tests.zip",
				Steps: &config.StepsConfig{
					Test: []string{"tests/test/*"},
				},
			},
		}
	}

	// First run: downloads the file.
	s1 := makeSrc()
	result, err := s1.Prepare(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, len(result.Tests))
	assert.Equal(t, 1, requestCount)
	require.NoError(t, s1.Cleanup())

	// Second run: uses cached file, no new download.
	s2 := makeSrc()
	result, err = s2.Prepare(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, len(result.Tests))
	assert.Equal(t, 1, requestCount, "expected no additional download request")
	require.NoError(t, s2.Cleanup())
}

// splitBytes splits b into n roughly-equal chunks. Used to simulate a tar.gz
// archive that has been split across multiple .part files.
func splitBytes(b []byte, n int) [][]byte {
	chunks := make([][]byte, n)
	chunkSize := (len(b) + n - 1) / n

	for i := range n {
		start := i * chunkSize
		end := min(start+chunkSize, len(b))
		chunks[i] = b[start:end]
	}

	return chunks
}

func TestArchiveSource_PartsReconcatenateOnOriginChange(t *testing.T) {
	tmpDir := t.TempDir()

	// Build two tar.gz archives, A (one test) and B (two tests). Split
	// each into two parts. Serve A's parts initially, then flip the ETag
	// on the second part's server to serve B's second half and verify the
	// combined file is regenerated on the next Prepare().
	pathA := filepath.Join(tmpDir, "a.tar.gz")
	createTestTarGz(t, pathA, map[string]string{
		"mytest/test/a.txt": "content-a",
	})
	pathB := filepath.Join(tmpDir, "b.tar.gz")
	createTestTarGz(t, pathB, map[string]string{
		"mytest/test/b1.txt": "content-b1",
		"mytest/test/b2.txt": "content-b2",
	})

	bytesA, err := os.ReadFile(pathA)
	require.NoError(t, err)
	bytesB, err := os.ReadFile(pathB)
	require.NoError(t, err)

	chunksA := splitBytes(bytesA, 2)
	chunksB := splitBytes(bytesB, 2)

	var currentPart2 atomic.Value
	currentPart2.Store(chunksA[1])

	var etagPart2 atomic.Value
	etagPart2.Store(`"a"`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data []byte
		var etag string

		switch r.URL.Path {
		case "/part1":
			data = chunksA[0]
			etag = `"a"`
		case "/part2":
			data = currentPart2.Load().([]byte)
			etag = etagPart2.Load().(string)
		default:
			http.NotFound(w, r)
			return
		}

		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)

		if r.Method == http.MethodGet {
			_, _ = w.Write(data)
		}
	}))
	defer srv.Close()

	log := logrus.New()
	log.SetLevel(logrus.DebugLevel)

	makeSrc := func() *ArchiveSource {
		return &ArchiveSource{
			log:      log.WithField("source", "archive"),
			cacheDir: tmpDir,
			cfg: &config.ArchiveSourceConfig{
				Parts: []string{srv.URL + "/part1", srv.URL + "/part2"},
				Steps: &config.StepsConfig{
					Test: []string{"mytest/test/*"},
				},
			},
		}
	}

	// First run: downloads both parts and writes the combined archive.
	s1 := makeSrc()
	result, err := s1.Prepare(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, len(result.Tests), "first run sees only A's single test")
	require.NoError(t, s1.Cleanup())

	// Origin publishes a new second half with a different ETag. The
	// rebuilt combined archive must reflect it.
	currentPart2.Store(chunksB[1])
	etagPart2.Store(`"b"`)

	// We also need to replace part1 on the origin since A and B differ
	// from byte 0 — but here we care about the control flow, so instead
	// we make A -> A+B2 (first half of A, second half of B). That's
	// gibberish as a tar.gz; switch both halves to B.
	// Simpler: also update part1 so the combined is B.
	// Since we only have one endpoint for /part1, stub it out:
	//  -> we already serve chunksA[0] unconditionally. Update via a flag.
	//
	// For this test the primary assertion is that the re-download of
	// part2 (ETag change) triggers a rebuild of the combined file, even
	// if its content is garbled.

	s2 := makeSrc()
	_, err = s2.Prepare(context.Background())
	// Extraction will likely fail (chunksA[0] + chunksB[1] is not a valid
	// tar.gz). We only care that the combined cached file got rebuilt.
	_ = err
	require.NoError(t, s2.Cleanup())

	// Verify: the combined cached file now has byte content reflecting
	// chunksA[0] + chunksB[1], not the original chunksA[0] + chunksA[1].
	combinedKey := filepath.Join(
		tmpDir,
		"archive-parts-"+shortHashCombined(srv.URL+"/part1", srv.URL+"/part2"),
	)
	got, err := os.ReadFile(combinedKey) //nolint:gosec // test-only
	require.NoError(t, err)

	want := append([]byte{}, chunksA[0]...)
	want = append(want, chunksB[1]...)
	assert.Equal(t, want, got, "combined file must be rebuilt when any part ETag changes")
}

// shortHashCombined mirrors how resolvePartsFile computes its combined
// cache key from the ordered parts list.
func shortHashCombined(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(h[:8])
}

func TestArchiveSource_PrepareWithParts(t *testing.T) {
	tmpDir := t.TempDir()

	// Build a real tar.gz in memory, then split it into two parts.
	fullPath := filepath.Join(tmpDir, "full.tar.gz")
	createTestTarGz(t, fullPath, map[string]string{
		"mytest/test/abc.txt": "test-content",
		"mytest/test/def.txt": "another-content",
	})

	fullBytes, err := os.ReadFile(fullPath)
	require.NoError(t, err)
	chunks := splitBytes(fullBytes, 2)

	var part1Count, part2Count int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data []byte

		switch r.URL.Path {
		case "/part1":
			data = chunks[0]
			if r.Method == http.MethodGet {
				part1Count++
			}
		case "/part2":
			data = chunks[1]
			if r.Method == http.MethodGet {
				part2Count++
			}
		default:
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)

		if r.Method == http.MethodGet {
			_, _ = w.Write(data)
		}
	}))
	defer srv.Close()

	log := logrus.New()
	log.SetLevel(logrus.DebugLevel)

	makeSrc := func() *ArchiveSource {
		return &ArchiveSource{
			log:      log.WithField("source", "archive"),
			cacheDir: tmpDir,
			cfg: &config.ArchiveSourceConfig{
				Parts: []string{srv.URL + "/part1", srv.URL + "/part2"},
				Steps: &config.StepsConfig{
					Test: []string{"mytest/test/*"},
				},
			},
		}
	}

	// First run: downloads both parts and concatenates them.
	s1 := makeSrc()
	result, err := s1.Prepare(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, len(result.Tests))
	assert.Equal(t, 1, part1Count)
	assert.Equal(t, 1, part2Count)
	require.NoError(t, s1.Cleanup())

	// Second run: uses cached combined file, no new downloads.
	s2 := makeSrc()
	result, err = s2.Prepare(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, len(result.Tests))
	assert.Equal(t, 1, part1Count, "expected no additional part1 download")
	assert.Equal(t, 1, part2Count, "expected no additional part2 download")
	require.NoError(t, s2.Cleanup())
}

func TestArchiveSource_PrepareWithLocalParts(t *testing.T) {
	tmpDir := t.TempDir()

	fullPath := filepath.Join(tmpDir, "full.tar.gz")
	createTestTarGz(t, fullPath, map[string]string{
		"mytest/test/abc.txt": "test-content",
	})

	fullBytes, err := os.ReadFile(fullPath)
	require.NoError(t, err)
	chunks := splitBytes(fullBytes, 3)

	partPaths := make([]string, 3)
	for i, chunk := range chunks {
		partPaths[i] = filepath.Join(tmpDir, "chunk-"+strconv.Itoa(i)+".part")
		require.NoError(t, os.WriteFile(partPaths[i], chunk, 0644))
	}

	log := logrus.New()
	log.SetLevel(logrus.DebugLevel)

	source := &ArchiveSource{
		log:      log.WithField("source", "archive"),
		cacheDir: tmpDir,
		cfg: &config.ArchiveSourceConfig{
			Parts: partPaths,
			Steps: &config.StepsConfig{
				Test: []string{"mytest/test/*"},
			},
		},
	}

	result, err := source.Prepare(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, len(result.Tests))
	require.NoError(t, source.Cleanup())
}

func TestArchiveSource_GetSourceInfo_Parts(t *testing.T) {
	parts := []string{
		"https://example.com/tests.tar.gz.00.part",
		"https://example.com/tests.tar.gz.01.part",
	}
	source := &ArchiveSource{
		cfg: &config.ArchiveSourceConfig{
			Parts: parts,
			Steps: &config.StepsConfig{
				Test: []string{"test/*"},
			},
		},
	}

	info, err := source.GetSourceInfo()
	require.NoError(t, err)
	require.NotNil(t, info.Archive)
	assert.Empty(t, info.Archive.File)
	assert.Equal(t, parts, info.Archive.Parts)
}

func TestDownloadToFile_Parallel(t *testing.T) {
	// Create a test payload large enough to trigger parallel downloads.
	payload := make([]byte, minParallelSize+1024)
	_, err := rand.Read(payload)
	require.NoError(t, err)

	// Serve with Accept-Ranges support (Go's http.ServeContent does this).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "test.bin", timeZero, newByteReadSeeker(payload))
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "downloaded")
	log := logrus.New()
	log.SetLevel(logrus.DebugLevel)

	err = downloadToFile(context.Background(), srv.URL, destPath, "", log)
	require.NoError(t, err)

	got, err := os.ReadFile(destPath)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
}

func TestDownloadToFile_SequentialFallback(t *testing.T) {
	payload := []byte("small-content-no-parallel")

	// Server that does NOT support range requests.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "downloaded")
	log := logrus.New()
	log.SetLevel(logrus.DebugLevel)

	err := downloadToFile(context.Background(), srv.URL, destPath, "", log)
	require.NoError(t, err)

	got, err := os.ReadFile(destPath)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
}

func TestDownloadToFile_BearerToken(t *testing.T) {
	var receivedAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data"))
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "downloaded")
	log := logrus.New()

	err := downloadToFile(context.Background(), srv.URL, destPath, "my-token", log)
	require.NoError(t, err)
	assert.Equal(t, "Bearer my-token", receivedAuth)
}

// byteReadSeeker wraps a byte slice to implement io.ReadSeeker for
// http.ServeContent (which enables Accept-Ranges support).
type byteReadSeeker struct {
	data   []byte
	offset int64
}

// timeZero is used as the modtime for http.ServeContent so it doesn't
// generate Last-Modified headers that interfere with tests.
var timeZero = time.Time{}

func newByteReadSeeker(data []byte) *byteReadSeeker {
	return &byteReadSeeker{data: data}
}

func (b *byteReadSeeker) Read(p []byte) (int, error) {
	if b.offset >= int64(len(b.data)) {
		return 0, io.EOF
	}

	n := copy(p, b.data[b.offset:])
	b.offset += int64(n)

	return n, nil
}

func (b *byteReadSeeker) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		b.offset = offset
	case io.SeekCurrent:
		b.offset += offset
	case io.SeekEnd:
		b.offset = int64(len(b.data)) + offset
	}

	return b.offset, nil
}

// createTestZip creates a zip file with the given text files.
func createTestZip(t *testing.T, path string, files map[string]string) {
	t.Helper()

	f, err := os.Create(path)
	require.NoError(t, err)

	w := zip.NewWriter(f)

	for name, content := range files {
		fw, err := w.Create(name)
		require.NoError(t, err)

		_, err = fw.Write([]byte(content))
		require.NoError(t, err)
	}

	require.NoError(t, w.Close())
	require.NoError(t, f.Close())
}

// createTestZipWithBinary creates a zip file with binary content entries.
func createTestZipWithBinary(t *testing.T, path string, files map[string][]byte) {
	t.Helper()

	f, err := os.Create(path)
	require.NoError(t, err)

	w := zip.NewWriter(f)

	for name, content := range files {
		fw, err := w.Create(name)
		require.NoError(t, err)

		_, err = fw.Write(content)
		require.NoError(t, err)
	}

	require.NoError(t, w.Close())
	require.NoError(t, f.Close())
}

// createTestTarGz creates a tar.gz file with the given text files.
func createTestTarGz(t *testing.T, path string, files map[string]string) {
	t.Helper()

	f, err := os.Create(path)
	require.NoError(t, err)

	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}

		require.NoError(t, tw.WriteHeader(hdr))

		_, err = tw.Write([]byte(content))
		require.NoError(t, err)
	}

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	require.NoError(t, f.Close())
}

// createTestZipBytes creates a zip in memory and returns its bytes.
func createTestZipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer

	w := zip.NewWriter(&buf)

	for name, content := range files {
		fw, err := w.Create(name)
		require.NoError(t, err)

		_, err = fw.Write([]byte(content))
		require.NoError(t, err)
	}

	require.NoError(t, w.Close())

	return buf.Bytes()
}
