package upload

import (
	"context"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

// uploadPartSize is the multipart chunk size. S3-compatible stores cap a
// single PutObject at 5 GiB and a multipart upload at 10000 parts, so 64 MiB
// parts carry any file the runner produces (640 GiB ceiling) — including the
// multi-GB pre-run bundles that a bare PutObject rejects with EntityTooLarge.
const uploadPartSize = 64 * 1024 * 1024

// uploadPartConcurrency is the per-file part concurrency. Files are already
// fanned out ParallelUploads-wide, so this stays low; it only matters for the
// few files big enough to be split at all.
const uploadPartConcurrency = 4

// s3Uploader implements Uploader for S3-compatible storage.
type s3Uploader struct {
	log    logrus.FieldLogger
	cfg    *config.S3UploadConfig
	client *s3.Client
	// The successor, feature/s3/transfermanager, is still a v0 preview; stay on
	// the stable manager until it reaches v1.
	uploader *manager.Uploader //nolint:staticcheck // SA1019: successor is pre-v1
}

// Ensure interface compliance.
var _ Uploader = (*s3Uploader)(nil)

// newS3Client constructs an S3 client from the given configuration.
func newS3Client(cfg *config.S3UploadConfig) *s3.Client {
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

// NewS3Uploader creates a new S3 uploader from the given configuration.
func NewS3Uploader(
	log logrus.FieldLogger,
	cfg *config.S3UploadConfig,
) (Uploader, error) {
	client := newS3Client(cfg)

	return &s3Uploader{
		log:    log.WithField("component", "s3-uploader"),
		cfg:    cfg,
		client: client,
		uploader: manager.NewUploader(client, func(u *manager.Uploader) { //nolint:staticcheck // SA1019: successor is pre-v1
			u.PartSize = uploadPartSize
			u.Concurrency = uploadPartConcurrency
		}),
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

// uploadJob describes a single file to upload.
type uploadJob struct {
	localPath string
	key       string
	size      int64
}

// Upload walks localDir and uploads all files to S3 under the configured prefix.
func (u *s3Uploader) Upload(ctx context.Context, localDir string) error {
	baseName := filepath.Base(localDir)
	prefix := u.resolvePrefix(baseName)

	jobs, err := u.collectJobs(localDir, prefix)
	if err != nil {
		return fmt.Errorf("walking directory %s: %w", localDir, err)
	}

	return u.uploadJobs(ctx, jobs, prefix)
}

// collectJobs walks a directory and builds the list of upload jobs.
func (u *s3Uploader) collectJobs(localDir, keyPrefix string) ([]uploadJob, error) {
	var jobs []uploadJob

	err := filepath.Walk(localDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if info.IsDir() {
			return nil
		}

		relPath, relErr := filepath.Rel(localDir, path)
		if relErr != nil {
			return fmt.Errorf("computing relative path: %w", relErr)
		}

		jobs = append(jobs, uploadJob{
			localPath: path,
			key:       keyPrefix + "/" + filepath.ToSlash(relPath),
			size:      info.Size(),
		})

		return nil
	})

	return jobs, err
}

// uploadJobs uploads a slice of jobs using a parallel worker pool with progress logging.
func (u *s3Uploader) uploadJobs(ctx context.Context, jobs []uploadJob, prefix string) error {
	total := len(jobs)
	if total == 0 {
		u.log.WithField("prefix", prefix).Info("No files to upload")

		return nil
	}

	u.log.WithFields(logrus.Fields{
		"files":            total,
		"bucket":           u.cfg.Bucket,
		"prefix":           prefix,
		"parallel_uploads": u.cfg.ParallelUploads,
	}).Info("Starting upload")

	// Upload phase: fan out to workers via a channel.
	var uploaded atomic.Int64

	ch := make(chan uploadJob, u.cfg.ParallelUploads)

	g, gCtx := errgroup.WithContext(ctx)

	for range u.cfg.ParallelUploads {
		g.Go(func() error {
			for job := range ch {
				if err := u.uploadFile(gCtx, job.localPath, job.key); err != nil {
					return fmt.Errorf("uploading %s: %w", job.key, err)
				}

				uploaded.Add(1)
			}

			return nil
		})
	}

	// Progress goroutine: log every 5 seconds.
	stopProgress := make(chan struct{})
	progressDone := make(chan struct{})

	go func() {
		defer close(progressDone)

		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				u.log.WithFields(logrus.Fields{
					"uploaded": uploaded.Load(),
					"total":    total,
				}).Info("Upload progress")
			case <-stopProgress:
				return
			}
		}
	}()

	// Feed jobs to workers.
	for _, job := range jobs {
		select {
		case ch <- job:
		case <-gCtx.Done():
		}

		if gCtx.Err() != nil {
			break
		}
	}

	close(ch)

	uploadErr := g.Wait()

	// Signal the progress goroutine to stop, then wait for it.
	close(stopProgress)
	<-progressDone

	if uploadErr != nil {
		return uploadErr
	}

	u.log.WithFields(logrus.Fields{
		"files":  total,
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

	// The manager sends anything under PartSize as a plain PutObject and splits
	// the rest into a multipart upload. Body is an *os.File, so parts are read
	// via io.ReaderAt section reads rather than buffered in memory.
	_, err = u.uploader.Upload(ctx, input) //nolint:staticcheck // SA1019: successor is pre-v1
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}

	return nil
}

// resolvePrefix builds the S3 key prefix for a run directory.
// The configured prefix is the base (default "results"), and runs are stored
// under prefix + "/runs/" + baseName.
func (u *s3Uploader) resolvePrefix(baseName string) string {
	prefix := u.cfg.Prefix
	if prefix == "" {
		prefix = "results"
	}

	return strings.TrimRight(prefix, "/") + "/runs/" + baseName
}

// UploadSuiteDir uploads all files in a suite directory to S3 under
// prefix + "/suites/" + dirname. Objects already there at the same size are
// skipped: a suite hash is a digest of its step file contents, so a given key
// under it always holds the same bytes and re-sending them is pure waste.
func (u *s3Uploader) UploadSuiteDir(ctx context.Context, localSuiteDir string) error {
	prefix := u.cfg.Prefix
	if prefix == "" {
		prefix = "results"
	}

	keyPrefix := strings.TrimRight(prefix, "/") + "/suites/" + filepath.Base(localSuiteDir)

	jobs, err := u.collectJobs(localSuiteDir, keyPrefix)
	if err != nil {
		return fmt.Errorf("walking suite directory %s: %w", localSuiteDir, err)
	}

	remote, err := u.listSizes(ctx, keyPrefix)
	if err != nil {
		// A listing failure only costs us the optimisation, so fall back to
		// uploading everything rather than failing the suite.
		u.log.WithError(err).WithField("prefix", keyPrefix).
			Warn("Failed to list existing suite objects; uploading all files")

		return u.uploadJobs(ctx, jobs, keyPrefix)
	}

	pending := make([]uploadJob, 0, len(jobs))

	var skipped, skippedBytes int64

	for _, job := range jobs {
		// summary.json is rewritten every run — metadata labels change without
		// affecting the suite hash — so it is the one file never skipped.
		if size, ok := remote[job.key]; ok && size == job.size &&
			filepath.Base(job.key) != suiteSummaryFile {
			skipped++
			skippedBytes += job.size

			continue
		}

		pending = append(pending, job)
	}

	if skipped > 0 {
		u.log.WithFields(logrus.Fields{
			"skipped":       skipped,
			"skipped_bytes": skippedBytes,
			"pending":       len(pending),
			"prefix":        keyPrefix,
		}).Info("Skipping suite objects already present at the same size")
	}

	return u.uploadJobs(ctx, pending, keyPrefix)
}

// suiteSummaryFile is the one file in a suite directory that is rewritten
// rather than content-addressed.
const suiteSummaryFile = "summary.json"

// listSizes returns the size of every object under prefix, keyed by full key.
func (u *s3Uploader) listSizes(
	ctx context.Context, prefix string,
) (map[string]int64, error) {
	sizes := make(map[string]int64)

	paginator := s3.NewListObjectsV2Paginator(u.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(u.cfg.Bucket),
		Prefix: aws.String(prefix + "/"),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing objects under %q: %w", prefix, err)
		}

		for _, obj := range page.Contents {
			if obj.Key == nil || obj.Size == nil {
				continue
			}

			sizes[*obj.Key] = *obj.Size
		}
	}

	return sizes, nil
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
