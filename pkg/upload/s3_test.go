package upload

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolvePrefix(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		baseName string
		want     string
	}{
		{
			name:     "default prefix",
			prefix:   "",
			baseName: "1769791126_8cec1fab_nethermind",
			want:     "results/runs/1769791126_8cec1fab_nethermind",
		},
		{
			name:     "custom prefix",
			prefix:   "my-project/benchmarks",
			baseName: "1769791126_8cec1fab_geth",
			want:     "my-project/benchmarks/runs/1769791126_8cec1fab_geth",
		},
		{
			name:     "trailing slash stripped",
			prefix:   "my-prefix/",
			baseName: "run123",
			want:     "my-prefix/runs/run123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &s3Uploader{
				cfg: &config.S3UploadConfig{Prefix: tt.prefix},
			}
			got := u.resolvePrefix(tt.baseName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDetectContentType(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantPrefix string
	}{
		{
			name:       "json file",
			path:       "results/config.json",
			wantPrefix: "application/json",
		},
		{
			name:       "no extension",
			path:       "results/Makefile",
			wantPrefix: "application/octet-stream",
		},
		{
			name:       "html file",
			path:       "results/index.html",
			wantPrefix: "text/html",
		},
		{
			name:       "txt file",
			path:       "results/notes.txt",
			wantPrefix: "text/plain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectContentType(tt.path)
			assert.Contains(t, got, tt.wantPrefix)
		})
	}
}

// fakeS3 speaks the subset of the S3 API the uploader touches and records
// which upload path each object took.
type fakeS3 struct {
	mu         sync.Mutex
	singlePuts []string
	multiparts []string
	parts      int
	// stored mimics bucket contents so ListObjectsV2 can report them back.
	stored    map[string]int64
	partBytes map[string]int64
}

func newFakeS3() *fakeS3 {
	return &fakeS3{
		stored:    make(map[string]int64),
		partBytes: make(map[string]int64),
	}
}

// store records an object under its bucket-relative key. Callers hold f.mu.
func (f *fakeS3) store(path string, size int64) {
	f.stored[strings.TrimPrefix(path, "/b/")] = size
}

func (f *fakeS3) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		key := r.URL.Path

		f.mu.Lock()
		defer f.mu.Unlock()

		switch {
		case r.Method == http.MethodGet && q.Get("list-type") == "2":
			w.Header().Set("Content-Type", "application/xml")

			body := `<ListBucketResult><IsTruncated>false</IsTruncated>`

			for k, size := range f.stored {
				if strings.HasPrefix(k, q.Get("prefix")) {
					body += `<Contents><Key>` + k +
						`</Key><Size>` + strconv.FormatInt(size, 10) + `</Size></Contents>`
				}
			}

			_, _ = w.Write([]byte(body + `</ListBucketResult>`))
		case r.Method == http.MethodPost && q.Has("uploads"):
			w.Header().Set("Content-Type", "application/xml")
			f.multiparts = append(f.multiparts, key)
			_, _ = w.Write([]byte(`<InitiateMultipartUploadResult>` +
				`<Bucket>b</Bucket><Key>` + key + `</Key><UploadId>up-1</UploadId>` +
				`</InitiateMultipartUploadResult>`))
		case r.Method == http.MethodPut && q.Has("partNumber"):
			n, _ := io.Copy(io.Discard, r.Body)
			f.parts++
			f.partBytes[key] += n
			w.Header().Set("ETag", `"etag"`)
		case r.Method == http.MethodPost && q.Has("uploadId"):
			w.Header().Set("Content-Type", "application/xml")

			f.store(key, f.partBytes[key])

			_, _ = w.Write([]byte(`<CompleteMultipartUploadResult>` +
				`<Bucket>b</Bucket><Key>` + key + `</Key><ETag>"etag"</ETag>` +
				`</CompleteMultipartUploadResult>`))
		case r.Method == http.MethodPut:
			n, _ := io.Copy(io.Discard, r.Body)
			f.singlePuts = append(f.singlePuts, key)
			f.store(key, n)
			w.Header().Set("ETag", `"etag"`)
		default:
			w.WriteHeader(http.StatusNotImplemented)
		}
	})
}

