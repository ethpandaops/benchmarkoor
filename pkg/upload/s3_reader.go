package upload

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/sirupsen/logrus"
)

// S3Reader reads objects from S3-compatible storage.
type S3Reader struct {
	log    logrus.FieldLogger
	cfg    *config.S3UploadConfig
	client *s3.Client
}

// NewS3Reader creates a new S3Reader from the given configuration.
func NewS3Reader(
	log logrus.FieldLogger,
	cfg *config.S3UploadConfig,
) *S3Reader {
	return &S3Reader{
		log:    log.WithField("component", "s3-reader"),
		cfg:    cfg,
		client: newS3Client(cfg),
	}
}

// ListPrefixes lists immediate "subdirectory" prefixes under the given prefix.
// The prefix should end with "/" (e.g. "results/runs/").
func (r *S3Reader) ListPrefixes(
	ctx context.Context, prefix string,
) ([]string, error) {
	var prefixes []string

	paginator := s3.NewListObjectsV2Paginator(r.client, &s3.ListObjectsV2Input{
		Bucket:    aws.String(r.cfg.Bucket),
		Prefix:    aws.String(prefix),
		Delimiter: aws.String("/"),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing prefixes under %q: %w", prefix, err)
		}

		for _, cp := range page.CommonPrefixes {
			if cp.Prefix != nil {
				prefixes = append(prefixes, *cp.Prefix)
			}
		}
	}

	return prefixes, nil
}

// GetObject returns the contents of the given key.
// If the key does not exist, it returns (nil, nil).
func (r *S3Reader) GetObject(
	ctx context.Context, key string,
) ([]byte, error) {
	out, err := r.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(r.cfg.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isS3NotFound(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("getting object %q: %w", key, err)
	}

	defer func() { _ = out.Body.Close() }()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("reading object %q: %w", key, err)
	}

	return data, nil
}

// PutObject writes data to the given key with the specified content type.
func (r *S3Reader) PutObject(
	ctx context.Context, key string, data []byte, contentType string,
) error {
	_, err := r.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(r.cfg.Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("putting object %q: %w", key, err)
	}

	return nil
}

// isS3NotFound returns true if the error indicates the object does not exist.
func isS3NotFound(err error) bool {
	var nsk *s3types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}

	// Some S3-compatible implementations return a generic error with
	// "NoSuchKey" in the message rather than the typed error.
	return strings.Contains(err.Error(), "NoSuchKey")
}
