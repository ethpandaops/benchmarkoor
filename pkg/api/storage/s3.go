package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

// Compile-time interface check.
var _ Reader = (*s3Reader)(nil)

type s3Reader struct {
	client         *s3.Client
	bucket         string
	discoveryPaths []string
}

// NewS3Reader creates a Reader backed by S3-compatible storage.
func NewS3Reader(cfg *config.APIS3Config) Reader {
	client := newS3Client(cfg)

	paths := make([]string, 0, len(cfg.DiscoveryPaths))
	for _, p := range cfg.DiscoveryPaths {
		paths = append(paths, strings.TrimRight(p, "/"))
	}

	sort.Strings(paths)

	return &s3Reader{
		client:         client,
		bucket:         cfg.Bucket,
		discoveryPaths: paths,
	}
}

// DiscoveryPaths returns the configured S3 discovery paths.
func (r *s3Reader) DiscoveryPaths() []string {
	return r.discoveryPaths
}

// ListRunIDs lists run IDs (common prefixes) under {dp}/runs/.
func (r *s3Reader) ListRunIDs(
	ctx context.Context, discoveryPath string,
) ([]string, error) {
	prefix := discoveryPath + "/runs/"

	paginator := s3.NewListObjectsV2Paginator(
		r.client, &s3.ListObjectsV2Input{
			Bucket:    aws.String(r.bucket),
			Prefix:    aws.String(prefix),
			Delimiter: aws.String("/"),
		},
	)

	var ids []string

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf(
				"listing run prefixes under %q: %w", prefix, err,
			)
		}

		for _, cp := range page.CommonPrefixes {
			if cp.Prefix != nil {
				// Extract run ID: "dp/runs/abc123/" → "abc123"
				id := path.Base(strings.TrimRight(*cp.Prefix, "/"))
				ids = append(ids, id)
			}
		}
	}

	return ids, nil
}

// GetRunFile reads {dp}/runs/{runID}/{filename} from S3.
// Returns (nil, nil) when the key does not exist.
func (r *s3Reader) GetRunFile(
	ctx context.Context, discoveryPath, runID, filename string,
) ([]byte, error) {
	key := discoveryPath + "/runs/" + runID + "/" + filename

	return r.getObject(ctx, key)
}

// GetSuiteFile reads {dp}/suites/{suiteHash}/{filename} from S3.
// Returns (nil, nil) when the key does not exist.
func (r *s3Reader) GetSuiteFile(
	ctx context.Context, discoveryPath, suiteHash, filename string,
) ([]byte, error) {
	key := discoveryPath + "/suites/" + suiteHash + "/" + filename

	return r.getObject(ctx, key)
}

func (r *s3Reader) getObject(
	ctx context.Context, key string,
) ([]byte, error) {
	out, err := r.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(r.bucket),
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

func isS3NotFound(err error) bool {
	var nsk *s3types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}

	return strings.Contains(err.Error(), "NoSuchKey")
}

func newS3Client(cfg *config.APIS3Config) *s3.Client {
	opts := []func(*s3.Options){
		func(o *s3.Options) {
			if cfg.Region != "" {
				o.Region = cfg.Region
			} else {
				o.Region = "us-east-1"
			}

			if cfg.EndpointURL != "" {
				o.BaseEndpoint = aws.String(cfg.EndpointURL)
			}

			if cfg.ForcePathStyle {
				o.UsePathStyle = true
			}

			if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
				o.Credentials = credentials.NewStaticCredentialsProvider(
					cfg.AccessKeyID, cfg.SecretAccessKey, "",
				)
			}
		},
	}

	return s3.New(s3.Options{}, opts...)
}
