package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_EnvVarOverrides(t *testing.T) {
	// Create a minimal config file for testing.
	configContent := `
global:
  log_level: info
  docker_network: test-network
  client_logs_to_stdout: false
  cleanup_on_start: false
  directories:
    tmp_datadir: /tmp/original
    tmp_cachedir: /cache/original
benchmark:
  results_dir: ./original-results
  generate_results_index: false
  generate_suite_stats: false
  tests:
    filter: "original-filter"
client:
  config:
    jwt: original-jwt
    genesis:
      geth: http://example.com/genesis.json
  instances:
    - id: test-instance
      client: geth
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0o644))

	tests := []struct {
		name     string
		envVars  map[string]string
		validate func(t *testing.T, cfg *Config)
	}{
		{
			name:    "no env vars uses yaml values",
			envVars: map[string]string{},
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "info", cfg.Global.LogLevel)
				assert.Equal(t, "test-network", cfg.Global.DockerNetwork)
				assert.Equal(t, "./original-results", cfg.Benchmark.ResultsDir)
				assert.Equal(t, "original-jwt", cfg.Client.Config.JWT)
			},
		},
		{
			name: "string override - log_level",
			envVars: map[string]string{
				"BENCHMARKOOR_GLOBAL_LOG_LEVEL": "debug",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "debug", cfg.Global.LogLevel)
			},
		},
		{
			name: "string override - docker_network",
			envVars: map[string]string{
				"BENCHMARKOOR_GLOBAL_DOCKER_NETWORK": "custom-network",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "custom-network", cfg.Global.DockerNetwork)
			},
		},
		{
			name: "boolean override - cleanup_on_start true",
			envVars: map[string]string{
				"BENCHMARKOOR_GLOBAL_CLEANUP_ON_START": "true",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.True(t, cfg.Global.CleanupOnStart)
			},
		},
		{
			name: "boolean override - client_logs_to_stdout true",
			envVars: map[string]string{
				"BENCHMARKOOR_GLOBAL_CLIENT_LOGS_TO_STDOUT": "true",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.True(t, cfg.Global.ClientLogsToStdout)
			},
		},
		{
			name: "nested field override - directories.tmp_datadir",
			envVars: map[string]string{
				"BENCHMARKOOR_GLOBAL_DIRECTORIES_TMP_DATADIR": "/tmp/custom-datadir",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "/tmp/custom-datadir", cfg.Global.Directories.TmpDataDir)
			},
		},
		{
			name: "nested field override - directories.tmp_cachedir",
			envVars: map[string]string{
				"BENCHMARKOOR_GLOBAL_DIRECTORIES_TMP_CACHEDIR": "/cache/custom",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "/cache/custom", cfg.Global.Directories.TmpCacheDir)
			},
		},
		{
			name: "benchmark override - results_dir",
			envVars: map[string]string{
				"BENCHMARKOOR_BENCHMARK_RESULTS_DIR": "/tmp/test-results",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "/tmp/test-results", cfg.Benchmark.ResultsDir)
			},
		},
		{
			name: "benchmark override - tests.filter",
			envVars: map[string]string{
				"BENCHMARKOOR_BENCHMARK_TESTS_FILTER": "custom-filter",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "custom-filter", cfg.Benchmark.Tests.Filter)
			},
		},
		{
			name: "client override - config.jwt",
			envVars: map[string]string{
				"BENCHMARKOOR_CLIENT_CONFIG_JWT": "env-jwt-secret",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "env-jwt-secret", cfg.Client.Config.JWT)
			},
		},
		{
			name: "boolean override - generate_results_index",
			envVars: map[string]string{
				"BENCHMARKOOR_BENCHMARK_GENERATE_RESULTS_INDEX": "true",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.True(t, cfg.Benchmark.GenerateResultsIndex)
			},
		},
		{
			name: "boolean override - generate_suite_stats",
			envVars: map[string]string{
				"BENCHMARKOOR_BENCHMARK_GENERATE_SUITE_STATS": "true",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.True(t, cfg.Benchmark.GenerateSuiteStats)
			},
		},
		{
			name: "multiple overrides",
			envVars: map[string]string{
				"BENCHMARKOOR_GLOBAL_LOG_LEVEL":        "trace",
				"BENCHMARKOOR_GLOBAL_DOCKER_NETWORK":   "multi-network",
				"BENCHMARKOOR_BENCHMARK_RESULTS_DIR":   "/results/multi",
				"BENCHMARKOOR_GLOBAL_CLEANUP_ON_START": "true",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "trace", cfg.Global.LogLevel)
				assert.Equal(t, "multi-network", cfg.Global.DockerNetwork)
				assert.Equal(t, "/results/multi", cfg.Benchmark.ResultsDir)
				assert.True(t, cfg.Global.CleanupOnStart)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables.
			for key, value := range tt.envVars {
				t.Setenv(key, value)
			}

			cfg, err := Load(configPath)
			require.NoError(t, err)

			tt.validate(t, cfg)
		})
	}
}

func TestLoad_DefaultsAppliedWhenEmpty(t *testing.T) {
	// Create a minimal config with only required fields.
	configContent := `
client:
  config:
    genesis:
      geth: http://example.com/genesis.json
  instances:
    - id: test-instance
      client: geth
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0o644))

	cfg, err := Load(configPath)
	require.NoError(t, err)

	// Verify defaults are applied.
	assert.Equal(t, DefaultLogLevel, cfg.Global.LogLevel)
	assert.Equal(t, DefaultDockerNetwork, cfg.Global.DockerNetwork)
	assert.Equal(t, DefaultResultsDir, cfg.Benchmark.ResultsDir)
	assert.Equal(t, DefaultJWT, cfg.Client.Config.JWT)
	assert.Equal(t, DefaultPullPolicy, cfg.Client.Instances[0].PullPolicy)
}

