package snapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/ethpandaops/benchmarkoor/pkg/upload"
	"github.com/sirupsen/logrus"
)

// DefaultUploadConcurrency is the number of parts in flight; memory is this
// times the part size.
const DefaultUploadConcurrency = 8

// ExportOptions describes one snapshot export.
type ExportOptions struct {
	Layout
	Bucket  string
	Datadir string
	// HeadBlock is the eth_getBlockByNumber JSON-RPC response (full
	// transactions) for Layout.Block, published as-is.
	HeadBlock []byte
	// Metadata is merged over the generated _snapshot_metadata.json.
	Metadata map[string]any
	// WriteLatest points <network>/<client>/latest at Block, after everything
	// else is uploaded and verified.
	WriteLatest bool
	// VerifyPublicBase, when set, is the public URL of the bucket root, and
	// verification reads through it as consumers do; otherwise through S3.
	VerifyPublicBase string
	VerifyBlocks     int
	ZstdLevel        int

	// PartSize overrides upload.StreamPartSize; BlockSize overrides BlockSize.
	// Both exist for tests.
	PartSize    int64
	BlockSize   int64
	Concurrency int
}

// Validate checks the layout and the options Export does not default.
func (o ExportOptions) Validate() error {
	if err := o.Layout.Validate(); err != nil {
		return err
	}

	if o.VerifyBlocks < 0 {
		return fmt.Errorf("verify blocks must be >= 0, got %d", o.VerifyBlocks)
	}

	return nil
}

// Result reports what was published.
type Result struct {
	ArchiveKey   string
	ArchiveBytes int64
	DataBytes    int64
	BlockSums    []string
}

// Export packs a stopped datadir, streams it to the bucket while hashing every
// block, verifies the upload, then writes the head block and metadata, and
// finally latest. Nothing is written to disk.
func Export(ctx context.Context, log logrus.FieldLogger, client *s3.Client, o ExportOptions) (*Result, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}

	if err := checkHeadBlock(o.HeadBlock, o.Block); err != nil {
		return nil, err
	}

	m, err := BuildManifest(o.Client, o.Datadir)
	if err != nil {
		return nil, err
	}

	partSize := o.PartSize
	if partSize == 0 {
		if partSize, err = upload.StreamPartSize(m.MaxArchiveBytes()); err != nil {
			return nil, err
		}
	}

	blockSize := o.BlockSize
	if blockSize == 0 {
		blockSize = BlockSize
	}

	concurrency := o.Concurrency
	if concurrency == 0 {
		concurrency = DefaultUploadConcurrency
	}

	key := o.ArchiveKey()
	log = log.WithFields(logrus.Fields{"bucket": o.Bucket, "key": key})
	log.WithFields(logrus.Fields{
		"members": len(m.Members), "data_bytes": m.Bytes, "part_size": partSize,
	}).Info("Streaming datadir")

	packCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream, wait, err := Pack(packCtx, m, o.ZstdLevel)
	if err != nil {
		return nil, err
	}

	body := &checkedReader{r: stream, wait: wait}
	hasher := NewBlockHasher(blockSize)

	if err := upload.UploadStream(ctx, client, o.Bucket, key, io.TeeReader(body, hasher), partSize, concurrency); err != nil {
		cancel()

		if !body.done {
			_ = wait()
		}

		return nil, err
	}

	res := &Result{ArchiveKey: key, ArchiveBytes: hasher.Total(), DataBytes: m.Bytes, BlockSums: hasher.Sums()}
	log.WithField("bytes", res.ArchiveBytes).Info("Uploaded; verifying")

	var reader RangeReader = S3RangeReader{Client: client, Bucket: o.Bucket, Key: key}
	if o.VerifyPublicBase != "" {
		reader = HTTPRangeReader{URL: strings.TrimSuffix(o.VerifyPublicBase, "/") + "/" + key}
	}

	if err := Verify(ctx, reader, res.ArchiveBytes, blockSize, res.BlockSums, o.VerifyBlocks); err != nil {
		return nil, fmt.Errorf("verifying %s: %w", key, err)
	}

	metadata, err := buildMetadata(m.Bytes, o.Metadata)
	if err != nil {
		return nil, err
	}

	objects := []struct {
		key  string
		body []byte
	}{
		{o.HeadBlockKey(), o.HeadBlock},
		{o.MetadataKey(), metadata},
	}
	if o.WriteLatest {
		objects = append(objects, struct {
			key  string
			body []byte
		}{o.LatestKey(), []byte(strconv.FormatUint(o.Block, 10) + "\n")})
	}

	for _, obj := range objects {
		if err := put(ctx, client, o.Bucket, obj.key, obj.body); err != nil {
			return nil, err
		}

		log.WithField("key", obj.key).Info("Uploaded")
	}

	return res, nil
}

func put(ctx context.Context, client *s3.Client, bucket, key string, body []byte) error {
	contentType := "application/json"
	if !strings.HasSuffix(key, ".json") {
		contentType = "text/plain"
	}

	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key), Body: bytes.NewReader(body), ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("uploading s3://%s/%s: %w", bucket, key, err)
	}

	return nil
}

// checkHeadBlock requires an eth_getBlockByNumber response for block, as
// snapshots.ethpandaops.io publishes it.
func checkHeadBlock(raw []byte, block uint64) error {
	var resp struct {
		Result *struct {
			Number string `json:"number"`
			Hash   string `json:"hash"`
		} `json:"result"`
	}

	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("head block: %w", err)
	}

	if resp.Result == nil || resp.Result.Hash == "" {
		return fmt.Errorf("head block: not an eth_getBlockByNumber response (no result.hash)")
	}

	n, err := strconv.ParseUint(strings.TrimPrefix(resp.Result.Number, "0x"), 16, 64)
	if err != nil {
		return fmt.Errorf("head block: result.number %q: %w", resp.Result.Number, err)
	}

	if n != block {
		return fmt.Errorf("head block is %d, exporting block %d", n, block)
	}

	return nil
}

// buildMetadata returns {"data_size_bytes": dataBytes} with extra merged over
// it, objects recursively.
func buildMetadata(dataBytes int64, extra map[string]any) ([]byte, error) {
	md := merge(map[string]any{"data_size_bytes": dataBytes}, extra)

	b, err := json.MarshalIndent(md, "", "  ")
	if err != nil {
		return nil, err
	}

	return append(b, '\n'), nil
}

func merge(dst, src map[string]any) map[string]any {
	for k, v := range src {
		sub, ok := v.(map[string]any)
		if cur, isMap := dst[k].(map[string]any); ok && isMap {
			dst[k] = merge(cur, sub)

			continue
		}

		dst[k] = v
	}

	return dst
}

// checkedReader ends a stream with the producer's exit status: a failed tar
// or zstd surfaces as a read error, so the multipart upload is aborted rather
// than completed with a truncated archive.
type checkedReader struct {
	r    io.Reader
	wait func() error
	done bool
	err  error
}

func (c *checkedReader) Read(p []byte) (int, error) {
	if c.done {
		return 0, c.err
	}

	n, err := c.r.Read(p)
	if err == io.EOF {
		c.done = true
		if c.err = c.wait(); c.err != nil {
			c.err = fmt.Errorf("packing: %w", c.err)
		} else {
			c.err = io.EOF
		}

		return n, c.err
	}

	return n, err
}