// A file larger than the part size must go out as a multipart upload. A bare
// PutObject caps at 5 GiB and fails with EntityTooLarge on the multi-GB
// pre-run bundles a stateful suite carries.
func TestUploadFileSplitsLargeFiles(t *testing.T) {
	fake := newFakeS3()
	srv := httptest.NewServer(fake.handler())

	defer srv.Close()

	uploader, err := NewS3Uploader(logrus.New(), &config.S3UploadConfig{
		Bucket:          "b",
		EndpointURL:     srv.URL,
		Region:          "us-east-1",
		AccessKeyID:     "id",
		SecretAccessKey: "secret",
		ForcePathStyle:  true,
		ParallelUploads: 1,
	})
	require.NoError(t, err)

	dir := t.TempDir()

	big := filepath.Join(dir, "big.bin")
	f, err := os.Create(big)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(uploadPartSize+1))
	require.NoError(t, f.Close())

	small := filepath.Join(dir, "small.bin")
	require.NoError(t, os.WriteFile(small, []byte("small"), 0o600))

	s3u, ok := uploader.(*s3Uploader)
	require.True(t, ok)

	require.NoError(t, s3u.uploadFile(t.Context(), big, "suites/h/big.bin"))
	require.NoError(t, s3u.uploadFile(t.Context(), small, "suites/h/small.bin"))

	assert.Equal(t, []string{"/b/suites/h/big.bin"}, fake.multiparts)
	assert.Equal(t, 2, fake.parts)
	assert.Equal(t, []string{"/b/suites/h/small.bin"}, fake.singlePuts)
}

// A suite directory is content-addressed by its hash, so re-uploading it on
// every run re-sends bytes the bucket already holds — for a stateful suite
// that is ~12 GB a run, most of it one pre-run bundle.
func TestUploadSuiteDirSkipsUnchangedObjects(t *testing.T) {
	fake := newFakeS3()
	srv := httptest.NewServer(fake.handler())

	defer srv.Close()

	uploader, err := NewS3Uploader(logrus.New(), &config.S3UploadConfig{
		Bucket:          "b",
		EndpointURL:     srv.URL,
		Region:          "us-east-1",
		AccessKeyID:     "id",
		SecretAccessKey: "secret",
		ForcePathStyle:  true,
		ParallelUploads: 4,
		Prefix:          "results",
	})
	require.NoError(t, err)

	dir := filepath.Join(t.TempDir(), "0d93b5bf3b970403")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "benchmark", "t1"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".eest-meta"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "benchmark", "t1", "test.request"), []byte("payload"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".eest-meta", "fixtures.ini"), []byte("meta"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "summary.json"), []byte(`{"hash":"x"}`), 0o600))

	require.NoError(t, uploader.UploadSuiteDir(t.Context(), dir))
	assert.Len(t, fake.singlePuts, 3, "first upload sends every file")

	fake.singlePuts = nil

	require.NoError(t, uploader.UploadSuiteDir(t.Context(), dir))

	// summary.json is rewritten every run — labels change without changing the
	// suite hash — so it alone is re-sent.
	assert.Equal(t, []string{"/b/results/suites/0d93b5bf3b970403/summary.json"}, fake.singlePuts)

	// A file whose size no longer matches is re-sent. Same size with different
	// bytes is deliberately not detected: a suite key is content-addressed, so
	// that cannot happen without the hash changing too.
	fake.singlePuts = nil
	require.NoError(t, os.WriteFile(filepath.Join(dir, "benchmark", "t1", "test.request"), []byte("longer payload"), 0o600))
	require.NoError(t, uploader.UploadSuiteDir(t.Context(), dir))
	assert.ElementsMatch(t, []string{
		"/b/results/suites/0d93b5bf3b970403/summary.json",
		"/b/results/suites/0d93b5bf3b970403/benchmark/t1/test.request",
	}, fake.singlePuts)
}
