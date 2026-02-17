package api

import (
	"context"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/sirupsen/logrus"
)

// presignCacheEntry holds a cached presigned URL and its expiration time.
type presignCacheEntry struct {
	url       string
	expiresAt time.Time
}

// s3Presigner generates presigned GET URLs for objects stored in S3.
type s3Presigner struct {
	log            logrus.FieldLogger
	cfg            *config.APIS3Config
	presignClient  *s3.PresignClient
	expiry         time.Duration
	discoveryPaths []string
	cacheTTL       time.Duration
	mu             sync.RWMutex
	cache          map[string]presignCacheEntry
}

// newS3Presigner creates a new S3 presigner from the given configuration.
func newS3Presigner(
	log logrus.FieldLogger,
	cfg *config.APIS3Config,
) (*s3Presigner, error) {
	expiry, err := time.ParseDuration(cfg.PresignedURLs.Expiry)
	if err != nil {
		return nil, fmt.Errorf("parsing presigned_urls.expiry: %w", err)
	}

	client := newAPIPresignS3Client(cfg)
	presignClient := s3.NewPresignClient(client)

	// Normalize discovery paths: trim trailing slashes.
	paths := make([]string, 0, len(cfg.DiscoveryPaths))
	for _, p := range cfg.DiscoveryPaths {
		paths = append(paths, strings.TrimRight(p, "/"))
	}

	return &s3Presigner{
		log:            log.WithField("component", "s3-presigner"),
		cfg:            cfg,
		presignClient:  presignClient,
		expiry:         expiry,
		discoveryPaths: paths,
		cacheTTL:       expiry / 2,
		cache:          make(map[string]presignCacheEntry),
	}, nil
}

// GeneratePresignedURL returns a presigned GET URL for the given S3 key.
// Results are cached for half the presigned URL expiry duration to avoid
// redundant presigning while ensuring URLs always have sufficient validity.
func (p *s3Presigner) GeneratePresignedURL(
	ctx context.Context,
	key string,
) (string, error) {
	if !p.isAllowedPath(key) {
		return "", fmt.Errorf("path %q is not within any allowed discovery path", key)
	}

	now := time.Now()

	// Fast path: check cache under read lock.
	p.mu.RLock()
	if entry, ok := p.cache[key]; ok && now.Before(entry.expiresAt) {
		p.mu.RUnlock()

		return entry.url, nil
	}
	p.mu.RUnlock()

	// Slow path: acquire write lock and double-check.
	p.mu.Lock()
	defer p.mu.Unlock()

	if entry, ok := p.cache[key]; ok && now.Before(entry.expiresAt) {
		return entry.url, nil
	}

	result, err := p.presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(p.cfg.Bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(p.expiry))
	if err != nil {
		return "", fmt.Errorf("presigning URL for %q: %w", key, err)
	}

	p.cache[key] = presignCacheEntry{
		url:       result.URL,
		expiresAt: now.Add(p.cacheTTL),
	}

	return result.URL, nil
}

// isAllowedPath checks that the key is clean and falls under a discovery path.
func (p *s3Presigner) isAllowedPath(key string) bool {
	if key == "" {
		return false
	}

	// Reject path traversal.
	if strings.Contains(key, "..") {
		return false
	}

	// Clean the path and ensure it didn't change meaning.
	cleaned := path.Clean(key)
	if cleaned != key {
		return false
	}

	// Must be under at least one discovery path prefix.
	for _, prefix := range p.discoveryPaths {
		if key == prefix || strings.HasPrefix(key, prefix+"/") {
			return true
		}
	}

	return false
}

// newAPIPresignS3Client constructs an S3 client from the API storage config.
func newAPIPresignS3Client(cfg *config.APIS3Config) *s3.Client {
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
