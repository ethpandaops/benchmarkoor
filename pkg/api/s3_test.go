package api

import (
	"context"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestS3Presigner_IsAllowedPath(t *testing.T) {
	log := logrus.New()

	presigner, err := newS3Presigner(log, &config.APIS3Config{
		Enabled:        true,
		Bucket:         "test-bucket",
		Region:         "us-east-1",
		DiscoveryPaths: []string{"results", "archive/2024"},
		PresignedURLs: config.APIS3PresignedURLConfig{
			Expiry: "1h",
		},
	})
	require.NoError(t, err)

	tests := []struct {
		name    string
		key     string
		allowed bool
	}{
		{
			name:    "exact discovery path match",
			key:     "results",
			allowed: true,
		},
		{
			name:    "nested file under discovery path",
			key:     "results/index.json",
			allowed: true,
		},
		{
			name:    "deeply nested file under discovery path",
			key:     "results/runs/2024-01-01/run.json",
			allowed: true,
		},
		{
			name:    "nested discovery path exact match",
			key:     "archive/2024",
			allowed: true,
		},
		{
			name:    "nested file under nested discovery path",
			key:     "archive/2024/data.json",
			allowed: true,
		},
		{
			name:    "different prefix not allowed",
			key:     "other/file.json",
			allowed: false,
		},
		{
			name:    "path traversal rejected",
			key:     "results/../secrets/key",
			allowed: false,
		},
		{
			name:    "double dot in middle rejected",
			key:     "results/..hidden/file",
			allowed: false,
		},
		{
			name:    "empty path rejected",
			key:     "",
			allowed: false,
		},
		{
			name:    "partial prefix match rejected",
			key:     "results_backup/file.json",
			allowed: false,
		},
		{
			name:    "prefix without slash rejected",
			key:     "resultsx/file.json",
			allowed: false,
		},
		{
			name:    "sibling of nested discovery path rejected",
			key:     "archive/2023/data.json",
			allowed: false,
		},
		{
			name:    "parent of nested discovery path rejected",
			key:     "archive/secret.json",
			allowed: false,
		},
		{
			name:    "trailing slash makes path unclean",
			key:     "results/",
			allowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.allowed, presigner.isAllowedPath(tt.key))
		})
	}
}

func TestS3Presigner_CachesURLs(t *testing.T) {
	log := logrus.New()

	// Use a MinIO-style endpoint so presigning works without real AWS creds.
	presigner, err := newS3Presigner(log, &config.APIS3Config{
		Enabled:         true,
		Bucket:          "test-bucket",
		Region:          "us-east-1",
		EndpointURL:     "http://localhost:9000",
		ForcePathStyle:  true,
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		DiscoveryPaths:  []string{"results"},
		PresignedURLs: config.APIS3PresignedURLConfig{
			Expiry: "1h",
		},
	})
	require.NoError(t, err)

	ctx := context.Background()

	// First call generates a fresh presigned URL.
	url1, err := presigner.GeneratePresignedURL(ctx, "results/index.json")
	require.NoError(t, err)
	assert.NotEmpty(t, url1)

	// Second call for the same key should return the cached URL (identical).
	url2, err := presigner.GeneratePresignedURL(ctx, "results/index.json")
	require.NoError(t, err)
	assert.Equal(t, url1, url2, "expected cached URL to be identical")

	// A different key should produce a different URL.
	url3, err := presigner.GeneratePresignedURL(ctx, "results/other.json")
	require.NoError(t, err)
	assert.NotEqual(t, url1, url3,
		"expected different key to produce different URL")
}
