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
			name: "eest_fixtures missing github_release and artifacts",
			source: SourceConfig{
				EESTFixtures: &EESTFixturesSource{
					GitHubRepo: "ethereum/execution-spec-tests",
				},
			},
			wantErr:   true,
			errSubstr: "must specify either github_release or fixtures_artifact_name",
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
			errSubstr: "cannot specify both github_release and fixtures_artifact_name",
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
				Client: ClientConfig{
					Config: ClientDefaults{
						PostTestRPCCalls: tt.global,
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
				Client: ClientConfig{
					Config: ClientDefaults{
						PostTestRPCCalls: []PostTestRPCCall{
							{Method: "debug_traceBlockByNumber", Params: []any{"latest"}},
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
				Client: ClientConfig{
					Config: ClientDefaults{
						PostTestRPCCalls: []PostTestRPCCall{
							{Params: []any{"latest"}},
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
				Client: ClientConfig{
					Config: ClientDefaults{
						PostTestRPCCalls: []PostTestRPCCall{
							{
								Method: "debug_traceBlockByNumber",
								Dump:   DumpConfig{Enabled: true},
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
					Instances: []ClientInstance{{ID: "test", Client: "geth"}},
				},
			},
			wantErr: false,
		},
		{
			name: "instance-level missing method",
			cfg: Config{
				Client: ClientConfig{
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
				Client: ClientConfig{
					Config: ClientDefaults{
						PostTestRPCCalls: []PostTestRPCCall{
							{Method: "debug_executionWitness", Timeout: "2m"},
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
				Client: ClientConfig{
					Config: ClientDefaults{
						PostTestRPCCalls: []PostTestRPCCall{
							{Method: "debug_executionWitness", Timeout: "notaduration"},
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
				Client: ClientConfig{
					Config: ClientDefaults{
						PostTestRPCCalls: []PostTestRPCCall{
							{Method: "debug_executionWitness", Timeout: "-5s"},
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
				Client: ClientConfig{
					Config: ClientDefaults{
						PostTestRPCCalls: []PostTestRPCCall{
							{Method: "debug_executionWitness", Timeout: "0s"},
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

	require.Len(t, cfg.Client.Config.PostTestRPCCalls, 1)
	assert.Equal(t, "debug_traceBlockByNumber", cfg.Client.Config.PostTestRPCCalls[0].Method)
	assert.True(t, cfg.Client.Config.PostTestRPCCalls[0].Dump.Enabled)
	assert.Equal(t, "trace", cfg.Client.Config.PostTestRPCCalls[0].Dump.Filename)
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
				Client: ClientConfig{
					Config: ClientDefaults{
						BootstrapFCU: tt.global,
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
global:
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
	require.Len(t, cfg.Client.Instances, 1)

	env := cfg.Client.Instances[0].Environment
	assert.Equal(t, "512", env["MAX_REORG_DEPTH"])
	assert.Equal(t, "value", env["SOME_lower_Mixed"])

	// Verify lowercased keys are NOT present.
	_, hasLower := env["max_reorg_depth"]
	assert.False(t, hasLower)
}

func TestLoad_BootstrapFCU(t *testing.T) {
	t.Run("shorthand bool true", func(t *testing.T) {
		configContent := `
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
		require.NotNil(t, cfg.Client.Config.BootstrapFCU)
		assert.True(t, cfg.Client.Config.BootstrapFCU.Enabled)
		assert.Equal(t, 30, cfg.Client.Config.BootstrapFCU.MaxRetries)
		assert.Equal(t, "1s", cfg.Client.Config.BootstrapFCU.Backoff)

		// First instance inherits global.
		fcuCfg := cfg.GetBootstrapFCU(&cfg.Client.Instances[0])
		require.NotNil(t, fcuCfg)
		assert.True(t, fcuCfg.Enabled)

		// Second instance overrides to false.
		fcuCfg = cfg.GetBootstrapFCU(&cfg.Client.Instances[1])
		require.NotNil(t, fcuCfg)
		assert.False(t, fcuCfg.Enabled)
	})

	t.Run("full struct config", func(t *testing.T) {
		configContent := `
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

		require.NotNil(t, cfg.Client.Config.BootstrapFCU)
		assert.True(t, cfg.Client.Config.BootstrapFCU.Enabled)
		assert.Equal(t, 10, cfg.Client.Config.BootstrapFCU.MaxRetries)
		assert.Equal(t, "2s", cfg.Client.Config.BootstrapFCU.Backoff)

		fcuCfg := cfg.GetBootstrapFCU(&cfg.Client.Instances[0])
		require.NotNil(t, fcuCfg)
		assert.Equal(t, 10, fcuCfg.MaxRetries)
		assert.Equal(t, "2s", fcuCfg.Backoff)
	})

	t.Run("not configured returns nil", func(t *testing.T) {
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

		assert.Nil(t, cfg.Client.Config.BootstrapFCU)
		assert.Nil(t, cfg.GetBootstrapFCU(&cfg.Client.Instances[0]))
	})

	t.Run("with block_hash", func(t *testing.T) {
		configContent := `
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

		require.NotNil(t, cfg.Client.Config.BootstrapFCU)
		assert.True(t, cfg.Client.Config.BootstrapFCU.Enabled)
		assert.Equal(t,
			"0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			cfg.Client.Config.BootstrapFCU.HeadBlockHash,
		)

		fcuCfg := cfg.GetBootstrapFCU(&cfg.Client.Instances[0])
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
					Client: ClientConfig{
						Config: ClientDefaults{
							BootstrapFCU: &BootstrapFCUConfig{
								Enabled:       true,
								MaxRetries:    10,
								Backoff:       "2s",
								HeadBlockHash: tt.blockHash,
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
