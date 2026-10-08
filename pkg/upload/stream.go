package upload

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

const (
	// maxUploadParts is S3's part limit, minus headroom for an estimate that
	// runs short.
	maxUploadParts = 9000

	// maxPartSize is S3's largest allowed part.
	maxPartSize = 5 << 30

	mib = 1 << 20
)

// NewS3Client returns the S3 client every uploader in this package uses.
func NewS3Client(cfg *config.S3UploadConfig) *s3.Client {
	return newS3Client(cfg)
}

// StreamPartSize returns the multipart part size for a stream of at most
// maxBytes: uploadPartSize, grown in whole MiB until the stream fits in
// maxUploadParts parts. A 1.5 TB stream gets ~160 MiB parts.
func StreamPartSize(maxBytes int64) (int64, error) {
	size := max(int64(uploadPartSize), (maxBytes/maxUploadParts/mib+1)*mib)
	if size > maxPartSize {
		return 0, fmt.Errorf("%d bytes need %d-byte parts, over S3's 5 GiB part limit", maxBytes, size)
	}

	return size, nil
}

// UploadStream uploads r, of unknown length, to bucket/key as a multipart
// upload. Memory is bounded by partSize * concurrency.
func UploadStream(
	ctx context.Context,
	client *s3.Client,
	bucket, key string,
	r io.Reader,
	partSize int64,
	concurrency int,
) error {
	u := manager.NewUploader(client, func(u *manager.Uploader) { //nolint:staticcheck // SA1019: successor is pre-v1
		u.PartSize = partSize
		u.Concurrency = concurrency
	})

	_, err := u.Upload(ctx, &s3.PutObjectInput{ //nolint:staticcheck // SA1019: successor is pre-v1
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        r,
		ContentType: aws.String("application/octet-stream"),
	})
	if err != nil {
		return fmt.Errorf("uploading s3://%s/%s: %w", bucket, key, err)
	}

	return nil
}
