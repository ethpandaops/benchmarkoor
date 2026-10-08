package upload

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamPartSize(t *testing.T) {
	for _, n := range []int64{0, 1 << 30, 500 << 30, 1_500_000_000_000, 3 << 40, 40 << 40} {
		size, err := StreamPartSize(n)
		require.NoError(t, err, n)
		assert.GreaterOrEqual(t, size, int64(uploadPartSize), n)
		assert.Zero(t, size%mib, "whole MiB")
		assert.LessOrEqual(t, (n+size-1)/size, int64(maxUploadParts), "%d bytes fit the part limit", n)
	}

	small, _ := StreamPartSize(1 << 30)
	assert.Equal(t, int64(uploadPartSize), small, "small streams keep the existing part size")

	big, _ := StreamPartSize(1_500_000_000_000)
	assert.Greater(t, big, int64(uploadPartSize), "a 1.5 TB stream outgrows 64 MiB parts")

	_, err := StreamPartSize(50 << 40)
	require.ErrorContains(t, err, "5 GiB part limit")
}

// A Ctrl-C cancels the upload's context; the multipart upload must still be
// aborted, on a context that outlives it.
func TestUploadStream_CanceledAbortsUpload(t *testing.T) {
	var (
		mu      sync.Mutex
		aborted []string
	)

	ctx, cancel := context.WithCancel(context.Background())
	firstPart := sync.OnceFunc(cancel)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		q := r.URL.Query()

		switch {
		case r.Method == http.MethodPost && q.Has("uploads"):
			_, _ = io.WriteString(w, `<InitiateMultipartUploadResult><Bucket>b</Bucket><Key>k</Key><UploadId>up-1</UploadId></InitiateMultipartUploadResult>`)
		case r.Method == http.MethodPut && q.Has("partNumber"):
			firstPart()
			w.Header().Set("ETag", `"e"`)
		case r.Method == http.MethodDelete && q.Has("uploadId"):
			mu.Lock()
			aborted = append(aborted, q.Get("uploadId"))
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, r.Method+" "+r.URL.String(), http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)

	client := NewS3Client(&config.S3UploadConfig{
		EndpointURL: srv.URL, ForcePathStyle: true, AccessKeyID: "a", SecretAccessKey: "s",
	})

	// Endless body: only the cancel can end the upload.
	err := UploadStream(ctx, client, "b", "k", infinite{}, 5*mib, 1)
	require.Error(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, aborted, "up-1")
}

type infinite struct{}

func (infinite) Read(p []byte) (int, error) { return len(p), nil }
