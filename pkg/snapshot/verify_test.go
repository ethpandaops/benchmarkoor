package snapshot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func randomBytes(n int) []byte {
	b := make([]byte, n)
	r := rand.NewChaCha8([32]byte{1})
	_, _ = r.Read(b)

	return b
}

func TestBlockHasher(t *testing.T) {
	data := randomBytes(10*1024 + 17)

	h := NewBlockHasher(1024)
	// Odd write sizes cross block boundaries mid-write.
	for rest := data; len(rest) > 0; {
		n := min(len(rest), 333)
		_, _ = h.Write(rest[:n])
		rest = rest[n:]
	}

	sums := h.Sums()
	require.Len(t, sums, 11, "ten full blocks plus a partial one")
	assert.Equal(t, int64(len(data)), h.Total())

	for i, s := range sums {
		end := min((i+1)*1024, len(data))
		want := sha256.Sum256(data[i*1024 : end])
		assert.Equal(t, hex.EncodeToString(want[:]), s, "block %d", i)
	}

	exact := NewBlockHasher(1024)
	_, _ = exact.Write(data[:2048])
	assert.Len(t, exact.Sums(), 2, "no empty trailing block when the stream ends on a boundary")
}

// memObject is a RangeReader over an in-memory object.
type memObject struct {
	data  []byte
	reads []int64
}

func (m *memObject) Size(context.Context) (int64, error) { return int64(len(m.data)), nil }

func (m *memObject) Range(_ context.Context, off, n int64) (io.ReadCloser, error) {
	m.reads = append(m.reads, off)

	return io.NopCloser(bytes.NewReader(m.data[off : off+n])), nil
}

func sumsOf(data []byte, block int64) []string {
	h := NewBlockHasher(block)
	_, _ = h.Write(data)

	return h.Sums()
}

func TestVerify(t *testing.T) {
	data := randomBytes(5000)
	sums := sumsOf(data, 1024)

	obj := &memObject{data: bytes.Clone(data)}
	require.NoError(t, Verify(context.Background(), obj, 5000, 1024, sums, 3))
	assert.Len(t, obj.reads, 3, "n random blocks")

	all := &memObject{data: bytes.Clone(data)}
	require.NoError(t, Verify(context.Background(), all, 5000, 1024, sums, 100))
	assert.ElementsMatch(t, []int64{0, 1024, 2048, 3072, 4096}, all.reads, "every block once, the short last one included")

	corrupt := &memObject{data: bytes.Clone(data)}
	corrupt.data[2048+5] ^= 0xff
	err := Verify(context.Background(), corrupt, 5000, 1024, sums, 100)
	require.ErrorContains(t, err, "block 2 (bytes 2048-3071)")

	err = Verify(context.Background(), &memObject{data: data[:4999]}, 5000, 1024, sums, 1)
	require.ErrorContains(t, err, "4999 bytes, uploaded 5000")
}

func TestHTTPRangeReader(t *testing.T) {
	data := randomBytes(5000)
	sums := sumsOf(data, 1024)

	ranged := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "snapshot.tar.zst", time.Time{}, bytes.NewReader(data))
	}))
	defer ranged.Close()

	require.NoError(t, Verify(context.Background(), HTTPRangeReader{URL: ranged.URL}, 5000, 1024, sums, 100))

	// A server that ignores Range answers with the whole object and a 200:
	// refused before anything is hashed.
	whole := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "5000")
		_, _ = w.Write(data)
	}))
	defer whole.Close()

	err := Verify(context.Background(), HTTPRangeReader{URL: whole.URL}, 5000, 1024, sums, 1)
	require.ErrorContains(t, err, "HTTP 200, want 206")

	missing := httptest.NewServer(http.NotFoundHandler())
	defer missing.Close()

	err = Verify(context.Background(), HTTPRangeReader{URL: missing.URL}, 5000, 1024, sums, 1)
	require.ErrorContains(t, err, "HTTP 404")
}
