package config

import (
	"archive/tar"
	"compress/gzip"
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
runner:
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
				assert.Equal(t, "test-network", cfg.Runner.DockerNetwork)
				assert.Equal(t, "./original-results", cfg.Runner.Benchmark.ResultsDir)
				assert.Equal(t, "original-jwt", cfg.Runner.Client.Config.JWT)
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
				"BENCHMARKOOR_RUNNER_DOCKER_NETWORK": "custom-network",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "custom-network", cfg.Runner.DockerNetwork)
			},
		},
		{
			name: "boolean override - cleanup_on_start true",
			envVars: map[string]string{
				"BENCHMARKOOR_RUNNER_CLEANUP_ON_START": "true",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.True(t, cfg.Runner.CleanupOnStart)
			},
		},
		{
			name: "boolean override - client_logs_to_stdout true",
			envVars: map[string]string{
				"BENCHMARKOOR_RUNNER_CLIENT_LOGS_TO_STDOUT": "true",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.True(t, cfg.Runner.ClientLogsToStdout)
			},
		},
		{
			name: "nested field override - directories.tmp_datadir",
			envVars: map[string]string{
				"BENCHMARKOOR_RUNNER_DIRECTORIES_TMP_DATADIR": "/tmp/custom-datadir",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "/tmp/custom-datadir", cfg.Runner.Directories.TmpDataDir)
			},
		},
		{
			name: "nested field override - directories.tmp_cachedir",
			envVars: map[string]string{
				"BENCHMARKOOR_RUNNER_DIRECTORIES_TMP_CACHEDIR": "/cache/custom",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "/cache/custom", cfg.Runner.Directories.TmpCacheDir)
			},
		},
		{
			name: "benchmark override - results_dir",
			envVars: map[string]string{
				"BENCHMARKOOR_RUNNER_BENCHMARK_RESULTS_DIR": "/tmp/test-results",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "/tmp/test-results", cfg.Runner.Benchmark.ResultsDir)
			},
		},
		{
			name: "benchmark override - tests.filter",
			envVars: map[string]string{
				"BENCHMARKOOR_RUNNER_BENCHMARK_TESTS_FILTER": "custom-filter",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "custom-filter", cfg.Runner.Benchmark.Tests.Filter)
			},
		},
		{
			name: "client override - config.jwt",
			envVars: map[string]string{
				"BENCHMARKOOR_RUNNER_CLIENT_CONFIG_JWT": "env-jwt-secret",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "env-jwt-secret", cfg.Runner.Client.Config.JWT)
			},
		},
		{
			name: "boolean override - generate_results_index",
			envVars: map[string]string{
				"BENCHMARKOOR_RUNNER_BENCHMARK_GENERATE_RESULTS_INDEX": "true",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.True(t, cfg.Runner.Benchmark.GenerateResultsIndex)
			},
		},
		{
			name: "boolean override - generate_suite_stats",
			envVars: map[string]string{
				"BENCHMARKOOR_RUNNER_BENCHMARK_GENERATE_SUITE_STATS": "true",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.True(t, cfg.Runner.Benchmark.GenerateSuiteStats)
			},
		},
		{
			name: "multiple overrides",
			envVars: map[string]string{
				"BENCHMARKOOR_GLOBAL_LOG_LEVEL":             "trace",
				"BENCHMARKOOR_RUNNER_DOCKER_NETWORK":        "multi-network",
				"BENCHMARKOOR_RUNNER_BENCHMARK_RESULTS_DIR": "/results/multi",
				"BENCHMARKOOR_RUNNER_CLEANUP_ON_START":      "true",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "trace", cfg.Global.LogLevel)
				assert.Equal(t, "multi-network", cfg.Runner.DockerNetwork)
				assert.Equal(t, "/results/multi", cfg.Runner.Benchmark.ResultsDir)
				assert.True(t, cfg.Runner.CleanupOnStart)
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

func TestExpandEnvWithDefaults(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		envVars  map[string]string
		expected string
	}{
		{
			name:     "default used when var is unset",
			input:    "${TEST_EXPAND_UNSET:-fallback}",
			expected: "fallback",
		},
		{
			name:     "default used when var is empty",
			input:    "${TEST_EXPAND_EMPTY:-fallback}",
			envVars:  map[string]string{"TEST_EXPAND_EMPTY": ""},
			expected: "fallback",
		},
		{
			name:     "var value used when set",
			input:    "${TEST_EXPAND_SET:-fallback}",
			envVars:  map[string]string{"TEST_EXPAND_SET": "actual"},
			expected: "actual",
		},
		{
			name:     "plain var returns empty when unset",
			input:    "${TEST_EXPAND_PLAIN_UNSET}",
			expected: "",
		},
		{
			name:     "plain var returns value when set",
			input:    "${TEST_EXPAND_PLAIN_SET}",
			envVars:  map[string]string{"TEST_EXPAND_PLAIN_SET": "hello"},
			expected: "hello",
		},
		{
			name:     "default containing colons",
			input:    "${TEST_EXPAND_URL:-http://localhost:8080}",
			expected: "http://localhost:8080",
		},
		{
			name:     "multiple expansions in one string",
			input:    "${TEST_EXPAND_A:-alpha}_${TEST_EXPAND_B:-beta}",
			envVars:  map[string]string{"TEST_EXPAND_A": "one"},
			expected: "one_beta",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			result := os.Expand(tt.input, expandEnvWithDefaults)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLoad_DefaultsAppliedWhenEmpty(t *testing.T) {
	// Create a minimal config with only required fields.
	configContent := `
runner:
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
	assert.Equal(t, DefaultDockerNetwork, cfg.Runner.DockerNetwork)
	assert.Equal(t, DefaultResultsDir, cfg.Runner.Benchmark.ResultsDir)
	assert.Equal(t, DefaultJWT, cfg.Runner.Client.Config.JWT)
	assert.Equal(t, DefaultPullPolicy, cfg.Runner.Instances[0].PullPolicy)
}

func TestLoad_EnvVarOverridesDefaults(t *testing.T) {
	// Create a minimal config without log_level set.
	configContent := `
runner:
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

	// Create test tarballs for local tarball validation tests.
	fixturesTarball := filepath.Join(tmpDir, "fixtures.tar.gz")
	genesisTarball := filepath.Join(tmpDir, "genesis.tar.gz")
	createTestTarball(t, fixturesTarball)
	createTestTarball(t, genesisTarball)

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
			name: "eest_fixtures missing github_repo for release mode",
			source: SourceConfig{
				EESTFixtures: &EESTFixturesSource{
					GitHubRelease: "benchmark@v0.0.6",
				},
			},
			wantErr:   true,
			errSubstr: "eest_fixtures.github_repo is required for release/artifact modes",
		},
		{
			name: "eest_fixtures missing github_release and artifacts",
			source: SourceConfig{
				EESTFixtures: &EESTFixturesSource{
					GitHubRepo: "ethereum/execution-spec-tests",
				},
			},
			wantErr:   true,
			errSubstr: "must specify one of",
		},
		{
			name: "valid eest_fixtures with artifacts",
			source: SourceConfig{
				EESTFixtures: &EESTFixturesSource{
					GitHubRepo:           "ethereum/execution-spec-tests",
					FixturesArtifactName: "fixtures_benchmark_fast",
				},
			},
			wantErr: false,
		},
		{
			name: "eest_fixtures cannot have both release and artifact",
			source: SourceConfig{
				EESTFixtures: &EESTFixturesSource{
					GitHubRepo:           "ethereum/execution-spec-tests",
					GitHubRelease:        "benchmark@v0.0.6",
					FixturesArtifactName: "fixtures_benchmark_fast",
				},
			},
			wantErr:   true,
			errSubstr: "cannot combine modes",
		},
		{
			name: "valid eest_fixtures with local dir",
			source: SourceConfig{
				EESTFixtures: &EESTFixturesSource{
					LocalFixturesDir: tmpDir,
					LocalGenesisDir:  tmpDir,
				},
			},
			wantErr: false,
		},
		{
			name: "eest_fixtures local dir missing local_genesis_dir",
			source: SourceConfig{
				EESTFixtures: &EESTFixturesSource{
					LocalFixturesDir: tmpDir,
				},
			},
			wantErr:   true,
			errSubstr: "local_genesis_dir is required",
		},
		{
			name: "eest_fixtures local dir missing local_fixtures_dir",
			source: SourceConfig{
				EESTFixtures: &EESTFixturesSource{
					LocalGenesisDir: tmpDir,
				},
			},
			wantErr:   true,
			errSubstr: "local_fixtures_dir is required",
		},
		{
			name: "eest_fixtures local dir path does not exist",
			source: SourceConfig{
				EESTFixtures: &EESTFixturesSource{
					LocalFixturesDir: "/nonexistent/fixtures",
					LocalGenesisDir:  tmpDir,
				},
			},
			wantErr:   true,
			errSubstr: "does not exist",
		},
		{
			name: "eest_fixtures local dir does not require github_repo",
			source: SourceConfig{
				EESTFixtures: &EESTFixturesSource{
					LocalFixturesDir: tmpDir,
					LocalGenesisDir:  tmpDir,
				},
			},
			wantErr: false,
		},
		{
			name: "eest_fixtures cannot mix local dir and release",
			source: SourceConfig{
				EESTFixtures: &EESTFixturesSource{
					GitHubRepo:       "ethereum/execution-spec-tests",
					GitHubRelease:    "benchmark@v0.0.6",
					LocalFixturesDir: tmpDir,
					LocalGenesisDir:  tmpDir,
				},
			},
			wantErr:   true,
			errSubstr: "cannot combine modes",
		},
		{
			name: "eest_fixtures cannot mix local dir and local tarball",
			source: SourceConfig{
				EESTFixtures: &EESTFixturesSource{
					LocalFixturesDir:     tmpDir,
					LocalGenesisDir:      tmpDir,
					LocalFixturesTarball: fixturesTarball,
					LocalGenesisTarball:  genesisTarball,
				},
			},
			wantErr:   true,
			errSubstr: "cannot combine modes",
		},
		{
			name: "valid eest_fixtures with local tarball",
			source: SourceConfig{
				EESTFixtures: &EESTFixturesSource{
					LocalFixturesTarball: fixturesTarball,
					LocalGenesisTarball:  genesisTarball,
				},
			},
			wantErr: false,
		},
		{
			name: "eest_fixtures local tarball missing local_genesis_tarball",
			source: SourceConfig{
				EESTFixtures: &EESTFixturesSource{
					LocalFixturesTarball: fixturesTarball,
				},
			},
			wantErr:   true,
			errSubstr: "local_genesis_tarball is required",
		},
		{
			name: "eest_fixtures local tarball missing local_fixtures_tarball",
			source: SourceConfig{
				EESTFixtures: &EESTFixturesSource{
					LocalGenesisTarball: genesisTarball,
				},
			},
			wantErr:   true,
			errSubstr: "local_fixtures_tarball is required",
		},
		{
			name: "eest_fixtures local tarball path does not exist",
			source: SourceConfig{
				EESTFixtures: &EESTFixturesSource{
					LocalFixturesTarball: "/nonexistent/fixtures.tar.gz",
					LocalGenesisTarball:  genesisTarball,
				},
			},
			wantErr:   true,
			errSubstr: "does not exist",
		},
		{
			name: "eest_fixtures cannot mix local tarball and artifact",
			source: SourceConfig{
				EESTFixtures: &EESTFixturesSource{
					GitHubRepo:           "ethereum/execution-spec-tests",
					FixturesArtifactName: "fixtures_benchmark",
					LocalFixturesTarball: fixturesTarball,
					LocalGenesisTarball:  genesisTarball,
				},
			},
			wantErr:   true,
			errSubstr: "cannot combine modes",
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

func TestGetPostTestRPCCalls(t *testing.T) {
	tests := []struct {
		name     string
		global   []PostTestRPCCall
		instance []PostTestRPCCall
		expected []PostTestRPCCall
	}{
		{
			name:     "no calls configured",
			global:   nil,
			instance: nil,
			expected: nil,
		},
		{
			name: "global only",
			global: []PostTestRPCCall{
				{Method: "debug_traceBlockByNumber"},
			},
			instance: nil,
			expected: []PostTestRPCCall{
				{Method: "debug_traceBlockByNumber"},
			},
		},
		{
			name:   "instance overrides global",
			global: []PostTestRPCCall{{Method: "global_method"}},
			instance: []PostTestRPCCall{
				{Method: "instance_method"},
			},
			expected: []PostTestRPCCall{
				{Method: "instance_method"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Runner: RunnerConfig{
					Client: ClientConfig{
						Config: ClientDefaults{
							PostTestRPCCalls: tt.global,
						},
					},
				},
			}
			instance := &ClientInstance{
				PostTestRPCCalls: tt.instance,
			}
			result := cfg.GetPostTestRPCCalls(instance)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidatePostTestRPCCalls(t *testing.T) {
	tests := []struct {
		name      string
		cfg       Config
		wantErr   bool
		errSubstr string
	}{
		{
			name: "valid global call",
			cfg: Config{
				Runner: RunnerConfig{
					Client: ClientConfig{
						Config: ClientDefaults{
							PostTestRPCCalls: []PostTestRPCCall{
								{Method: "debug_traceBlockByNumber", Params: []any{"latest"}},
							},
						},
					},
					Instances: []ClientInstance{{ID: "test", Client: "geth"}},
				},
			},
			wantErr: false,
		},
		{
			name: "missing method",
			cfg: Config{
				Runner: RunnerConfig{
					Client: ClientConfig{
						Config: ClientDefaults{
							PostTestRPCCalls: []PostTestRPCCall{
								{Params: []any{"latest"}},
							},
						},
					},
					Instances: []ClientInstance{{ID: "test", Client: "geth"}},
				},
			},
			wantErr:   true,
			errSubstr: "method is required",
		},
		{
			name: "dump enabled without filename",
			cfg: Config{
				Runner: RunnerConfig{
					Client: ClientConfig{
						Config: ClientDefaults{
							PostTestRPCCalls: []PostTestRPCCall{
								{
									Method: "debug_traceBlockByNumber",
									Dump:   DumpConfig{Enabled: true},
								},
							},
						},
					},
					Instances: []ClientInstance{{ID: "test", Client: "geth"}},
				},
			},
			wantErr:   true,
			errSubstr: "dump.filename is required",
		},
		{
			name: "dump enabled with filename is valid",
			cfg: Config{
				Runner: RunnerConfig{
					Client: ClientConfig{
						Config: ClientDefaults{
							PostTestRPCCalls: []PostTestRPCCall{
								{
									Method: "debug_traceBlockByNumber",
									Dump: DumpConfig{
										Enabled:  true,
										Filename: "trace",
									},
								},
							},
						},
					},
					Instances: []ClientInstance{{ID: "test", Client: "geth"}},
				},
			},
			wantErr: false,
		},
		{
			name: "instance-level missing method",
			cfg: Config{
				Runner: RunnerConfig{
					Instances: []ClientInstance{
						{
							ID:     "test",
							Client: "geth",
							PostTestRPCCalls: []PostTestRPCCall{
								{Params: []any{"latest"}},
							},
						},
					},
				},
			},
			wantErr:   true,
			errSubstr: "method is required",
		},
		{
			name: "valid timeout",
			cfg: Config{
				Runner: RunnerConfig{
					Client: ClientConfig{
						Config: ClientDefaults{
							PostTestRPCCalls: []PostTestRPCCall{
								{Method: "debug_executionWitness", Timeout: "2m"},
							},
						},
					},
					Instances: []ClientInstance{{ID: "test", Client: "geth"}},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid timeout string",
			cfg: Config{
				Runner: RunnerConfig{
					Client: ClientConfig{
						Config: ClientDefaults{
							PostTestRPCCalls: []PostTestRPCCall{
								{Method: "debug_executionWitness", Timeout: "notaduration"},
							},
						},
					},
					Instances: []ClientInstance{{ID: "test", Client: "geth"}},
				},
			},
			wantErr:   true,
			errSubstr: "invalid timeout",
		},
		{
			name: "negative timeout",
			cfg: Config{
				Runner: RunnerConfig{
					Client: ClientConfig{
						Config: ClientDefaults{
							PostTestRPCCalls: []PostTestRPCCall{
								{Method: "debug_executionWitness", Timeout: "-5s"},
							},
						},
					},
					Instances: []ClientInstance{{ID: "test", Client: "geth"}},
				},
			},
			wantErr:   true,
			errSubstr: "timeout must be positive",
		},
		{
			name: "zero timeout",
			cfg: Config{
				Runner: RunnerConfig{
					Client: ClientConfig{
						Config: ClientDefaults{
							PostTestRPCCalls: []PostTestRPCCall{
								{Method: "debug_executionWitness", Timeout: "0s"},
							},
						},
					},
					Instances: []ClientInstance{{ID: "test", Client: "geth"}},
				},
			},
			wantErr:   true,
			errSubstr: "timeout must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.validatePostTestRPCCalls()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDumpConfigDecodeHook(t *testing.T) {
	// Test that dump: true gets decoded to DumpConfig{Enabled: true}.
	configContent := `
runner:
  client:
    config:
      genesis:
        geth: http://example.com/genesis.json
      post_test_rpc_calls:
        - method: debug_traceBlockByNumber
          params: ["latest"]
          dump:
            enabled: true
            filename: trace
  instances:
    - id: test-instance
      client: geth
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0o644))

	cfg, err := Load(configPath)
	require.NoError(t, err)

	require.Len(t, cfg.Runner.Client.Config.PostTestRPCCalls, 1)
	assert.Equal(t, "debug_traceBlockByNumber", cfg.Runner.Client.Config.PostTestRPCCalls[0].Method)
	assert.True(t, cfg.Runner.Client.Config.PostTestRPCCalls[0].Dump.Enabled)
	assert.Equal(t, "trace", cfg.Runner.Client.Config.PostTestRPCCalls[0].Dump.Filename)
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

func TestGetBootstrapFCU(t *testing.T) {
	tests := []struct {
		name     string
		global   *BootstrapFCUConfig
		instance *BootstrapFCUConfig
		expected *BootstrapFCUConfig
	}{
		{
			name:     "both nil returns nil",
			global:   nil,
			instance: nil,
			expected: nil,
		},
		{
			name:     "global set, instance nil inherits",
			global:   &BootstrapFCUConfig{Enabled: true, MaxRetries: 30, Backoff: "1s"},
			instance: nil,
			expected: &BootstrapFCUConfig{Enabled: true, MaxRetries: 30, Backoff: "1s"},
		},
		{
			name:     "instance overrides global",
			global:   &BootstrapFCUConfig{Enabled: true, MaxRetries: 30, Backoff: "1s"},
			instance: &BootstrapFCUConfig{Enabled: true, MaxRetries: 5, Backoff: "2s"},
			expected: &BootstrapFCUConfig{Enabled: true, MaxRetries: 5, Backoff: "2s"},
		},
		{
			name:     "instance disabled overrides global enabled",
			global:   &BootstrapFCUConfig{Enabled: true, MaxRetries: 30, Backoff: "1s"},
			instance: &BootstrapFCUConfig{Enabled: false},
			expected: &BootstrapFCUConfig{Enabled: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Runner: RunnerConfig{
					Client: ClientConfig{
						Config: ClientDefaults{
							BootstrapFCU: tt.global,
						},
					},
				},
			}
			instance := &ClientInstance{
				BootstrapFCU: tt.instance,
			}
			result := cfg.GetBootstrapFCU(instance)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLoad_PreservesEnvironmentKeyCasing(t *testing.T) {
	configContent := `
runner:
  docker_network: test-network
  client:
    config:
      jwt: test-jwt
      genesis:
        geth: http://example.com/genesis.json
  instances:
    - id: test-instance
      client: geth
      environment:
        MAX_REORG_DEPTH: "512"
        SOME_lower_Mixed: "value"
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0o644))

	cfg, err := Load(configPath)
	require.NoError(t, err)
	require.Len(t, cfg.Runner.Instances, 1)

	env := cfg.Runner.Instances[0].Environment
	assert.Equal(t, "512", env["MAX_REORG_DEPTH"])
	assert.Equal(t, "value", env["SOME_lower_Mixed"])

	// Verify lowercased keys are NOT present.
	_, hasLower := env["max_reorg_depth"]
	assert.False(t, hasLower)
}

func TestLoad_BootstrapFCU(t *testing.T) {
	t.Run("shorthand bool true", func(t *testing.T) {
		configContent := `
runner:
  client:
    config:
      bootstrap_fcu: true
      genesis:
        geth: http://example.com/genesis.json
  instances:
    - id: inherits-global
      client: geth
    - id: override-false
      client: geth
      bootstrap_fcu: false
`
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")
		require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0o644))

		cfg, err := Load(configPath)
		require.NoError(t, err)

		// Global default decoded from bool shorthand.
		require.NotNil(t, cfg.Runner.Client.Config.BootstrapFCU)
		assert.True(t, cfg.Runner.Client.Config.BootstrapFCU.Enabled)
		assert.Equal(t, 30, cfg.Runner.Client.Config.BootstrapFCU.MaxRetries)
		assert.Equal(t, "1s", cfg.Runner.Client.Config.BootstrapFCU.Backoff)

		// First instance inherits global.
		fcuCfg := cfg.GetBootstrapFCU(&cfg.Runner.Instances[0])
		require.NotNil(t, fcuCfg)
		assert.True(t, fcuCfg.Enabled)

		// Second instance overrides to false.
		fcuCfg = cfg.GetBootstrapFCU(&cfg.Runner.Instances[1])
		require.NotNil(t, fcuCfg)
		assert.False(t, fcuCfg.Enabled)
	})

	t.Run("full struct config", func(t *testing.T) {
		configContent := `
runner:
  client:
    config:
      bootstrap_fcu:
        enabled: true
        max_retries: 10
        backoff: 2s
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

		require.NotNil(t, cfg.Runner.Client.Config.BootstrapFCU)
		assert.True(t, cfg.Runner.Client.Config.BootstrapFCU.Enabled)
		assert.Equal(t, 10, cfg.Runner.Client.Config.BootstrapFCU.MaxRetries)
		assert.Equal(t, "2s", cfg.Runner.Client.Config.BootstrapFCU.Backoff)

		fcuCfg := cfg.GetBootstrapFCU(&cfg.Runner.Instances[0])
		require.NotNil(t, fcuCfg)
		assert.Equal(t, 10, fcuCfg.MaxRetries)
		assert.Equal(t, "2s", fcuCfg.Backoff)
	})

	t.Run("not configured returns nil", func(t *testing.T) {
		configContent := `
runner:
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

		assert.Nil(t, cfg.Runner.Client.Config.BootstrapFCU)
		assert.Nil(t, cfg.GetBootstrapFCU(&cfg.Runner.Instances[0]))
	})

	t.Run("with block_hash", func(t *testing.T) {
		configContent := `
runner:
  client:
    config:
      bootstrap_fcu:
        enabled: true
        max_retries: 10
        backoff: 2s
        head_block_hash: "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
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

		require.NotNil(t, cfg.Runner.Client.Config.BootstrapFCU)
		assert.True(t, cfg.Runner.Client.Config.BootstrapFCU.Enabled)
		assert.Equal(t,
			"0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			cfg.Runner.Client.Config.BootstrapFCU.HeadBlockHash,
		)

		fcuCfg := cfg.GetBootstrapFCU(&cfg.Runner.Instances[0])
		require.NotNil(t, fcuCfg)
		assert.Equal(t,
			"0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			fcuCfg.HeadBlockHash,
		)
	})

	t.Run("invalid block_hash rejected", func(t *testing.T) {
		tests := []struct {
			name      string
			blockHash string
		}{
			{"missing 0x prefix", "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"},
			{"too short", "0x1234"},
			{"too long", "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef00"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				cfg := Config{
					Runner: RunnerConfig{
						Client: ClientConfig{
							Config: ClientDefaults{
								BootstrapFCU: &BootstrapFCUConfig{
									Enabled:       true,
									MaxRetries:    10,
									Backoff:       "2s",
									HeadBlockHash: tt.blockHash,
								},
							},
						},
						Instances: []ClientInstance{{ID: "test", Client: "geth"}},
					},
				}

				err := cfg.validateBootstrapFCU()
				require.Error(t, err)
				assert.Contains(t, err.Error(), "bootstrap_fcu.head_block_hash")
			})
		}
	})
}

func TestLoad_MetadataLabels(t *testing.T) {
	t.Run("parses labels from yaml", func(t *testing.T) {
		configContent := `
runner:
  metadata:
    labels:
      env: production
      team: platform
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

		require.Len(t, cfg.Runner.Metadata.Labels, 2)
		assert.Equal(t, "production", cfg.Runner.Metadata.Labels["env"])
		assert.Equal(t, "platform", cfg.Runner.Metadata.Labels["team"])
	})

	t.Run("empty metadata produces no errors", func(t *testing.T) {
		configContent := `
runner:
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

		assert.Nil(t, cfg.Runner.Metadata.Labels)
	})

	t.Run("empty labels map produces no errors", func(t *testing.T) {
		configContent := `
runner:
  metadata:
    labels: {}
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

		assert.Empty(t, cfg.Runner.Metadata.Labels)
	})
}

func TestValidateAPIStorage(t *testing.T) {
	// Helper to build a Config with API storage and minimal valid fields.
	makeConfig := func(s3Cfg *APIS3Config) Config {
		return Config{
			API: &APIConfig{
				Auth: APIAuthConfig{
					SessionTTL: "24h",
					Basic: BasicAuthConfig{
						Enabled: true,
						Users: []BasicAuthUser{
							{Username: "admin", Password: "pass", Role: "admin"},
						},
					},
				},
				Database: APIDatabaseConfig{Driver: "sqlite"},
				Storage:  APIStorageConfig{S3: s3Cfg},
			},
		}
	}

	tests := []struct {
		name      string
		s3Cfg     *APIS3Config
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "nil s3 config is valid",
			s3Cfg:   nil,
			wantErr: false,
		},
		{
			name: "disabled s3 is valid",
			s3Cfg: &APIS3Config{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "valid s3 config",
			s3Cfg: &APIS3Config{
				Enabled:        true,
				Bucket:         "my-bucket",
				Region:         "us-east-1",
				DiscoveryPaths: []string{"results"},
				PresignedURLs:  APIS3PresignedURLConfig{Expiry: "1h"},
			},
			wantErr: false,
		},
		{
			name: "missing bucket",
			s3Cfg: &APIS3Config{
				Enabled:        true,
				DiscoveryPaths: []string{"results"},
				PresignedURLs:  APIS3PresignedURLConfig{Expiry: "1h"},
			},
			wantErr:   true,
			errSubstr: "bucket is required",
		},
		{
			name: "empty discovery paths",
			s3Cfg: &APIS3Config{
				Enabled:        true,
				Bucket:         "my-bucket",
				DiscoveryPaths: []string{},
				PresignedURLs:  APIS3PresignedURLConfig{Expiry: "1h"},
			},
			wantErr:   true,
			errSubstr: "at least one discovery_path",
		},
		{
			name: "empty string in discovery paths",
			s3Cfg: &APIS3Config{
				Enabled:        true,
				Bucket:         "my-bucket",
				DiscoveryPaths: []string{"results", ""},
				PresignedURLs:  APIS3PresignedURLConfig{Expiry: "1h"},
			},
			wantErr:   true,
			errSubstr: "must not be empty",
		},
		{
			name: "path traversal in discovery paths",
			s3Cfg: &APIS3Config{
				Enabled:        true,
				Bucket:         "my-bucket",
				DiscoveryPaths: []string{"results/../secrets"},
				PresignedURLs:  APIS3PresignedURLConfig{Expiry: "1h"},
			},
			wantErr:   true,
			errSubstr: "must not contain \"..\"",
		},
		{
			name: "invalid expiry duration",
			s3Cfg: &APIS3Config{
				Enabled:        true,
				Bucket:         "my-bucket",
				DiscoveryPaths: []string{"results"},
				PresignedURLs:  APIS3PresignedURLConfig{Expiry: "notaduration"},
			},
			wantErr:   true,
			errSubstr: "invalid duration",
		},
		{
			name: "multiple valid discovery paths",
			s3Cfg: &APIS3Config{
				Enabled:        true,
				Bucket:         "my-bucket",
				DiscoveryPaths: []string{"results", "archive/2024"},
				PresignedURLs:  APIS3PresignedURLConfig{Expiry: "30m"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := makeConfig(tt.s3Cfg)
			err := cfg.validateAPIStorage()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateAPILocalStorage(t *testing.T) {
	makeConfig := func(
		localCfg *APILocalStorageConfig,
	) Config {
		return Config{
			API: &APIConfig{
				Auth: APIAuthConfig{
					SessionTTL: "24h",
					Basic: BasicAuthConfig{
						Enabled: true,
						Users: []BasicAuthUser{
							{Username: "admin", Password: "pass", Role: "admin"},
						},
					},
				},
				Database: APIDatabaseConfig{Driver: "sqlite"},
				Storage:  APIStorageConfig{Local: localCfg},
			},
		}
	}

	tests := []struct {
		name      string
		localCfg  *APILocalStorageConfig
		wantErr   bool
		errSubstr string
	}{
		{
			name:     "nil local config is valid",
			localCfg: nil,
			wantErr:  false,
		},
		{
			name: "disabled local is valid",
			localCfg: &APILocalStorageConfig{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "valid local config",
			localCfg: &APILocalStorageConfig{
				Enabled:        true,
				DiscoveryPaths: []string{"/data/results"},
			},
			wantErr: false,
		},
		{
			name: "empty discovery paths",
			localCfg: &APILocalStorageConfig{
				Enabled:        true,
				DiscoveryPaths: []string{},
			},
			wantErr:   true,
			errSubstr: "at least one discovery_path",
		},
		{
			name: "empty string in discovery paths",
			localCfg: &APILocalStorageConfig{
				Enabled:        true,
				DiscoveryPaths: []string{"/data/results", ""},
			},
			wantErr:   true,
			errSubstr: "must not be empty",
		},
		{
			name: "relative path in discovery paths",
			localCfg: &APILocalStorageConfig{
				Enabled:        true,
				DiscoveryPaths: []string{"results/data"},
			},
			wantErr:   true,
			errSubstr: "must be absolute",
		},
		{
			name: "path traversal in discovery paths",
			localCfg: &APILocalStorageConfig{
				Enabled:        true,
				DiscoveryPaths: []string{"/data/../etc/passwd"},
			},
			wantErr:   true,
			errSubstr: "must not contain \"..\"",
		},
		{
			name: "multiple valid discovery paths",
			localCfg: &APILocalStorageConfig{
				Enabled:        true,
				DiscoveryPaths: []string{"/data/results", "/archive/2024"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := makeConfig(tt.localCfg)
			err := cfg.validateAPIStorage()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateAPIStorageMutualExclusivity(t *testing.T) {
	cfg := Config{
		API: &APIConfig{
			Auth: APIAuthConfig{
				SessionTTL: "24h",
				Basic: BasicAuthConfig{
					Enabled: true,
					Users: []BasicAuthUser{
						{Username: "admin", Password: "pass", Role: "admin"},
					},
				},
			},
			Database: APIDatabaseConfig{Driver: "sqlite"},
			Storage: APIStorageConfig{
				S3: &APIS3Config{
					Enabled:        true,
					Bucket:         "my-bucket",
					DiscoveryPaths: []string{"results"},
					PresignedURLs:  APIS3PresignedURLConfig{Expiry: "1h"},
				},
				Local: &APILocalStorageConfig{
					Enabled:        true,
					DiscoveryPaths: []string{"/data/results"},
				},
			},
		},
	}

	err := cfg.validateAPIStorage()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only one backend")
}

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  uint64
		wantErr   bool
		errSubstr string
	}{
		{name: "docker-style gigabytes", input: "8g", expected: 8 * 1024 * 1024 * 1024},
		{name: "docker-style megabytes", input: "512m", expected: 512 * 1024 * 1024},
		{name: "docker-style kilobytes", input: "1024k", expected: 1024 * 1024},
		{name: "uppercase suffix", input: "8G", expected: 8 * 1024 * 1024 * 1024},
		{name: "long suffix GB", input: "8GB", expected: 8 * 1024 * 1024 * 1024},
		{name: "long suffix MB", input: "512MB", expected: 512 * 1024 * 1024},
		{name: "raw bytes", input: "1073741824", expected: 1073741824},
		{name: "zero", input: "0", expected: 0},
		{name: "invalid string", input: "abc", wantErr: true, errSubstr: "invalid byte size"},
		{name: "empty string", input: "", wantErr: true, errSubstr: "empty string"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseByteSize(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestGetCheckpointTmpfsThreshold(t *testing.T) {
	tests := []struct {
		name     string
		global   string
		instance string
		expected string
	}{
		{
			name:     "both empty returns empty (disabled)",
			global:   "",
			instance: "",
			expected: "",
		},
		{
			name:     "global set, instance empty inherits global",
			global:   "8g",
			instance: "",
			expected: "8g",
		},
		{
			name:     "instance overrides global",
			global:   "8g",
			instance: "4g",
			expected: "4g",
		},
		{
			name:     "instance set, global empty",
			global:   "",
			instance: "2g",
			expected: "2g",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var globalOpts *CheckpointRestoreStrategyOptions
			if tt.global != "" {
				globalOpts = &CheckpointRestoreStrategyOptions{TmpfsThreshold: tt.global}
			}

			var instanceOpts *CheckpointRestoreStrategyOptions
			if tt.instance != "" {
				instanceOpts = &CheckpointRestoreStrategyOptions{TmpfsThreshold: tt.instance}
			}

			cfg := &Config{
				Runner: RunnerConfig{
					Client: ClientConfig{
						Config: ClientDefaults{
							CheckpointRestoreStrategyOptions: globalOpts,
						},
					},
				},
			}
			instance := &ClientInstance{
				CheckpointRestoreStrategyOptions: instanceOpts,
			}
			result := cfg.GetCheckpointTmpfsThreshold(instance)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// createTestTarball creates a minimal .tar.gz file at the given path for testing.
func createTestTarball(t *testing.T, path string) {
	t.Helper()

	f, err := os.Create(path)
	require.NoError(t, err)

	defer func() { _ = f.Close() }()

	gw := gzip.NewWriter(f)
	defer func() { _ = gw.Close() }()

	tw := tar.NewWriter(gw)
	defer func() { _ = tw.Close() }()

	// Write a single dummy file into the tarball.
	content := []byte("test")
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "dummy.txt",
		Size: int64(len(content)),
		Mode: 0644,
	}))

	_, err = tw.Write(content)
	require.NoError(t, err)
}

func TestValidateContainerRuntime(t *testing.T) {
	tests := []struct {
		name      string
		runtime   string
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "empty string is valid (defaults to docker)",
			runtime: "",
			wantErr: false,
		},
		{
			name:    "docker is valid",
			runtime: "docker",
			wantErr: false,
		},
		{
			name:    "podman is valid",
			runtime: "podman",
			wantErr: false,
		},
		{
			name:      "invalid runtime rejected",
			runtime:   "containerd",
			wantErr:   true,
			errSubstr: "invalid container_runtime",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Runner: RunnerConfig{
					ContainerRuntime: tt.runtime,
					Instances:        []ClientInstance{{ID: "test", Client: "geth"}},
				},
			}
			err := cfg.validateContainerRuntime()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateRollbackStrategy_CheckpointRestore(t *testing.T) {
	validDir := t.TempDir()

	tests := []struct {
		name      string
		cfg       Config
		wantErr   bool
		errSubstr string
	}{
		{
			name: "checkpoint-restore valid with podman and zfs",
			cfg: Config{
				Runner: RunnerConfig{
					ContainerRuntime: "podman",
					Client: ClientConfig{
						Config: ClientDefaults{
							RollbackStrategy: RollbackStrategyCheckpointRestore,
						},
					},
					Instances: []ClientInstance{
						{
							ID:     "test",
							Client: "geth",
							DataDir: &DataDirConfig{
								SourceDir: validDir,
								Method:    "zfs",
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "checkpoint-restore requires podman runtime",
			cfg: Config{
				Runner: RunnerConfig{
					ContainerRuntime: "",
					Client: ClientConfig{
						Config: ClientDefaults{
							RollbackStrategy: RollbackStrategyCheckpointRestore,
						},
					},
					Instances: []ClientInstance{
						{
							ID:     "test",
							Client: "geth",
							DataDir: &DataDirConfig{
								SourceDir: validDir,
								Method:    "zfs",
							},
						},
					},
				},
			},
			wantErr:   true,
			errSubstr: "requires container_runtime: \"podman\"",
		},
		{
			name: "checkpoint-restore requires podman - explicit docker rejected",
			cfg: Config{
				Runner: RunnerConfig{
					ContainerRuntime: "docker",
					Client: ClientConfig{
						Config: ClientDefaults{
							RollbackStrategy: RollbackStrategyCheckpointRestore,
						},
					},
					Instances: []ClientInstance{
						{
							ID:     "test",
							Client: "geth",
							DataDir: &DataDirConfig{
								SourceDir: validDir,
								Method:    "zfs",
							},
						},
					},
				},
			},
			wantErr:   true,
			errSubstr: "requires container_runtime: \"podman\"",
		},
		{
			name: "checkpoint-restore requires zfs datadir method",
			cfg: Config{
				Runner: RunnerConfig{
					ContainerRuntime: "podman",
					Client: ClientConfig{
						Config: ClientDefaults{
							RollbackStrategy: RollbackStrategyCheckpointRestore,
						},
					},
					Instances: []ClientInstance{
						{
							ID:     "test",
							Client: "geth",
							DataDir: &DataDirConfig{
								SourceDir: validDir,
								Method:    "copy",
							},
						},
					},
				},
			},
			wantErr:   true,
			errSubstr: "with datadir requires datadir.method: \"zfs\"",
		},
		{
			name: "checkpoint-restore without datadir is valid (copy-based rollback)",
			cfg: Config{
				Runner: RunnerConfig{
					ContainerRuntime: "podman",
					Client: ClientConfig{
						Config: ClientDefaults{
							RollbackStrategy: RollbackStrategyCheckpointRestore,
						},
					},
					Instances: []ClientInstance{
						{
							ID:     "test",
							Client: "geth",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "checkpoint-restore with zfs from global datadirs",
			cfg: Config{
				Runner: RunnerConfig{
					ContainerRuntime: "podman",
					Client: ClientConfig{
						Config: ClientDefaults{
							RollbackStrategy: RollbackStrategyCheckpointRestore,
						},
						DataDirs: map[string]*DataDirConfig{
							"geth": {
								SourceDir: validDir,
								Method:    "zfs",
							},
						},
					},
					Instances: []ClientInstance{
						{
							ID:     "test",
							Client: "geth",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "checkpoint-restore instance-level strategy with podman and zfs",
			cfg: Config{
				Runner: RunnerConfig{
					ContainerRuntime: "podman",
					Instances: []ClientInstance{
						{
							ID:               "test",
							Client:           "geth",
							RollbackStrategy: RollbackStrategyCheckpointRestore,
							DataDir: &DataDirConfig{
								SourceDir: validDir,
								Method:    "zfs",
							},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.validateRollbackStrategy(ValidateOpts{})
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGetContainerRuntime(t *testing.T) {
	tests := []struct {
		name     string
		runtime  string
		expected string
	}{
		{
			name:     "empty defaults to docker",
			runtime:  "",
			expected: "docker",
		},
		{
			name:     "docker returns docker",
			runtime:  "docker",
			expected: "docker",
		},
		{
			name:     "podman returns podman",
			runtime:  "podman",
			expected: "podman",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Runner: RunnerConfig{
					ContainerRuntime: tt.runtime,
				},
			}
			assert.Equal(t, tt.expected, cfg.GetContainerRuntime())
		})
	}
}

func TestValidate_WithValidateOpts(t *testing.T) {
	// Create a real directory to use as a valid datadir source.
	validDir := t.TempDir()

	tests := []struct {
		name      string
		cfg       Config
		opts      ValidateOpts
		wantErr   bool
		errSubstr string
	}{
		{
			name: "no opts validates all instance datadirs",
			cfg: Config{
				Runner: RunnerConfig{
					Instances: []ClientInstance{
						{
							ID:     "good",
							Client: "geth",
							DataDir: &DataDirConfig{
								SourceDir: validDir,
								Method:    "copy",
							},
						},
						{
							ID:     "bad",
							Client: "reth",
							DataDir: &DataDirConfig{
								SourceDir: "/nonexistent/datadir",
								Method:    "copy",
							},
						},
					},
				},
			},
			wantErr:   true,
			errSubstr: "does not exist",
		},
		{
			name: "active instance IDs skips excluded instance datadir",
			cfg: Config{
				Runner: RunnerConfig{
					Instances: []ClientInstance{
						{
							ID:     "good",
							Client: "geth",
							DataDir: &DataDirConfig{
								SourceDir: validDir,
								Method:    "copy",
							},
						},
						{
							ID:     "bad",
							Client: "reth",
							DataDir: &DataDirConfig{
								SourceDir: "/nonexistent/datadir",
								Method:    "copy",
							},
						},
					},
				},
			},
			opts: ValidateOpts{
				ActiveInstanceIDs: map[string]struct{}{
					"good": {},
				},
			},
			wantErr: false,
		},
		{
			name: "active instance IDs still validates included instance",
			cfg: Config{
				Runner: RunnerConfig{
					Instances: []ClientInstance{
						{
							ID:     "bad",
							Client: "geth",
							DataDir: &DataDirConfig{
								SourceDir: "/nonexistent/datadir",
								Method:    "copy",
							},
						},
					},
				},
			},
			opts: ValidateOpts{
				ActiveInstanceIDs: map[string]struct{}{
					"bad": {},
				},
			},
			wantErr:   true,
			errSubstr: "does not exist",
		},
		{
			name: "active clients skips excluded global datadir",
			cfg: Config{
				Runner: RunnerConfig{
					Client: ClientConfig{
						DataDirs: map[string]*DataDirConfig{
							"geth": {
								SourceDir: validDir,
								Method:    "copy",
							},
							"reth": {
								SourceDir: "/nonexistent/global/datadir",
								Method:    "copy",
							},
						},
					},
					Instances: []ClientInstance{
						{ID: "inst-1", Client: "geth"},
					},
				},
			},
			opts: ValidateOpts{
				ActiveClients: map[string]struct{}{
					"geth": {},
				},
			},
			wantErr: false,
		},
		{
			name: "active clients still validates included global datadir",
			cfg: Config{
				Runner: RunnerConfig{
					Client: ClientConfig{
						DataDirs: map[string]*DataDirConfig{
							"geth": {
								SourceDir: "/nonexistent/global/datadir",
								Method:    "copy",
							},
						},
					},
					Instances: []ClientInstance{
						{ID: "inst-1", Client: "geth"},
					},
				},
			},
			opts: ValidateOpts{
				ActiveClients: map[string]struct{}{
					"geth": {},
				},
			},
			wantErr:   true,
			errSubstr: "does not exist",
		},
		{
			name: "empty opts maps validates everything",
			cfg: Config{
				Runner: RunnerConfig{
					Client: ClientConfig{
						DataDirs: map[string]*DataDirConfig{
							"reth": {
								SourceDir: "/nonexistent/global/datadir",
								Method:    "copy",
							},
						},
					},
					Instances: []ClientInstance{
						{ID: "inst-1", Client: "geth"},
					},
				},
			},
			opts: ValidateOpts{
				ActiveInstanceIDs: map[string]struct{}{},
				ActiveClients:     map[string]struct{}{},
			},
			wantErr:   true,
			errSubstr: "does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate(tt.opts)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestLoad_TestsMetadataLabels(t *testing.T) {
	t.Run("parses suite-level labels from yaml", func(t *testing.T) {
		configContent := `
runner:
  benchmark:
    tests:
      metadata:
        labels:
          name: "EIP-7934 BN128 Benchmarks"
          category: precompile
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

		require.Len(t, cfg.Runner.Benchmark.Tests.Metadata.Labels, 2)
		assert.Equal(t, "EIP-7934 BN128 Benchmarks", cfg.Runner.Benchmark.Tests.Metadata.Labels["name"])
		assert.Equal(t, "precompile", cfg.Runner.Benchmark.Tests.Metadata.Labels["category"])
	})

	t.Run("empty tests metadata produces no errors", func(t *testing.T) {
		configContent := `
runner:
  benchmark:
    tests:
      filter: "some-filter"
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

		assert.Nil(t, cfg.Runner.Benchmark.Tests.Metadata.Labels)
	})
}