func TestLoad_EnvVarOverridesDefaults(t *testing.T) {
	// Create a minimal config without log_level set.
	configContent := `
client:
  config:
    genesis:
      geth: http://example.com/genesis.json
  instances:
    - id: test-instance
      client: geth
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0o644))

	// Set env var to override the default.
	t.Setenv("BENCHMARKOOR_GLOBAL_LOG_LEVEL", "warn")

	cfg, err := Load(configPath)
	require.NoError(t, err)

	// Env var should take precedence over default.
	assert.Equal(t, "warn", cfg.Global.LogLevel)
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading config file")
}

func TestLoad_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("invalid: yaml: content:"), 0o644))

	_, err := Load(configPath)
	require.Error(t, err)
}

func TestSourceConfig_Validate(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name      string
		source    SourceConfig
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "no source configured is valid",
			source:  SourceConfig{},
			wantErr: false,
		},
		{
			name: "valid git source",
			source: SourceConfig{
				Git: &GitSourceV2{
					Repo:    "https://github.com/test/repo",
					Version: "v1.0.0",
				},
			},
			wantErr: false,
		},
		{
			name: "valid local source",
			source: SourceConfig{
				Local: &LocalSourceV2{
					BaseDir: tmpDir,
				},
			},
			wantErr: false,
		},
		{
			name: "valid eest_fixtures source",
			source: SourceConfig{
				EESTFixtures: &EESTFixturesSource{
					GitHubRepo:    "ethereum/execution-spec-tests",
					GitHubRelease: "benchmark@v0.0.6",
				},
			},
			wantErr: false,
		},
		{
			name: "multiple sources not allowed - git and local",
			source: SourceConfig{
				Git: &GitSourceV2{
					Repo:    "https://github.com/test/repo",
					Version: "v1.0.0",
				},
				Local: &LocalSourceV2{
					BaseDir: tmpDir,
				},
			},
			wantErr:   true,
			errSubstr: "cannot specify multiple sources",
		},
		{
			name: "multiple sources not allowed - git and eest",
			source: SourceConfig{
				Git: &GitSourceV2{
					Repo:    "https://github.com/test/repo",
					Version: "v1.0.0",
				},
				EESTFixtures: &EESTFixturesSource{
					GitHubRepo:    "ethereum/execution-spec-tests",
					GitHubRelease: "benchmark@v0.0.6",
				},
			},
			wantErr:   true,
			errSubstr: "cannot specify multiple sources",
		},
		{
			name: "eest_fixtures missing github_repo",
			source: SourceConfig{
				EESTFixtures: &EESTFixturesSource{
					GitHubRelease: "benchmark@v0.0.6",
				},
			},
			wantErr:   true,
			errSubstr: "eest_fixtures.github_repo is required",
		},
		{
			name: "eest_fixtures missing github_release",
			source: SourceConfig{
				EESTFixtures: &EESTFixturesSource{
					GitHubRepo: "ethereum/execution-spec-tests",
				},
			},
			wantErr:   true,
			errSubstr: "eest_fixtures.github_release is required",
		},
		{
			name: "git missing repo",
			source: SourceConfig{
				Git: &GitSourceV2{
					Version: "v1.0.0",
				},
			},
			wantErr:   true,
			errSubstr: "git.repo is required",
		},
		{
			name: "git missing version",
			source: SourceConfig{
				Git: &GitSourceV2{
					Repo: "https://github.com/test/repo",
				},
			},
			wantErr:   true,
			errSubstr: "git.version is required",
		},
		{
			name: "local missing base_dir",
			source: SourceConfig{
				Local: &LocalSourceV2{},
			},
			wantErr:   true,
			errSubstr: "local.base_dir is required",
		},
		{
			name: "local base_dir does not exist",
			source: SourceConfig{
				Local: &LocalSourceV2{
					BaseDir: "/nonexistent/path",
				},
			},
			wantErr:   true,
			errSubstr: "does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.source.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSourceConfig_IsConfigured(t *testing.T) {
	tests := []struct {
		name     string
		source   SourceConfig
		expected bool
	}{
		{
			name:     "no source",
			source:   SourceConfig{},
			expected: false,
		},
		{
			name: "git source",
			source: SourceConfig{
				Git: &GitSourceV2{Repo: "test", Version: "v1"},
			},
			expected: true,
		},
		{
			name: "local source",
			source: SourceConfig{
				Local: &LocalSourceV2{BaseDir: "/tmp"},
			},
			expected: true,
		},
		{
			name: "eest source",
			source: SourceConfig{
				EESTFixtures: &EESTFixturesSource{
					GitHubRepo:    "test/repo",
					GitHubRelease: "v1",
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.source.IsConfigured())
		})
	}
}
