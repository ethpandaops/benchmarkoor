package snapshot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// BlockSize is the span each block hash covers.
const BlockSize = 1 << 30

// BlockHasher is an io.Writer that records the sha256 of every blockSize
// bytes written to it, so an upload can later be checked range by range.
type BlockHasher struct {
	blockSize int64
	h         hash.Hash
	n         int64
	total     int64
	sums      []string
}

// NewBlockHasher returns a BlockHasher over blocks of blockSize bytes.
func NewBlockHasher(blockSize int64) *BlockHasher {
	return &BlockHasher{blockSize: blockSize, h: sha256.New()}
}

func (b *BlockHasher) Write(p []byte) (int, error) {
	written := len(p)
	for len(p) > 0 {
		take := min(int64(len(p)), b.blockSize-b.n)
		b.h.Write(p[:take])
		b.n += take
		b.total += take
		p = p[take:]

		if b.n == b.blockSize {
			b.flush()
		}
	}

	return written, nil
}

func (b *BlockHasher) flush() {
	b.sums = append(b.sums, hex.EncodeToString(b.h.Sum(nil)))
	b.h.Reset()
	b.n = 0
}

// Sums returns the block hashes, the last one covering any partial block.
// Call it once the stream is complete.
func (b *BlockHasher) Sums() []string {
	if b.n > 0 {
		b.flush()
	}

	return b.sums
}

// Total is the number of bytes hashed.
func (b *BlockHasher) Total() int64 { return b.total }

// RangeReader reads an uploaded object the way a verifier sees it.
type RangeReader interface {
	Size(ctx context.Context) (int64, error)
	Range(ctx context.Context, off, n int64) (io.ReadCloser, error)
}

// Verify checks an uploaded object against the stream that was sent: its size,
// then n distinct random blocks (all of them when n covers every block).
func Verify(ctx context.Context, r RangeReader, size, blockSize int64, sums []string, n int) error {
	got, err := r.Size(ctx)
	if err != nil {
		return err
	}

	if got != size {
		return fmt.Errorf("object is %d bytes, uploaded %d", got, size)
	}

	for _, i := range rand.Perm(len(sums))[:min(n, len(sums))] {
		off := int64(i) * blockSize
		length := min(blockSize, size-off)

		if err := verifyBlock(ctx, r, i, off, length, sums[i]); err != nil {
			return err
		}
	}

	return nil
}

func verifyBlock(ctx context.Context, r RangeReader, i int, off, length int64, want string) error {
	body, err := r.Range(ctx, off, length)
	if err != nil {
		return fmt.Errorf("block %d: %w", i, err)
	}
	defer func() { _ = body.Close() }()

	h := sha256.New()

	read, err := io.Copy(h, body)
	if err != nil {
		return fmt.Errorf("block %d: reading: %w", i, err)
	}

	if read != length {
		return fmt.Errorf("block %d: read %d bytes, want %d", i, read, length)
	}

	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("block %d (bytes %d-%d): sha256 %s, uploaded %s", i, off, off+length-1, got, want)
	}

	return nil
}

// HTTPRangeReader reads an object through a public URL, as consumers do.
// Every range must come back as 206: a 200 would mean the server ignored it.
type HTTPRangeReader struct {
	URL    string
	Client *http.Client
}

func (h HTTPRangeReader) do(ctx context.Context, method string, hdr map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, h.URL, nil)
	if err != nil {
		return nil, err
	}

	for k, v := range hdr {
		req.Header.Set(k, v)
	}

	c := h.Client
	if c == nil {
		c = http.DefaultClient
	}

	return c.Do(req)
}

// Size returns the object's Content-Length from a HEAD request.
func (h HTTPRangeReader) Size(ctx context.Context) (int64, error) {
	resp, err := h.do(ctx, http.MethodHead, nil)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HEAD %s: HTTP %d", h.URL, resp.StatusCode)
	}

	size, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("HEAD %s: no Content-Length", h.URL)
	}

	return size, nil
}

// Range returns n bytes from off.
func (h HTTPRangeReader) Range(ctx context.Context, off, n int64) (io.ReadCloser, error) {
	resp, err := h.do(ctx, http.MethodGet, map[string]string{"Range": byteRange(off, n)})
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusPartialContent {
		_ = resp.Body.Close()

		return nil, fmt.Errorf("GET %s %s: HTTP %d, want 206", h.URL, byteRange(off, n), resp.StatusCode)
	}

	return resp.Body, nil
}

// S3RangeReader reads an object with the bucket credentials.
type S3RangeReader struct {
	Client      *s3.Client
	Bucket, Key string
}

// Size returns the object's size.
func (s S3RangeReader) Size(ctx context.Context) (int64, error) {
	out, err := s.Client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(s.Key)})
	if err != nil {
		return 0, fmt.Errorf("HEAD s3://%s/%s: %w", s.Bucket, s.Key, err)
	}

	if out.ContentLength == nil {
		return 0, errors.New("HEAD returned no size")
	}

	return *out.ContentLength, nil
}

// Range returns n bytes from off.
func (s S3RangeReader) Range(ctx context.Context, off, n int64) (io.ReadCloser, error) {
	out, err := s.Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.Bucket), Key: aws.String(s.Key), Range: aws.String(byteRange(off, n)),
	})
	if err != nil {
		return nil, fmt.Errorf("GET s3://%s/%s %s: %w", s.Bucket, s.Key, byteRange(off, n), err)
	}

	return out.Body, nil
}

func byteRange(off, n int64) string {
	return fmt.Sprintf("bytes=%d-%d", off, off+n-1)
}
