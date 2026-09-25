package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

// minimalAPIConfig is an API server with no indexing section.
const minimalAPIConfig = `
global:
  log_level: info
api:
  server:
    listen: ":9090"
  database:
    driver: sqlite
    sqlite:
      path: /tmp/benchmarkoor-test.db
`

// indexingSection turns indexing on with the storage backend it requires.
const indexingSection = `
  indexing:
    enabled: true
    database:
      driver: sqlite
      sqlite:
        path: /tmp/benchmarkoor-test-index.db
  storage:
    local:
      enabled: true
      discovery_paths:
        results: /tmp
`

func writeConfig(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	return path
}

// TestAPIIndexingConfig_GracePeriod covers the grace period falling back when
// nothing usable sets it, including on a nil section.
func TestAPIIndexingConfig_GracePeriod(t *testing.T) {
	var absent *config.APIIndexingConfig

	assert.Equal(t, 6*time.Hour, absent.GetFailureGracePeriod())

	tests := []struct {
		name  string
		cfg   config.APIIndexingConfig
		grace time.Duration
	}{
		{name: "unset", cfg: config.APIIndexingConfig{}, grace: 6 * time.Hour},
		{
			name:  "set",
			cfg:   config.APIIndexingConfig{FailureGracePeriod: "90m"},
			grace: 90 * time.Minute,
		},
		{
			name:  "malformed falls back",
			cfg:   config.APIIndexingConfig{FailureGracePeriod: "not-a-duration"},
			grace: 6 * time.Hour,
		},
		{
			name:  "zero falls back",
			cfg:   config.APIIndexingConfig{FailureGracePeriod: "0s"},
			grace: 6 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.grace, tt.cfg.GetFailureGracePeriod())
		})
	}
}

// TestAPIIndexingConfig_EnvOverrides covers the documented environment
// variables. The guard that matters is the first case: binding these keys must
// not conjure an indexing section into a config that has none, which would
// turn the indexer on where nobody asked for it.
func TestAPIIndexingConfig_EnvOverrides(t *testing.T) {
	t.Run("absent section stays absent", func(t *testing.T) {
		cfg, err := config.Load(writeConfig(t, minimalAPIConfig))
		require.NoError(t, err)
		assert.Nil(t, cfg.API.Indexing)
	})

	t.Run("env overrides the grace period", func(t *testing.T) {
		t.Setenv("BENCHMARKOOR_API_INDEXING_FAILURE_GRACE_PERIOD", "2h")

		cfg, err := config.Load(
			writeConfig(t, minimalAPIConfig+indexingSection),
		)
		require.NoError(t, err)
		require.NotNil(t, cfg.API.Indexing)
		assert.Equal(t, 2*time.Hour, cfg.API.Indexing.GetFailureGracePeriod())
	})
}
