package upload

import (
	"context"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/sirupsen/logrus"
)

// s3Uploader implements Uploader for S3-compatible storage.
type s3Uploader struct {
	log    logrus.FieldLogger
	cfg    *config.S3UploadConfig
	client *s3.Client
}

// Ensure interface compliance.
var _ Uploader = (*s3Uploader)(nil)

// NewS3Uploader creates a new S3 uploader from the given configuration.
func NewS3Uploader(
	log logrus.FieldLogger,
	cfg *config.S3UploadConfig,
) (Uploader, error) {
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

	client := s3.New(s3.Options{}, opts...)

	return &s3Uploader{
		log:    log.WithField("component", "s3-uploader"),
		cfg:    cfg,
		client: client,
	}, nil
}

// Preflight verifies S3 connectivity by writing a small test object.
func (u *s3Uploader) Preflight(ctx context.Context) error {
	content := fmt.Sprintf("benchmarkoor write test: %s", time.Now().UTC().Format(time.RFC3339))
	body := strings.NewReader(content)

	_, err := u.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(u.cfg.Bucket),
		Key:         aws.String(".benchmarkoor-write-test"),
		Body:        body,
		ContentType: aws.String("text/plain"),
	})
	if err != nil {
		return fmt.Errorf("writing test object to s3://%s: %w", u.cfg.Bucket, err)
	}

	return nil
}

// Upload walks localDir and uploads all files to S3 under the configured prefix.
func (u *s3Uploader) Upload(ctx context.Context, localDir string) error {
	baseName := filepath.Base(localDir)
	prefix := u.resolvePrefix(baseName)

	var count int

	err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(localDir, path)
		if err != nil {
			return fmt.Errorf("computing relative path: %w", err)
		}

		key := prefix + "/" + filepath.ToSlash(relPath)

		if err := u.uploadFile(ctx, path, key); err != nil {
			return fmt.Errorf("uploading %s: %w", relPath, err)
		}

		count++

		return nil
	})
	if err != nil {
		return fmt.Errorf("walking directory %s: %w", localDir, err)
	}

	u.log.WithFields(logrus.Fields{
		"files":  count,
		"bucket": u.cfg.Bucket,
		"prefix": prefix,
	}).Info("Upload completed")

	return nil
}

// uploadFile uploads a single file to S3.
func (u *s3Uploader) uploadFile(ctx context.Context, localPath, key string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("opening file: %w", err)
	}
	defer func() { _ = f.Close() }()

	input := &s3.PutObjectInput{
		Bucket:      aws.String(u.cfg.Bucket),
		Key:         aws.String(key),
		Body:        f,
		ContentType: aws.String(detectContentType(localPath)),
	}

	if u.cfg.StorageClass != "" {
		input.StorageClass = s3types.StorageClass(u.cfg.StorageClass)
	}

	if u.cfg.ACL != "" {
		input.ACL = s3types.ObjectCannedACL(u.cfg.ACL)
	}

	u.log.WithFields(logrus.Fields{
		"key":    key,
		"bucket": u.cfg.Bucket,
	}).Debug("Uploading file")

	_, err = u.client.PutObject(ctx, input)
	if err != nil {
		return fmt.Errorf("PutObject: %w", err)
	}

	return nil
}

// resolvePrefix builds the S3 key prefix for a run directory.
func (u *s3Uploader) resolvePrefix(baseName string) string {
	prefix := u.cfg.Prefix
	if prefix == "" {
		prefix = "results/runs"
	}

	return strings.TrimRight(prefix, "/") + "/" + baseName
}

// detectContentType returns a MIME type based on file extension.
func detectContentType(path string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return "application/octet-stream"
	}

	ct := mime.TypeByExtension(ext)
	if ct == "" {
		return "application/octet-stream"
	}

	return ct
}
