package config

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildsFillImage(t *testing.T) {
	tests := []struct {
		name           string
		fillImage      string
		fillDockerfile string
		wantBuilds     bool
		wantTag        string
	}{
		{
			name:       "neither set builds from embedded default",
			wantBuilds: true,
			wantTag:    DefaultFillImageTag,
		},
		{
			name:       "fill_image only pulls",
			fillImage:  "fill:latest",
			wantBuilds: false,
			wantTag:    "fill:latest",
		},
		{
			name:           "fill_dockerfile only builds, default tag",
			fillDockerfile: "Dockerfile.eest-filler",
			wantBuilds:     true,
			wantTag:        DefaultFillImageTag,
		},
		{
			name:           "both set builds, tagged fill_image",
			fillImage:      "fill:latest",
			fillDockerfile: "Dockerfile.eest-filler",
			wantBuilds:     true,
			wantTag:        "fill:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &EESTPayloadsConfig{FillImage: tt.fillImage, FillDockerfile: tt.fillDockerfile}
			assert.Equal(t, tt.wantBuilds, e.BuildsFillImage())
			assert.Equal(t, tt.wantTag, e.ResolveFillImageTag())
		})
	}
}

func TestGetBuilderRunTimeout(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want time.Duration
	}{
		{"nil builder", &Config{}, 0},
		{"empty", &Config{Builder: &BuilderConfig{}}, 0},
		{"valid", &Config{Builder: &BuilderConfig{RunTimeout: "2h"}}, 2 * time.Hour},
		{"invalid falls back to 0", &Config{Builder: &BuilderConfig{RunTimeout: "nope"}}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cfg.GetBuilderRunTimeout())
		})
	}
}

func TestValidateBuilder_RunTimeout(t *testing.T) {
	require.NoError(t, (&Config{Builder: &BuilderConfig{RunTimeout: "30m"}}).validateBuilder())

	err := (&Config{Builder: &BuilderConfig{RunTimeout: "5 hours"}}).validateBuilder()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "builder.run_timeout")
}

func TestLoad_BuilderRunTimeoutEnv(t *testing.T) {
	// The key is absent from the file — the env binding must still populate it.
	f := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(f, []byte(
		"builder:\n  state_actor:\n    images: {geth: img}\n    targets:\n      - client: geth\n        output_dir: /tmp/x\n"), 0o600))

	t.Setenv("BENCHMARKOOR_BUILDER_RUN_TIMEOUT", "3h")

	c, err := Load(f)
	require.NoError(t, err)
	require.NotNil(t, c.Builder)
	assert.Equal(t, "3h", c.Builder.RunTimeout)
	assert.Equal(t, 3*time.Hour, c.GetBuilderRunTimeout())
}

func TestLoad_BuilderCleanupOnStart(t *testing.T) {
	base := "builder:\n%s  state_actor:\n    images: {geth: img}\n" +
		"    targets:\n      - client: geth\n        output_dir: /tmp/x\n"

	t.Run("from file", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(f, []byte(
			fmt.Sprintf(base, "  cleanup_on_start: true\n")), 0o600))

		c, err := Load(f)
		require.NoError(t, err)
		require.NotNil(t, c.Builder)
		assert.True(t, c.Builder.CleanupOnStart)
	})

	t.Run("defaults to false", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(f, []byte(fmt.Sprintf(base, "")), 0o600))

		c, err := Load(f)
		require.NoError(t, err)
		require.NotNil(t, c.Builder)
		assert.False(t, c.Builder.CleanupOnStart)
	})

	t.Run("from env when absent in file", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(f, []byte(fmt.Sprintf(base, "")), 0o600))

		t.Setenv("BENCHMARKOOR_BUILDER_CLEANUP_ON_START", "true")

		c, err := Load(f)
		require.NoError(t, err)
		require.NotNil(t, c.Builder)
		assert.True(t, c.Builder.CleanupOnStart)
	})
}

func TestLoad_InlineAddressStubsKeyCasing(t *testing.T) {
	// Viper is case-insensitive and lowercases all map keys; EEST resolves stub
	// names by exact match, so Load must restore the original casing.
	configContent := `
builder:
  eest_payloads:
    fill_image: fill:latest
    targets:
      - name: geth-stateful
        filler_client: geth
        filler_image: ethpandaops/geth:master
        source_dir: /snap
        output_dir: /out
        fork: Amsterdam
        tests:
          - tests/benchmark/stateful/bloatnet
        address_stubs:
          bloated_EOA_10GB:
            addr: "0x87a6314da5ac8832f6e7a176c8fb133b19f5be04"
            pkey: "0x4da32d29f6dcffa26e09dc4e102033f2d105de1444fb893493ae703289275e0e"
`

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0o644))

	cfg, err := Load(configPath)
	require.NoError(t, err)

	stubs := cfg.Builder.EESTPayloads.Targets[0].AddressStubs
	require.Contains(t, stubs, "bloated_EOA_10GB", "stub-name casing must survive Viper lowercasing")
	assert.Equal(t, "0x87a6314da5ac8832f6e7a176c8fb133b19f5be04", stubs["bloated_EOA_10GB"]["addr"])
	assert.Equal(t, "0x4da32d29f6dcffa26e09dc4e102033f2d105de1444fb893493ae703289275e0e", stubs["bloated_EOA_10GB"]["pkey"])
}

func TestLoad_GlobalAddressStubsHoistAndCasing(t *testing.T) {
	// address_stubs defined once under config: must keep its key casing and be
	// hoisted into a target that sets none of its own.
	configContent := `
builder:
  eest_payloads:
    fill_image: fill:latest
    config:
      fork: Amsterdam
      datadir_method: copy
      tests:
        - tests/benchmark/stateful/bloatnet
      filter: "not erc20"
      address_stubs:
        bloated_EOA_10GB:
          addr: "0x87a6314da5ac8832f6e7a176c8fb133b19f5be04"
    targets:
      - name: geth-stateful
        filler_client: geth
        filler_image: ethpandaops/geth:master
        source_dir: /snap
        output_dir: /out
`

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0o644))

	cfg, err := Load(configPath)
	require.NoError(t, err)

	// Global config block keeps the original stub-name casing.
	require.Contains(t, cfg.Builder.EESTPayloads.Config.AddressStubs, "bloated_EOA_10GB")

	// And it (plus tests/filter) is hoisted into the bare target.
	resolved := cfg.Builder.EESTPayloads.ResolveTarget(0)
	assert.Equal(t, []string{"tests/benchmark/stateful/bloatnet"}, resolved.Tests)
	assert.Equal(t, "not erc20", resolved.Filter)
	require.Contains(t, resolved.AddressStubs, "bloated_EOA_10GB")
	assert.Equal(t, "0x87a6314da5ac8832f6e7a176c8fb133b19f5be04", resolved.AddressStubs["bloated_EOA_10GB"]["addr"])
}

func TestValidate_InstanceGenesisOverrideMutualExclusion(t *testing.T) {
	forkOverride := map[string]uint64{"amsterdam": 1}
	eipOverride := &GenesisEIPOverride{Timestamp: 1, EIPs: []uint64{7928}}

	mkCfg := func(inst ClientInstance) *Config {
		return &Config{Runner: RunnerConfig{Instances: []ClientInstance{inst}}}
	}

	t.Run("both set is rejected", func(t *testing.T) {
		err := mkCfg(ClientInstance{
			ID: "geth", Client: "geth",
			GenesisForkOverride: forkOverride,
			GenesisEIPOverride:  eipOverride,
		}).Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "genesis_fork_override and genesis_eip_override are mutually exclusive")
	})

	t.Run("only fork override does not trip the check", func(t *testing.T) {
		// Validate may still fail for unrelated reasons (no test source, etc.) —
		// just assert it isn't the mutual-exclusion error.
		err := mkCfg(ClientInstance{
			ID: "geth", Client: "geth", GenesisForkOverride: forkOverride,
		}).Validate()
		if err != nil {
			assert.NotContains(t, err.Error(), "mutually exclusive")
		}
	})

	t.Run("only eip override does not trip the check", func(t *testing.T) {
		err := mkCfg(ClientInstance{
			ID: "nethermind", Client: "nethermind", GenesisEIPOverride: eipOverride,
		}).Validate()
		if err != nil {
			assert.NotContains(t, err.Error(), "mutually exclusive")
		}
	})
}

func TestLoad_StateActorSpec(t *testing.T) {
	dir := t.TempDir()

	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))

		return p
	}

	t.Run("structured mapping materializes to the YAML body with number fidelity", func(t *testing.T) {
		cfg, err := Load(write("structured.yaml", `
builder:
  state_actor:
    images: { geth: img }
    config: { target_size: 1GB }
    spec:
      entities:
        - kind: eoa
          name: bloated
          approximate_size_bytes: 2_000_000_000
        - kind: contract
          address: 0x4e59b44847b379578588920cA78FbF26c0B4956C
    targets:
      - { client: geth, output_dir: /o }
`))
		require.NoError(t, err)
		require.NoError(t, cfg.validateBuilder())

		kind, body := cfg.Builder.StateActor.ResolveSpec()
		assert.Equal(t, StateActorSpecInline, kind)
		assert.Contains(t, body, "entities:")
		// Bare integers and mixed-case hex round-trip exactly (no float coercion,
		// no lowercasing) because the spec is re-parsed from the raw YAML.
		assert.Contains(t, body, "2_000_000_000")
		assert.Contains(t, body, "0x4e59b44847b379578588920cA78FbF26c0B4956C")
	})

	t.Run("block-scalar spec still works (back-compat)", func(t *testing.T) {
		cfg, err := Load(write("scalar.yaml", "builder:\n"+
			"  state_actor:\n"+
			"    images: { geth: img }\n"+
			"    config: { target_size: 1GB }\n"+
			"    spec: |\n"+
			"      entities:\n"+
			"        - kind: eoa\n"+
			"          name: legacy\n"+
			"    targets:\n"+
			"      - { client: geth, output_dir: /o }\n"))
		require.NoError(t, err)

		kind, body := cfg.Builder.StateActor.ResolveSpec()
		assert.Equal(t, StateActorSpecInline, kind)
		assert.Contains(t, body, "name: legacy")
	})
}

func TestLoad_MultiFileConfigStubCasing(t *testing.T) {
	// Viper deep-merges the eest_payloads.config map across --config files. The
	// stub-name casing restore must accumulate config.address_stubs from every
	// file, not just the last one — otherwise a config.address_stubs defined in
	// an earlier file keeps its Viper-lowercased keys when a later file only
	// touches a different config field.
	dir := t.TempDir()

	base := filepath.Join(dir, "base.yaml")
	require.NoError(t, os.WriteFile(base, []byte(`
builder:
  eest_payloads:
    fill_image: fill:latest
    config:
      address_stubs:
        bloated_EOA_10GB: { addr: "0xabc" }
    targets:
      - name: t
        filler_client: geth
        filler_image: g
        source_dir: /s
        output_dir: /o
        fork: Osaka
        tests: [x]
`), 0o644))

	// Merged last; sets a different config field, no address_stubs.
	override := filepath.Join(dir, "override.yaml")
	require.NoError(t, os.WriteFile(override, []byte(`
builder:
  eest_payloads:
    config:
      fork: Prague
`), 0o644))

	cfg, err := Load(base, override)
	require.NoError(t, err)

	stubs := cfg.Builder.EESTPayloads.Config.AddressStubs
	require.Contains(t, stubs, "bloated_EOA_10GB",
		"config.address_stubs casing must survive a multi-file merge that touches other config fields")
	assert.Equal(t, "Prague", cfg.Builder.EESTPayloads.Config.Fork, "later file's fork still wins")
}

func TestLoad_GlobalEnv(t *testing.T) {
	configContent := `
global:
  log_level: info
  env:
    STATE_DIR: /tmp/bench/state-actor/simple-amsterdam-compute
    NESTED: ${BASE_DIR:-/srv}/fixtures
runner:
  container_network: ${STATE_DIR}
  benchmark:
    results_dir: ${NESTED}
    tests:
      filter: ${MISSING:-fallback}
`
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0o644))

	t.Run("global.env supplies ${VAR}; defaults and nesting work", func(t *testing.T) {
		cfg, err := Load(configPath)
		require.NoError(t, err)

		// global.env value substituted where no inline default is given.
		assert.Equal(t, "/tmp/bench/state-actor/simple-amsterdam-compute", cfg.Runner.ContainerNetwork)
		// global.env value that itself references the shell env (BASE_DIR unset → its default).
		assert.Equal(t, "/srv/fixtures", cfg.Runner.Benchmark.ResultsDir)
		// inline default still applies when neither shell nor global.env has the var.
		assert.Equal(t, "fallback", cfg.Runner.Benchmark.Tests.Filter)
		// keys keep their original casing for substitution despite Viper lowercasing.
		require.Contains(t, cfg.Global.Env, "state_dir") // parsed map is lowercased (documented)
	})

	t.Run("shell env overrides global.env", func(t *testing.T) {
		t.Setenv("STATE_DIR", "/mnt/big")
		cfg, err := Load(configPath)
		require.NoError(t, err)
		assert.Equal(t, "/mnt/big", cfg.Runner.ContainerNetwork, "shell env must win over global.env")
	})
}

func TestLoad_EnvVarOverrides(t *testing.T) {
	// Create a minimal config file for testing.
	configContent := `
global:
  log_level: info
runner:
  container_network: test-network
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
				assert.Equal(t, "test-network", cfg.Runner.ContainerNetwork)
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
			name: "string override - container_network",
			envVars: map[string]string{
				"BENCHMARKOOR_RUNNER_CONTAINER_NETWORK": "custom-network",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "custom-network", cfg.Runner.ContainerNetwork)
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
			name: "nested field override - global.directories.cachedir",
			envVars: map[string]string{
				"BENCHMARKOOR_GLOBAL_DIRECTORIES_CACHEDIR": "/cache/custom",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "/cache/custom", cfg.Global.Directories.CacheDir)
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
				"BENCHMARKOOR_RUNNER_CONTAINER_NETWORK":     "multi-network",
				"BENCHMARKOOR_RUNNER_BENCHMARK_RESULTS_DIR": "/results/multi",
				"BENCHMARKOOR_RUNNER_CLEANUP_ON_START":      "true",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "trace", cfg.Global.LogLevel)
				assert.Equal(t, "multi-network", cfg.Runner.ContainerNetwork)
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
	assert.Equal(t, DefaultContainerNetwork, cfg.Runner.ContainerNetwork)
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
			name: "eest_fixtures local dir without local_genesis_dir (stateful)",
			source: SourceConfig{
				EESTFixtures: &EESTFixturesSource{
					LocalFixturesDir: tmpDir,
				},
			},
			wantErr: false,
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
		{
			name: "valid archive source with URL",
			source: SourceConfig{
				Archive: &ArchiveSourceConfig{
					File: "https://example.com/fixtures.zip",
				},
			},
			wantErr: false,
		},
		{
			name: "valid archive source with local file",
			source: SourceConfig{
				Archive: &ArchiveSourceConfig{
					File: fixturesTarball,
				},
			},
			wantErr: false,
		},
		{
			name: "archive missing file and parts",
			source: SourceConfig{
				Archive: &ArchiveSourceConfig{},
			},
			wantErr:   true,
			errSubstr: "archive.file or archive.parts is required",
		},
		{
			name: "valid archive source with parts",
			source: SourceConfig{
				Archive: &ArchiveSourceConfig{
					Parts: []string{
						"https://example.com/fixtures.tar.gz.00.part",
						"https://example.com/fixtures.tar.gz.01.part",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "archive file and parts are mutually exclusive",
			source: SourceConfig{
				Archive: &ArchiveSourceConfig{
					File:  "https://example.com/fixtures.tar.gz",
					Parts: []string{"https://example.com/fixtures.tar.gz.00.part"},
				},
			},
			wantErr:   true,
			errSubstr: "mutually exclusive",
		},
		{
			name: "multiple sources not allowed - archive and git",
			source: SourceConfig{
				Archive: &ArchiveSourceConfig{
					File: "https://example.com/fixtures.zip",
				},
				Git: &GitSourceV2{
					Repo:    "https://github.com/test/repo",
					Version: "v1.0.0",
				},
			},
			wantErr:   true,
			errSubstr: "cannot specify multiple sources",
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
		{
			name: "archive source",
			source: SourceConfig{
				Archive: &ArchiveSourceConfig{
					File: "https://example.com/fixtures.zip",
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
  container_network: test-network
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
  client:
    config:
      genesis:
        geth: http://example.com/genesis.json
      metadata:
        labels:
          env: production
          team: platform
  instances:
    - id: test-instance
      client: geth
`
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")
		require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0o644))

		cfg, err := Load(configPath)
		require.NoError(t, err)

		require.Len(t, cfg.Runner.Client.Config.Metadata.Labels, 2)
		assert.Equal(t, "production", cfg.Runner.Client.Config.Metadata.Labels["env"])
		assert.Equal(t, "platform", cfg.Runner.Client.Config.Metadata.Labels["team"])
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

		assert.Nil(t, cfg.Runner.Client.Config.Metadata.Labels)
	})

	t.Run("empty labels map produces no errors", func(t *testing.T) {
		configContent := `
runner:
  client:
    config:
      genesis:
        geth: http://example.com/genesis.json
      metadata:
        labels: {}
  instances:
    - id: test-instance
      client: geth
`
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")
		require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0o644))

		cfg, err := Load(configPath)
		require.NoError(t, err)

		assert.Empty(t, cfg.Runner.Client.Config.Metadata.Labels)
	})
}

func TestGetMetadataLabels(t *testing.T) {
	tests := []struct {
		name           string
		clientLabels   map[string]string
		instanceLabels map[string]string
		expected       map[string]string
	}{
		{
			name:     "no labels at either level",
			expected: nil,
		},
		{
			name:         "only client-level labels",
			clientLabels: map[string]string{"env": "production", "team": "platform"},
			expected:     map[string]string{"env": "production", "team": "platform"},
		},
		{
			name:           "only instance-level labels",
			instanceLabels: map[string]string{"variant": "snap-sync"},
			expected:       map[string]string{"variant": "snap-sync"},
		},
		{
			name:           "both levels no overlap",
			clientLabels:   map[string]string{"env": "production"},
			instanceLabels: map[string]string{"variant": "snap-sync"},
			expected:       map[string]string{"env": "production", "variant": "snap-sync"},
		},
		{
			name:           "instance overrides client on conflict",
			clientLabels:   map[string]string{"env": "production", "team": "platform"},
			instanceLabels: map[string]string{"env": "staging"},
			expected:       map[string]string{"env": "staging", "team": "platform"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Runner: RunnerConfig{
					Client: ClientConfig{
						Config: ClientDefaults{
							Metadata: MetadataConfig{Labels: tt.clientLabels},
						},
					},
				},
			}
			instance := &ClientInstance{
				Metadata: MetadataConfig{Labels: tt.instanceLabels},
			}

			result := cfg.GetMetadataLabels(instance)
			assert.Equal(t, tt.expected, result)
		})
	}
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
				DiscoveryPaths: map[string]string{"results": "/data/results"},
			},
			wantErr: false,
		},
		{
			name: "empty discovery paths",
			localCfg: &APILocalStorageConfig{
				Enabled:        true,
				DiscoveryPaths: map[string]string{},
			},
			wantErr:   true,
			errSubstr: "at least one discovery_path",
		},
		{
			name: "empty value in discovery paths",
			localCfg: &APILocalStorageConfig{
				Enabled:        true,
				DiscoveryPaths: map[string]string{"results": ""},
			},
			wantErr:   true,
			errSubstr: "path must not be empty",
		},
		{
			name: "relative path in discovery paths",
			localCfg: &APILocalStorageConfig{
				Enabled:        true,
				DiscoveryPaths: map[string]string{"results": "results/data"},
			},
			wantErr:   true,
			errSubstr: "must be absolute",
		},
		{
			name: "path traversal in discovery paths",
			localCfg: &APILocalStorageConfig{
				Enabled:        true,
				DiscoveryPaths: map[string]string{"results": "/data/../etc/passwd"},
			},
			wantErr:   true,
			errSubstr: "must not contain \"..\"",
		},
		{
			name: "multiple valid discovery paths",
			localCfg: &APILocalStorageConfig{
				Enabled: true,
				DiscoveryPaths: map[string]string{
					"results": "/data/results",
					"archive": "/archive/2024",
				},
			},
			wantErr: false,
		},
		{
			name: "key with slash",
			localCfg: &APILocalStorageConfig{
				Enabled:        true,
				DiscoveryPaths: map[string]string{"a/b": "/data/results"},
			},
			wantErr:   true,
			errSubstr: "must not contain \"/\"",
		},
		{
			name: "key with dot-dot",
			localCfg: &APILocalStorageConfig{
				Enabled:        true,
				DiscoveryPaths: map[string]string{"..": "/data/results"},
			},
			wantErr:   true,
			errSubstr: "key must not contain \"..\"",
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
					DiscoveryPaths: map[string]string{"results": "/data/results"},
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
		{
			name: "checkpoint-restore rejects schelk datadir",
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
								Method:    "schelk",
							},
						},
					},
				},
			},
			wantErr:   true,
			errSubstr: "with datadir requires datadir.method: \"zfs\"",
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

func TestValidateDataDirMethods_SchelkBinary(t *testing.T) {
	validDir := t.TempDir()

	mkCfg := func(method, sourceDir string) Config {
		return Config{
			Runner: RunnerConfig{
				Instances: []ClientInstance{
					{
						ID:     "test",
						Client: "geth",
						DataDir: &DataDirConfig{
							SourceDir: sourceDir,
							Method:    method,
						},
					},
				},
			},
		}
	}

	// stageSchelkState writes a fake schelk state file with the given mount
	// point and is_mounted flag, then points SCHELK_STATE at it. For
	// "happy path" tests, pass mountPoint="/" and isMounted=true so the
	// /proc/mounts check matches state without running `schelk mount`.
	stageSchelkState := func(t *testing.T, mountPoint string, isMounted bool) {
		t.Helper()

		dir := t.TempDir()
		statePath := filepath.Join(dir, "state.json")
		body := fmt.Sprintf(`{"mount_point":%q,"is_mounted":%t}`, mountPoint, isMounted)
		require.NoError(t, os.WriteFile(statePath, []byte(body), 0o600))
		t.Setenv("SCHELK_STATE", statePath)
	}

	// stageSchelkBin installs a no-op `schelk` shim on PATH so the binary
	// preflight passes and any `schelk mount` invocation succeeds without
	// touching the kernel.
	stageSchelkBin := func(t *testing.T, name string) string {
		t.Helper()

		binDir := t.TempDir()
		fake := filepath.Join(binDir, name)
		require.NoError(t, os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755))
		t.Setenv("PATH", binDir)

		return fake
	}

	t.Run("non-schelk methods don't probe PATH", func(t *testing.T) {
		// Hide PATH so any LookPath call would fail; copy must still validate.
		t.Setenv("PATH", "")

		cfg := mkCfg("copy", validDir)
		require.NoError(t, cfg.validateDataDirMethods(ValidateOpts{}))
	})

	t.Run("schelk method requires `schelk` on PATH", func(t *testing.T) {
		t.Setenv("PATH", "")

		cfg := mkCfg("schelk", validDir)
		err := cfg.validateDataDirMethods(ValidateOpts{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires the `schelk` binary on PATH")
	})

	t.Run("inactive instance with schelk is skipped", func(t *testing.T) {
		t.Setenv("PATH", "")

		cfg := mkCfg("schelk", validDir)
		opts := ValidateOpts{ActiveInstanceIDs: map[string]struct{}{"other": {}}}
		require.NoError(t, cfg.validateDataDirMethods(opts))
	})

	t.Run("schelk method passes when binary and state are present", func(t *testing.T) {
		stageSchelkBin(t, "schelk")
		stageSchelkState(t, "/", true)

		cfg := mkCfg("schelk", validDir)
		require.NoError(t, cfg.validateDataDirMethods(ValidateOpts{}))
	})

	t.Run("missing schelk state file is reported clearly", func(t *testing.T) {
		stageSchelkBin(t, "schelk")
		// Point SCHELK_STATE at a path that doesn't exist.
		t.Setenv("SCHELK_STATE", filepath.Join(t.TempDir(), "missing.json"))

		cfg := mkCfg("schelk", validDir)
		err := cfg.validateDataDirMethods(ValidateOpts{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "schelk state file")
		assert.Contains(t, err.Error(), "init-new")
	})

	t.Run("source_dir under mount that doesn't exist fails clearly", func(t *testing.T) {
		stageSchelkBin(t, "schelk")
		stageSchelkState(t, "/", true)

		cfg := mkCfg("schelk", "/this/path/should/not/exist/in/tests")
		err := cfg.validateDataDirMethods(ValidateOpts{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not exist under schelk mount")
	})

	t.Run("BENCHMARKOOR_SCHELK_BIN overrides the binary name", func(t *testing.T) {
		stageSchelkBin(t, "schelk-custom")
		t.Setenv("BENCHMARKOOR_SCHELK_BIN", "schelk-custom")
		stageSchelkState(t, "/", true)

		cfg := mkCfg("schelk", validDir)
		require.NoError(t, cfg.validateDataDirMethods(ValidateOpts{}))
	})

	t.Run("BENCHMARKOOR_SCHELK_BIN absolute path bypasses PATH lookup", func(t *testing.T) {
		fake := stageSchelkBin(t, "schelk-abs")
		t.Setenv("PATH", "")
		t.Setenv("BENCHMARKOOR_SCHELK_BIN", fake)
		stageSchelkState(t, "/", true)

		cfg := mkCfg("schelk", validDir)
		require.NoError(t, cfg.validateDataDirMethods(ValidateOpts{}))
	})

	t.Run("BENCHMARKOOR_SCHELK_BIN error message names the override", func(t *testing.T) {
		t.Setenv("PATH", "")
		t.Setenv("BENCHMARKOOR_SCHELK_BIN", "missing-schelk-binary")

		cfg := mkCfg("schelk", validDir)
		err := cfg.validateDataDirMethods(ValidateOpts{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing-schelk-binary")
		assert.Contains(t, err.Error(), "BENCHMARKOOR_SCHELK_BIN")
	})

	t.Run("state inconsistency points user at full-recover", func(t *testing.T) {
		stageSchelkBin(t, "schelk")
		// State says mounted at a path that isn't in /proc/mounts.
		stageSchelkState(t, filepath.Join(t.TempDir(), "fake-mount-point"), true)

		cfg := mkCfg("schelk", validDir)
		err := cfg.validateDataDirMethods(ValidateOpts{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "inconsistent")
		assert.Contains(t, err.Error(), "full-recover")
	})

	t.Run("not-mounted state triggers schelk mount call", func(t *testing.T) {
		// Use a shim that records the args it was called with so we can
		// confirm we called `schelk mount -y` exactly once.
		binDir := t.TempDir()
		logFile := filepath.Join(binDir, "calls.log")
		fake := filepath.Join(binDir, "schelk")
		script := fmt.Sprintf("#!/bin/sh\necho \"$@\" >> %q\nexit 0\n", logFile)
		require.NoError(t, os.WriteFile(fake, []byte(script), 0o755))
		t.Setenv("PATH", binDir)

		// State says unmounted at a path that isn't in /proc/mounts.
		stageSchelkState(t, filepath.Join(t.TempDir(), "not-mounted-anywhere"), false)

		cfg := mkCfg("schelk", validDir)
		require.NoError(t, cfg.validateDataDirMethods(ValidateOpts{}))

		logged, err := os.ReadFile(logFile)
		require.NoError(t, err)
		assert.Equal(t, "mount -y\n", string(logged))
	})
}

func TestDataDirConfig_Validate_Methods(t *testing.T) {
	validDir := t.TempDir()

	tests := []struct {
		name      string
		method    string
		wantErr   bool
		errSubstr string
	}{
		{name: "empty defaults ok", method: "", wantErr: false},
		{name: "copy", method: "copy", wantErr: false},
		{name: "overlayfs", method: "overlayfs", wantErr: false},
		{name: "fuse-overlayfs", method: "fuse-overlayfs", wantErr: false},
		{name: "zfs", method: "zfs", wantErr: false},
		{name: "direct", method: "direct", wantErr: false},
		{name: "schelk", method: "schelk", wantErr: false},
		{name: "unknown rejected", method: "bogus", wantErr: true, errSubstr: "invalid method"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dd := &DataDirConfig{SourceDir: validDir, Method: tt.method}
			err := dd.Validate("dd")

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)

				return
			}

			require.NoError(t, err)
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

func TestValidateAPIIndexing(t *testing.T) {
	// Helper to build a Config with API indexing and minimal valid fields.
	makeConfig := func(idx *APIIndexingConfig, storage APIStorageConfig) Config {
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
				Storage:  storage,
				Indexing: idx,
			},
		}
	}

	validLocalStorage := APIStorageConfig{
		Local: &APILocalStorageConfig{
			Enabled:        true,
			DiscoveryPaths: map[string]string{"results": "/tmp/results"},
		},
	}

	tests := []struct {
		name      string
		idx       *APIIndexingConfig
		storage   APIStorageConfig
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "nil indexing config is valid",
			idx:     nil,
			storage: validLocalStorage,
			wantErr: false,
		},
		{
			name:    "disabled indexing is valid",
			idx:     &APIIndexingConfig{Enabled: false},
			storage: validLocalStorage,
			wantErr: false,
		},
		{
			name: "valid sqlite indexing config",
			idx: &APIIndexingConfig{
				Enabled:  true,
				Interval: "30s",
				Database: APIDatabaseConfig{
					Driver: "sqlite",
					SQLite: SQLiteDatabaseConfig{Path: "/tmp/index.db"},
				},
			},
			storage: validLocalStorage,
			wantErr: false,
		},
		{
			name: "valid postgres indexing config",
			idx: &APIIndexingConfig{
				Enabled:  true,
				Interval: "60s",
				Database: APIDatabaseConfig{
					Driver: "postgres",
					Postgres: PostgresConfig{
						Host:     "localhost",
						Port:     5432,
						User:     "bench",
						Password: "secret",
						Database: "indexdb",
					},
				},
			},
			storage: validLocalStorage,
			wantErr: false,
		},
		{
			name: "missing database driver",
			idx: &APIIndexingConfig{
				Enabled:  true,
				Interval: "30s",
				Database: APIDatabaseConfig{
					Driver: "",
				},
			},
			storage:   validLocalStorage,
			wantErr:   true,
			errSubstr: "api.indexing.database.driver",
		},
		{
			name: "invalid database driver",
			idx: &APIIndexingConfig{
				Enabled:  true,
				Interval: "30s",
				Database: APIDatabaseConfig{
					Driver: "mysql",
				},
			},
			storage:   validLocalStorage,
			wantErr:   true,
			errSubstr: "api.indexing.database.driver",
		},
		{
			name: "missing sqlite path",
			idx: &APIIndexingConfig{
				Enabled:  true,
				Interval: "30s",
				Database: APIDatabaseConfig{
					Driver: "sqlite",
					SQLite: SQLiteDatabaseConfig{Path: ""},
				},
			},
			storage:   validLocalStorage,
			wantErr:   true,
			errSubstr: "api.indexing.database.sqlite.path is required",
		},
		{
			name: "missing postgres host",
			idx: &APIIndexingConfig{
				Enabled:  true,
				Interval: "30s",
				Database: APIDatabaseConfig{
					Driver: "postgres",
					Postgres: PostgresConfig{
						Host:     "",
						User:     "bench",
						Database: "indexdb",
					},
				},
			},
			storage:   validLocalStorage,
			wantErr:   true,
			errSubstr: "api.indexing.database.postgres.host is required",
		},
		{
			name: "invalid interval duration",
			idx: &APIIndexingConfig{
				Enabled:  true,
				Interval: "notaduration",
				Database: APIDatabaseConfig{
					Driver: "sqlite",
					SQLite: SQLiteDatabaseConfig{Path: "/tmp/index.db"},
				},
			},
			storage:   validLocalStorage,
			wantErr:   true,
			errSubstr: "api.indexing.interval: invalid duration",
		},
		{
			name: "no storage backend configured",
			idx: &APIIndexingConfig{
				Enabled:  true,
				Interval: "30s",
				Database: APIDatabaseConfig{
					Driver: "sqlite",
					SQLite: SQLiteDatabaseConfig{Path: "/tmp/index.db"},
				},
			},
			storage:   APIStorageConfig{},
			wantErr:   true,
			errSubstr: "at least one storage backend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := makeConfig(tt.idx, tt.storage)
			err := cfg.validateAPIIndexing()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGetRunTimeout(t *testing.T) {
	tests := []struct {
		name     string
		global   string
		instance string
		expected time.Duration
	}{
		{
			name:     "empty returns zero",
			global:   "",
			instance: "",
			expected: 0,
		},
		{
			name:     "global value used",
			global:   "30m",
			instance: "",
			expected: 30 * time.Minute,
		},
		{
			name:     "instance overrides global",
			global:   "30m",
			instance: "1h",
			expected: 1 * time.Hour,
		},
		{
			name:     "instance only",
			global:   "",
			instance: "45m",
			expected: 45 * time.Minute,
		},
		{
			name:     "invalid returns zero",
			global:   "not-a-duration",
			instance: "",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Runner: RunnerConfig{
					Client: ClientConfig{
						Config: ClientDefaults{
							RunTimeout: tt.global,
						},
					},
				},
			}
			instance := &ClientInstance{
				RunTimeout: tt.instance,
			}
			assert.Equal(t, tt.expected, cfg.GetRunTimeout(instance))
		})
	}
}

func TestGetRunnerRunTimeout(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected time.Duration
	}{
		{
			name:     "empty returns zero",
			value:    "",
			expected: 0,
		},
		{
			name:     "valid duration",
			value:    "4h",
			expected: 4 * time.Hour,
		},
		{
			name:     "valid minutes",
			value:    "30m",
			expected: 30 * time.Minute,
		},
		{
			name:     "invalid returns zero",
			value:    "not-a-duration",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Runner: RunnerConfig{
					RunTimeout: tt.value,
				},
			}
			assert.Equal(t, tt.expected, cfg.GetRunnerRunTimeout())
		})
	}
}

func TestValidateRunTimeout(t *testing.T) {
	tests := []struct {
		name        string
		runnerLevel string
		global      string
		instance    string
		wantErr     bool
		errSubstr   string
	}{
		{
			name:     "empty is valid",
			global:   "",
			instance: "",
		},
		{
			name:   "valid global",
			global: "30m",
		},
		{
			name:     "valid instance",
			instance: "1h",
		},
		{
			name:      "invalid global",
			global:    "bad",
			wantErr:   true,
			errSubstr: "invalid run_timeout",
		},
		{
			name:      "invalid instance",
			instance:  "bad",
			wantErr:   true,
			errSubstr: "invalid run_timeout",
		},
		{
			name:        "valid runner-level timeout",
			runnerLevel: "4h",
		},
		{
			name:        "invalid runner-level timeout",
			runnerLevel: "bad",
			wantErr:     true,
			errSubstr:   "invalid runner.run_timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Runner: RunnerConfig{
					RunTimeout: tt.runnerLevel,
					Client: ClientConfig{
						Config: ClientDefaults{
							RunTimeout: tt.global,
						},
					},
					Instances: []ClientInstance{
						{
							ID:         "test",
							Client:     "geth",
							RunTimeout: tt.instance,
						},
					},
				},
			}
			err := cfg.validateRunTimeout()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGetRetryNewPayloadsFailedState(t *testing.T) {
	tests := []struct {
		name     string
		global   *RetryNewPayloadsFailedConfig
		instance *RetryNewPayloadsFailedConfig
		expected *RetryNewPayloadsFailedConfig
	}{
		{
			name:     "both nil returns nil",
			global:   nil,
			instance: nil,
			expected: nil,
		},
		{
			name:     "global set, instance nil inherits",
			global:   &RetryNewPayloadsFailedConfig{Enabled: true, MaxRetries: 5, Backoff: "1s"},
			instance: nil,
			expected: &RetryNewPayloadsFailedConfig{Enabled: true, MaxRetries: 5, Backoff: "1s"},
		},
		{
			name:     "instance overrides global",
			global:   &RetryNewPayloadsFailedConfig{Enabled: true, MaxRetries: 5, Backoff: "1s"},
			instance: &RetryNewPayloadsFailedConfig{Enabled: true, MaxRetries: 1, Backoff: "5s"},
			expected: &RetryNewPayloadsFailedConfig{Enabled: true, MaxRetries: 1, Backoff: "5s"},
		},
		{
			name:     "instance disabled overrides global enabled",
			global:   &RetryNewPayloadsFailedConfig{Enabled: true, MaxRetries: 5, Backoff: "1s"},
			instance: &RetryNewPayloadsFailedConfig{Enabled: false},
			expected: &RetryNewPayloadsFailedConfig{Enabled: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Runner: RunnerConfig{
					Client: ClientConfig{
						Config: ClientDefaults{
							RetryNewPayloadsFailedState: tt.global,
						},
					},
				},
			}
			instance := &ClientInstance{
				RetryNewPayloadsFailedState: tt.instance,
			}
			result := cfg.GetRetryNewPayloadsFailedState(instance)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidateRetryNewPayloadsFailedState(t *testing.T) {
	tests := []struct {
		name      string
		global    *RetryNewPayloadsFailedConfig
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "disabled is valid",
			global:  &RetryNewPayloadsFailedConfig{Enabled: false},
			wantErr: false,
		},
		{
			name:    "valid enabled config",
			global:  &RetryNewPayloadsFailedConfig{Enabled: true, MaxRetries: 3, Backoff: "500ms"},
			wantErr: false,
		},
		{
			name:      "max_retries zero",
			global:    &RetryNewPayloadsFailedConfig{Enabled: true, MaxRetries: 0, Backoff: "1s"},
			wantErr:   true,
			errSubstr: "max_retries must be at least 1",
		},
		{
			name:      "missing backoff",
			global:    &RetryNewPayloadsFailedConfig{Enabled: true, MaxRetries: 3, Backoff: ""},
			wantErr:   true,
			errSubstr: "backoff is required",
		},
		{
			name:      "invalid backoff",
			global:    &RetryNewPayloadsFailedConfig{Enabled: true, MaxRetries: 3, Backoff: "not-a-duration"},
			wantErr:   true,
			errSubstr: "invalid retry_new_payloads_failed_state.backoff",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Runner: RunnerConfig{
					Client: ClientConfig{
						Config: ClientDefaults{
							RetryNewPayloadsFailedState: tt.global,
						},
					},
					Instances: []ClientInstance{
						{ID: "test", Client: "geth"},
					},
				},
			}
			err := cfg.validateRetryNewPayloadsFailedState()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateTestFilter(t *testing.T) {
	tests := []struct {
		name      string
		filter    string
		wantErr   bool
		errSubstr string
	}{
		{name: "empty is valid", filter: "", wantErr: false},
		{name: "substring is always valid", filter: "test_keccak", wantErr: false},
		{
			name:    "substring with regex metachars is valid",
			filter:  "test.*name[0]",
			wantErr: false,
		},
		{
			name:    "regex prefix with valid expression",
			filter:  "regex:test_sstore_bloated.*benchmark_300M",
			wantErr: false,
		},
		{
			name:    "regex prefix with empty expression",
			filter:  "regex:",
			wantErr: false,
		},
		{
			name:      "regex prefix with unclosed character class",
			filter:    "regex:[unclosed",
			wantErr:   true,
			errSubstr: "invalid regex",
		},
		{
			name:      "regex prefix with invalid quantifier",
			filter:    "regex:*invalid",
			wantErr:   true,
			errSubstr: "invalid regex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Runner: RunnerConfig{
					Benchmark: BenchmarkConfig{
						Tests: TestsConfig{Filter: tt.filter},
					},
				},
			}
			err := cfg.validateTestFilter()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGetOpcodeExtraction(t *testing.T) {
	tests := []struct {
		name     string
		global   *OpcodeExtractionConfig
		instance *OpcodeExtractionConfig
		expected *OpcodeExtractionConfig
	}{
		{name: "both nil returns nil", global: nil, instance: nil, expected: nil},
		{
			name:     "global set, instance nil inherits",
			global:   &OpcodeExtractionConfig{Enabled: true, Timeout: "5m"},
			instance: nil,
			expected: &OpcodeExtractionConfig{Enabled: true, Timeout: "5m"},
		},
		{
			name:     "instance overrides global",
			global:   &OpcodeExtractionConfig{Enabled: true, Timeout: "5m"},
			instance: &OpcodeExtractionConfig{Enabled: false},
			expected: &OpcodeExtractionConfig{Enabled: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Runner: RunnerConfig{
					Client: ClientConfig{
						Config: ClientDefaults{OpcodeExtraction: tt.global},
					},
				},
			}
			instance := &ClientInstance{OpcodeExtraction: tt.instance}
			assert.Equal(t, tt.expected, cfg.GetOpcodeExtraction(instance))
		})
	}
}

func TestOpcodeExtractionConfig_EffectiveTimeout(t *testing.T) {
	def, _ := time.ParseDuration(DefaultOpcodeExtractionTimeout)

	tests := []struct {
		name     string
		cfg      *OpcodeExtractionConfig
		expected time.Duration
	}{
		{name: "nil returns default", cfg: nil, expected: def},
		{name: "empty returns default", cfg: &OpcodeExtractionConfig{Timeout: ""}, expected: def},
		{name: "invalid returns default", cfg: &OpcodeExtractionConfig{Timeout: "not-a-duration"}, expected: def},
		{name: "valid returns parsed", cfg: &OpcodeExtractionConfig{Timeout: "10m"}, expected: 10 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.cfg.EffectiveTimeout())
		})
	}
}

func TestValidateOpcodeExtraction(t *testing.T) {
	tests := []struct {
		name      string
		global    *OpcodeExtractionConfig
		wantErr   bool
		errSubstr string
	}{
		{name: "disabled is always valid", global: &OpcodeExtractionConfig{Enabled: false, Timeout: "garbage"}, wantErr: false},
		{name: "enabled with empty timeout is valid (defaults)", global: &OpcodeExtractionConfig{Enabled: true}, wantErr: false},
		{name: "enabled with valid timeout is valid", global: &OpcodeExtractionConfig{Enabled: true, Timeout: "5m"}, wantErr: false},
		{
			name:      "enabled with invalid timeout",
			global:    &OpcodeExtractionConfig{Enabled: true, Timeout: "abc"},
			wantErr:   true,
			errSubstr: "invalid opcode_extraction.timeout",
		},
		{
			name:      "enabled with non-positive timeout",
			global:    &OpcodeExtractionConfig{Enabled: true, Timeout: "0s"},
			wantErr:   true,
			errSubstr: "must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Runner: RunnerConfig{
					Client: ClientConfig{
						Config: ClientDefaults{OpcodeExtraction: tt.global},
					},
					Instances: []ClientInstance{{ID: "test", Client: "geth"}},
				},
			}
			err := cfg.validateOpcodeExtraction()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateBuilder_PublicScope(t *testing.T) {
	// `benchmarkoor build` invokes Config.ValidateBuilder(), which must
	// not require any runner-side configuration (instances, test sources,
	// rollback strategies, ...) — it covers only the runner's
	// container_runtime and the builder.state_actor block.
	dir := t.TempDir()

	cfg := &Config{
		Builder: &BuilderConfig{
			StateActor: &StateActorConfig{
				Images:  map[string]string{"geth": "geth:img"},
				Targets: []StateActorTarget{{Client: "geth", OutputDir: dir, TargetSize: "5GB"}},
			},
		},
	}

	require.NoError(t, cfg.ValidateBuilder())
	require.Error(t, cfg.Validate(), "Validate() still requires runner.instances")
}

func TestValidateBuilder_PublicRejectsBadRuntime(t *testing.T) {
	cfg := &Config{
		Runner: RunnerConfig{ContainerRuntime: "lima"},
		Builder: &BuilderConfig{
			StateActor: &StateActorConfig{
				Images:  map[string]string{"geth": "geth:img"},
				Targets: []StateActorTarget{{Client: "geth", OutputDir: t.TempDir(), TargetSize: "5GB"}},
			},
		},
	}

	err := cfg.ValidateBuilder()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "container_runtime")
}

func TestValidateBuilder(t *testing.T) {
	t.Helper()

	// Use real absolute paths so the IsAbs check passes for rows where
	// the rule under test isn't about output_dir validity.
	dirA := t.TempDir()
	dirB := t.TempDir()
	intPtr := func(v int) *int { return &v }
	int64Ptr := func(v int64) *int64 { return &v }
	boolPtr := func(v bool) *bool { return &v }

	// allImages covers every supported client so rows that don't care
	// about image resolution don't have to spell it out.
	allImages := map[string]string{
		"geth":       "ghcr.io/ethereum/state-actor:latest",
		"reth":       "ghcr.io/ethereum/state-actor-reth:latest",
		"besu":       "ghcr.io/ethereum/state-actor-besu:latest",
		"nethermind": "ghcr.io/ethereum/state-actor-nethermind:latest",
		"ethrex":     "ghcr.io/ethereum/state-actor-ethrex:latest",
		"erigon":     "ghcr.io/ethereum/state-actor-erigon:latest",
	}

	// mkCfg builds a Config with just the builder block populated so the
	// builder validation runs in isolation. The wider Validate() expects
	// runner.instances etc., so we call validateBuilder() directly here.
	mkCfg := func(sa *StateActorConfig) *Config {
		return &Config{Builder: &BuilderConfig{StateActor: sa}}
	}

	tests := []struct {
		name      string
		sa        *StateActorConfig
		wantErr   bool
		errSubstr string
	}{
		{
			name: "nil builder is fine",
			sa:   nil,
		},
		{
			name: "no targets is fine",
			sa: &StateActorConfig{
				Images: allImages,
			},
		},
		{
			name: "invalid container_runtime",
			sa: &StateActorConfig{
				ContainerRuntime: "lima",
				Images:           allImages,
				Targets:          []StateActorTarget{{Client: "geth", OutputDir: dirA, TargetSize: "5GB"}},
			},
			wantErr:   true,
			errSubstr: "container_runtime",
		},
		{
			name: "invalid pull_policy",
			sa: &StateActorConfig{
				PullPolicy: "sometimes",
				Images:     allImages,
				Targets:    []StateActorTarget{{Client: "geth", OutputDir: dirA, TargetSize: "5GB"}},
			},
			wantErr:   true,
			errSubstr: "pull_policy",
		},
		{
			name: "pull_policy if-not-present ok",
			sa: &StateActorConfig{
				PullPolicy: "if-not-present",
				Images:     allImages,
				Targets:    []StateActorTarget{{Client: "geth", OutputDir: dirA, TargetSize: "5GB"}},
			},
		},
		{
			name: "supported client erigon",
			sa: &StateActorConfig{
				Images:  allImages,
				Targets: []StateActorTarget{{Client: "erigon", OutputDir: dirA, TargetSize: "5GB"}},
			},
		},
		{
			name: "unsupported client nimbus",
			sa: &StateActorConfig{
				Images:  allImages,
				Targets: []StateActorTarget{{Client: "nimbus", OutputDir: dirA, TargetSize: "5GB"}},
			},
			wantErr:   true,
			errSubstr: "nimbus",
		},
		{
			name: "missing output_dir",
			sa: &StateActorConfig{
				Images:  allImages,
				Targets: []StateActorTarget{{Client: "geth", TargetSize: "5GB"}},
			},
			wantErr:   true,
			errSubstr: "output_dir is required",
		},
		{
			name: "relative output_dir",
			sa: &StateActorConfig{
				Images:  allImages,
				Targets: []StateActorTarget{{Client: "geth", OutputDir: "./relative", TargetSize: "5GB"}},
			},
			wantErr:   true,
			errSubstr: "must be an absolute path",
		},
		{
			name: "duplicate output_dir",
			sa: &StateActorConfig{
				Images: allImages,
				Targets: []StateActorTarget{
					{Client: "geth", OutputDir: dirA, TargetSize: "5GB"},
					{Name: "reth-x", Client: "reth", OutputDir: dirA, TargetSize: "5GB"},
				},
			},
			wantErr:   true,
			errSubstr: "duplicates targets[0].output_dir",
		},
		{
			name: "duplicate target name",
			sa: &StateActorConfig{
				Images: allImages,
				Targets: []StateActorTarget{
					{Client: "geth", OutputDir: dirA, TargetSize: "5GB"},
					{Client: "geth", OutputDir: dirB, TargetSize: "10GB"},
				},
			},
			wantErr:   true,
			errSubstr: "duplicates targets[0]",
		},
		{
			name: "duplicate names allowed when disambiguated",
			sa: &StateActorConfig{
				Images: allImages,
				Targets: []StateActorTarget{
					{Name: "geth-5g", Client: "geth", OutputDir: dirA, TargetSize: "5GB"},
					{Name: "geth-50g", Client: "geth", OutputDir: dirB, TargetSize: "50GB"},
				},
			},
		},
		{
			name: "missing size and no top-level spec",
			sa: &StateActorConfig{
				Images:  allImages,
				Targets: []StateActorTarget{{Client: "geth", OutputDir: dirA}},
			},
			wantErr:   true,
			errSubstr: "no source resolved",
		},
		{
			name: "global config.target_size lets targets omit size",
			sa: &StateActorConfig{
				Images:  allImages,
				Config:  &StateActorClientDefaults{TargetSize: "5GB"},
				Targets: []StateActorTarget{{Client: "geth", OutputDir: dirA}},
			},
		},
		{
			name: "per-target target_size overrides global config.target_size",
			sa: &StateActorConfig{
				Images: allImages,
				Config: &StateActorClientDefaults{TargetSize: "5GB"},
				Targets: []StateActorTarget{{
					Client: "geth", OutputDir: dirA, TargetSize: "50GB",
				}},
			},
		},
		{
			name: "global config.target_size rejects invalid format",
			sa: &StateActorConfig{
				Images:  allImages,
				Config:  &StateActorClientDefaults{TargetSize: "5GG"},
				Targets: []StateActorTarget{{Client: "geth", OutputDir: dirA}},
			},
			wantErr:   true,
			errSubstr: "target_size",
		},
		{
			name: "global config.target_size and spec_file can coexist (target_size = headroom budget)",
			sa: &StateActorConfig{
				Images:   allImages,
				SpecFile: "/etc/spec.yaml",
				Config:   &StateActorClientDefaults{TargetSize: "5GB"},
				Targets:  []StateActorTarget{{Client: "geth", OutputDir: dirA}},
			},
		},
		{
			name: "per-target target_size + top-level spec_file can coexist",
			sa: &StateActorConfig{
				Images:   allImages,
				SpecFile: "/etc/spec.yaml",
				Targets:  []StateActorTarget{{Client: "geth", OutputDir: dirA, TargetSize: "5GB"}},
			},
		},
		{
			name: "top-level spec and spec_file both set",
			sa: &StateActorConfig{
				Images:   allImages,
				Spec:     "genesis: {}\n",
				SpecFile: "/etc/spec.yaml",
				Targets:  []StateActorTarget{{Client: "geth", OutputDir: dirA, TargetSize: "5GB"}},
			},
			wantErr:   true,
			errSubstr: "mutually exclusive",
		},
		{
			name: "top-level spec_file lets targets omit target_size",
			sa: &StateActorConfig{
				Images:   allImages,
				SpecFile: "/etc/spec.yaml",
				Targets:  []StateActorTarget{{Client: "geth", OutputDir: dirA}},
			},
		},
		{
			name: "top-level inline spec lets targets omit target_size",
			sa: &StateActorConfig{
				Images:  allImages,
				Spec:    "genesis:\n  chain_id: 1337\n",
				Targets: []StateActorTarget{{Client: "reth", OutputDir: dirA}},
			},
		},
		{
			name: "mixed: one target has target_size, other inherits top-level spec",
			sa: &StateActorConfig{
				Images: allImages,
				Spec:   "genesis: {}\n",
				Targets: []StateActorTarget{
					{Name: "geth-5g", Client: "geth", OutputDir: dirA, TargetSize: "5GB"},
					{Name: "reth-inherit", Client: "reth", OutputDir: dirB},
				},
			},
		},
		{
			name: "invalid target_size",
			sa: &StateActorConfig{
				Images:  allImages,
				Targets: []StateActorTarget{{Client: "geth", OutputDir: dirA, TargetSize: "5GG"}},
			},
			wantErr:   true,
			errSubstr: "target_size",
		},
		{
			name: "archive on besu rejected",
			sa: &StateActorConfig{
				Images:  allImages,
				Targets: []StateActorTarget{{Client: "besu", OutputDir: dirA, TargetSize: "5GB", Archive: boolPtr(true)}},
			},
			wantErr:   true,
			errSubstr: "archive",
		},
		{
			name: "archive on reth ok",
			sa: &StateActorConfig{
				Images:  allImages,
				Targets: []StateActorTarget{{Client: "reth", OutputDir: dirA, TargetSize: "5GB", Archive: boolPtr(true)}},
			},
		},
		{
			name: "binary_trie on reth rejected",
			sa: &StateActorConfig{
				Images:  allImages,
				Targets: []StateActorTarget{{Client: "reth", OutputDir: dirA, TargetSize: "5GB", BinaryTrie: boolPtr(true)}},
			},
			wantErr:   true,
			errSubstr: "binary_trie",
		},
		{
			name: "binary_trie on geth ok",
			sa: &StateActorConfig{
				Images:  allImages,
				Targets: []StateActorTarget{{Client: "geth", OutputDir: dirA, TargetSize: "5GB", BinaryTrie: boolPtr(true), GroupDepth: intPtr(4)}},
			},
		},
		{
			name: "group_depth without binary_trie rejected",
			sa: &StateActorConfig{
				Images:  allImages,
				Targets: []StateActorTarget{{Client: "geth", OutputDir: dirA, TargetSize: "5GB", GroupDepth: intPtr(4)}},
			},
			wantErr:   true,
			errSubstr: "binary_trie=true",
		},
		{
			name: "group_depth out of range low",
			sa: &StateActorConfig{
				Images:  allImages,
				Targets: []StateActorTarget{{Client: "geth", OutputDir: dirA, TargetSize: "5GB", BinaryTrie: boolPtr(true), GroupDepth: intPtr(0)}},
			},
			wantErr:   true,
			errSubstr: "1..8",
		},
		{
			name: "group_depth out of range high",
			sa: &StateActorConfig{
				Images:  allImages,
				Targets: []StateActorTarget{{Client: "geth", OutputDir: dirA, TargetSize: "5GB", BinaryTrie: boolPtr(true), GroupDepth: intPtr(9)}},
			},
			wantErr:   true,
			errSubstr: "1..8",
		},
		{
			name: "no image configured for target's client",
			sa: &StateActorConfig{
				Images:  map[string]string{"geth": "geth:img"},
				Targets: []StateActorTarget{{Client: "reth", OutputDir: dirA, TargetSize: "5GB"}},
			},
			wantErr:   true,
			errSubstr: "no image configured",
		},
		{
			name: "per-client image resolves",
			sa: &StateActorConfig{
				Images:  map[string]string{"besu": "besu:img"},
				Targets: []StateActorTarget{{Client: "besu", OutputDir: dirA, TargetSize: "5GB"}},
			},
		},
		{
			name: "spec_file passthrough ok (file existence deferred)",
			sa: &StateActorConfig{
				Images:   allImages,
				SpecFile: "/nonexistent/spec.yaml",
				Targets:  []StateActorTarget{{Client: "reth", OutputDir: dirA}},
			},
		},
		{
			name: "full pointer values pass",
			sa: &StateActorConfig{
				Images: allImages,
				Targets: []StateActorTarget{{
					Client: "geth", OutputDir: dirA, TargetSize: "5GB",
					Seed: int64Ptr(42), Fork: "prague", ChainID: int64Ptr(1337),
				}},
			},
		},
		{
			name: "global archive applies to besu target via resolution",
			sa: &StateActorConfig{
				Images:  allImages,
				Config:  &StateActorClientDefaults{Archive: boolPtr(true)},
				Targets: []StateActorTarget{{Client: "besu", OutputDir: dirA, TargetSize: "5GB"}},
			},
			wantErr:   true,
			errSubstr: "archive",
		},
		{
			name: "target archive=false overrides global archive=true on besu",
			sa: &StateActorConfig{
				Images: allImages,
				Config: &StateActorClientDefaults{Archive: boolPtr(true)},
				Targets: []StateActorTarget{{
					Client: "besu", OutputDir: dirA, TargetSize: "5GB",
					Archive: boolPtr(false),
				}},
			},
		},
		{
			name: "global binary_trie rejected on reth",
			sa: &StateActorConfig{
				Images:  allImages,
				Config:  &StateActorClientDefaults{BinaryTrie: boolPtr(true)},
				Targets: []StateActorTarget{{Client: "reth", OutputDir: dirA, TargetSize: "5GB"}},
			},
			wantErr:   true,
			errSubstr: "binary_trie",
		},
		{
			name: "global group_depth requires effective binary_trie",
			sa: &StateActorConfig{
				Images:  allImages,
				Config:  &StateActorClientDefaults{GroupDepth: intPtr(4)},
				Targets: []StateActorTarget{{Client: "geth", OutputDir: dirA, TargetSize: "5GB"}},
			},
			wantErr:   true,
			errSubstr: "binary_trie=true",
		},
		{
			name: "global binary_trie + group_depth on geth ok",
			sa: &StateActorConfig{
				Images: allImages,
				Config: &StateActorClientDefaults{BinaryTrie: boolPtr(true), GroupDepth: intPtr(4)},
				Targets: []StateActorTarget{{
					Client: "geth", OutputDir: dirA, TargetSize: "5GB",
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mkCfg(tt.sa).validateBuilder()

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)

				return
			}

			require.NoError(t, err)
		})
	}
}

func TestStateActorImageFor(t *testing.T) {
	sa := &StateActorConfig{
		Images: map[string]string{
			"geth": "geth-image:1",
			"reth": "reth-image:2",
		},
	}

	assert.Equal(t, "geth-image:1", sa.ImageFor("geth"))
	assert.Equal(t, "reth-image:2", sa.ImageFor("reth"))
	assert.Empty(t, sa.ImageFor("besu"))

	empty := &StateActorConfig{}
	assert.Empty(t, empty.ImageFor("geth"))
}

func TestStateActorTargetEffectiveName(t *testing.T) {
	withName := &StateActorTarget{Name: "geth-5g", Client: "geth"}
	assert.Equal(t, "geth-5g", withName.EffectiveName())

	noName := &StateActorTarget{Client: "geth"}
	assert.Equal(t, "geth", noName.EffectiveName())
}

func TestGetStateActorContainerRuntime(t *testing.T) {
	t.Run("builder override wins", func(t *testing.T) {
		cfg := &Config{
			Runner:  RunnerConfig{ContainerRuntime: "docker"},
			Builder: &BuilderConfig{StateActor: &StateActorConfig{ContainerRuntime: "podman"}},
		}
		assert.Equal(t, "podman", cfg.GetStateActorContainerRuntime())
	})

	t.Run("falls back to runner runtime", func(t *testing.T) {
		cfg := &Config{
			Runner:  RunnerConfig{ContainerRuntime: "podman"},
			Builder: &BuilderConfig{StateActor: &StateActorConfig{}},
		}
		assert.Equal(t, "podman", cfg.GetStateActorContainerRuntime())
	})

	t.Run("no builder block defaults to runner runtime", func(t *testing.T) {
		cfg := &Config{Runner: RunnerConfig{ContainerRuntime: "docker"}}
		assert.Equal(t, "docker", cfg.GetStateActorContainerRuntime())
	})
}

func TestValidateEESTPayloads(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	// mkCfg builds a Config with only the eest_payloads builder block so the
	// builder validation runs in isolation.
	mkCfg := func(ep *EESTPayloadsConfig) *Config {
		return &Config{Builder: &BuilderConfig{EESTPayloads: ep}}
	}

	// base returns a minimal valid target rooted at dir.
	base := func(dir string) EESTPayloadTarget {
		return EESTPayloadTarget{
			FillerClient: "geth",
			FillerImage:  "ethpandaops/geth:master",
			SourceDir:    "/snap",
			OutputDir:    dir,
			Fork:         "Osaka",
			Tests:        []string{"tests/benchmark/compute"},
		}
	}

	dockerfile := filepath.Join(dirA, "Dockerfile.eest-filler")
	if err := os.WriteFile(dockerfile, []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		ep        *EESTPayloadsConfig
		wantErr   bool
		errSubstr string
	}{
		{
			name: "nil builder is fine",
			ep:   nil,
		},
		{
			name: "valid minimal",
			ep: &EESTPayloadsConfig{
				FillImage: "fill:latest",
				Targets:   []EESTPayloadTarget{base(dirA)},
			},
		},
		{
			name: "neither fill_image nor fill_dockerfile is valid (embedded default)",
			ep: &EESTPayloadsConfig{
				Targets: []EESTPayloadTarget{base(dirA)},
			},
		},
		{
			name: "fill_dockerfile only is valid",
			ep: &EESTPayloadsConfig{
				FillDockerfile: dockerfile,
				Targets:        []EESTPayloadTarget{base(dirA)},
			},
		},
		{
			name: "fill_dockerfile not found",
			ep: &EESTPayloadsConfig{
				FillDockerfile: filepath.Join(dirB, "missing", "Dockerfile"),
				Targets:        []EESTPayloadTarget{base(dirA)},
			},
			wantErr:   true,
			errSubstr: "fill_dockerfile",
		},
		{
			name: "eest_repo without eest_ref is valid (ref defaults)",
			ep: &EESTPayloadsConfig{
				FillImage: "fill:latest",
				EESTRepo:  "https://github.com/ethereum/execution-specs.git",
				Targets:   []EESTPayloadTarget{base(dirA)},
			},
		},
		{
			name: "eest_ref alone is valid (repo defaults)",
			ep: &EESTPayloadsConfig{
				FillImage: "fill:latest",
				EESTRef:   "v1.2.3",
				Targets:   []EESTPayloadTarget{base(dirA)},
			},
		},
		{
			name: "invalid container_runtime",
			ep: &EESTPayloadsConfig{
				ContainerRuntime: "lima",
				FillImage:        "fill:latest",
				Targets:          []EESTPayloadTarget{base(dirA)},
			},
			wantErr:   true,
			errSubstr: "container_runtime",
		},
		{
			name: "unsupported filler client",
			ep: func() *EESTPayloadsConfig {
				tgt := base(dirA)
				tgt.FillerClient = "reth"

				return &EESTPayloadsConfig{FillImage: "fill:latest", Targets: []EESTPayloadTarget{tgt}}
			}(),
			wantErr:   true,
			errSubstr: "cannot act as the fill-stateful filler",
		},
		{
			name: "besu filler client is supported",
			ep: func() *EESTPayloadsConfig {
				tgt := base(dirA)
				tgt.FillerClient = "besu"

				return &EESTPayloadsConfig{FillImage: "fill:latest", Targets: []EESTPayloadTarget{tgt}}
			}(),
		},
		{
			name: "nethermind filler client is supported",
			ep: func() *EESTPayloadsConfig {
				tgt := base(dirA)
				tgt.FillerClient = "nethermind"

				return &EESTPayloadsConfig{FillImage: "fill:latest", Targets: []EESTPayloadTarget{tgt}}
			}(),
		},
		{
			name: "missing tests",
			ep: func() *EESTPayloadsConfig {
				tgt := base(dirA)
				tgt.Tests = nil

				return &EESTPayloadsConfig{FillImage: "fill:latest", Targets: []EESTPayloadTarget{tgt}}
			}(),
			wantErr:   true,
			errSubstr: "tests is required",
		},
		{
			name: "missing fork",
			ep: func() *EESTPayloadsConfig {
				tgt := base(dirA)
				tgt.Fork = ""

				return &EESTPayloadsConfig{FillImage: "fill:latest", Targets: []EESTPayloadTarget{tgt}}
			}(),
			wantErr:   true,
			errSubstr: "fork is required",
		},
		{
			name: "fork hoisted from config defaults",
			ep: func() *EESTPayloadsConfig {
				tgt := base(dirA)
				tgt.Fork = ""

				return &EESTPayloadsConfig{
					FillImage: "fill:latest",
					Config:    &EESTPayloadDefaults{Fork: "Osaka"},
					Targets:   []EESTPayloadTarget{tgt},
				}
			}(),
		},
		{
			name: "relative source_dir",
			ep: func() *EESTPayloadsConfig {
				tgt := base(dirA)
				tgt.SourceDir = "relative/path"

				return &EESTPayloadsConfig{FillImage: "fill:latest", Targets: []EESTPayloadTarget{tgt}}
			}(),
			wantErr:   true,
			errSubstr: "source_dir must be an absolute path",
		},
		{
			name: "relative output_dir",
			ep: func() *EESTPayloadsConfig {
				tgt := base("relative/out")

				return &EESTPayloadsConfig{FillImage: "fill:latest", Targets: []EESTPayloadTarget{tgt}}
			}(),
			wantErr:   true,
			errSubstr: "output_dir must be an absolute path",
		},
		{
			name: "genesis http url is valid",
			ep: func() *EESTPayloadsConfig {
				tgt := base(dirA)
				tgt.Genesis = "https://example.com/chainspec.json"

				return &EESTPayloadsConfig{FillImage: "fill:latest", Targets: []EESTPayloadTarget{tgt}}
			}(),
		},
		{
			name: "relative genesis path rejected",
			ep: func() *EESTPayloadsConfig {
				tgt := base(dirA)
				tgt.Genesis = "relative/chainspec.json"

				return &EESTPayloadsConfig{FillImage: "fill:latest", Targets: []EESTPayloadTarget{tgt}}
			}(),
			wantErr:   true,
			errSubstr: "absolute path or http(s) URL",
		},
		{
			name: "duplicate output_dir",
			ep: &EESTPayloadsConfig{
				FillImage: "fill:latest",
				Targets: []EESTPayloadTarget{
					func() EESTPayloadTarget { t := base(dirA); t.Name = "a"; return t }(),
					func() EESTPayloadTarget { t := base(dirA); t.Name = "b"; return t }(),
				},
			},
			wantErr:   true,
			errSubstr: "duplicates",
		},
		{
			name: "duplicate name",
			ep: &EESTPayloadsConfig{
				FillImage: "fill:latest",
				Targets: []EESTPayloadTarget{
					func() EESTPayloadTarget { t := base(dirA); t.Name = "dup"; return t }(),
					func() EESTPayloadTarget { t := base(dirB); t.Name = "dup"; return t }(),
				},
			},
			wantErr:   true,
			errSubstr: "duplicates",
		},
		{
			name: "invalid datadir_method",
			ep: func() *EESTPayloadsConfig {
				tgt := base(dirA)
				tgt.DataDirMethod = "btrfs"

				return &EESTPayloadsConfig{FillImage: "fill:latest", Targets: []EESTPayloadTarget{tgt}}
			}(),
			wantErr:   true,
			errSubstr: "datadir_method",
		},
		{
			name: "invalid gas_benchmark_values",
			ep: func() *EESTPayloadsConfig {
				tgt := base(dirA)
				tgt.GasBenchmarkValues = []int{10, 0}

				return &EESTPayloadsConfig{FillImage: "fill:latest", Targets: []EESTPayloadTarget{tgt}}
			}(),
			wantErr:   true,
			errSubstr: "gas_benchmark_values",
		},
		{
			name: "invalid fixed_opcode_count",
			ep: func() *EESTPayloadsConfig {
				tgt := base(dirA)
				tgt.FixedOpcodeCount = &[]float64{0.5, -1}

				return &EESTPayloadsConfig{FillImage: "fill:latest", Targets: []EESTPayloadTarget{tgt}}
			}(),
			wantErr:   true,
			errSubstr: "fixed_opcode_count",
		},
		{
			name: "gas_benchmark_values and fixed_opcode_count are mutually exclusive",
			ep: func() *EESTPayloadsConfig {
				tgt := base(dirA)
				tgt.GasBenchmarkValues = []int{10}
				tgt.FixedOpcodeCount = &[]float64{0.5}

				return &EESTPayloadsConfig{FillImage: "fill:latest", Targets: []EESTPayloadTarget{tgt}}
			}(),
			wantErr:   true,
			errSubstr: "mutually exclusive",
		},
		{
			name: "fixed_opcode_count bare (empty list) is valid",
			ep: func() *EESTPayloadsConfig {
				tgt := base(dirA)
				tgt.FixedOpcodeCount = &[]float64{}

				return &EESTPayloadsConfig{FillImage: "fill:latest", Targets: []EESTPayloadTarget{tgt}}
			}(),
			wantErr: false,
		},
		{
			name: "inline address_stubs is valid",
			ep: func() *EESTPayloadsConfig {
				tgt := base(dirA)
				tgt.AddressStubs = map[string]map[string]string{
					"bloated_eoa_10GB": {"addr": "0x87a6", "pkey": "0x4da3"},
				}

				return &EESTPayloadsConfig{FillImage: "fill:latest", Targets: []EESTPayloadTarget{tgt}}
			}(),
			wantErr: false,
		},
		{
			name: "address_stubs_file and address_stubs are mutually exclusive",
			ep: func() *EESTPayloadsConfig {
				tgt := base(dirA)
				tgt.AddressStubsFile = "/host/stubs.json"
				tgt.AddressStubs = map[string]map[string]string{
					"bloated_eoa_10GB": {"addr": "0x87a6"},
				}

				return &EESTPayloadsConfig{FillImage: "fill:latest", Targets: []EESTPayloadTarget{tgt}}
			}(),
			wantErr:   true,
			errSubstr: "mutually exclusive",
		},
		{
			name: "inline address_stubs entry without addr fails",
			ep: func() *EESTPayloadsConfig {
				tgt := base(dirA)
				tgt.AddressStubs = map[string]map[string]string{
					"bloated_eoa_10GB": {"pkey": "0x4da3"},
				}

				return &EESTPayloadsConfig{FillImage: "fill:latest", Targets: []EESTPayloadTarget{tgt}}
			}(),
			wantErr:   true,
			errSubstr: "addr is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mkCfg(tt.ep).validateBuilder()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)

				return
			}

			require.NoError(t, err)
		})
	}
}

func TestEESTPayloadsResolveTarget(t *testing.T) {
	ep := &EESTPayloadsConfig{
		Config: &EESTPayloadDefaults{
			FillerImage:        "ethpandaops/geth:master",
			Fork:               "Osaka",
			Tests:              []string{"tests/benchmark/stateful/bloatnet"},
			Filter:             "not erc20",
			Marker:             "not repricing",
			AddressStubs:       map[string]map[string]string{"bloated_eoa_10GB": {"addr": "0x87a6"}},
			GasBenchmarkValues: []int{10, 30},
			DataDirMethod:      "zfs",
			MaxGasPerTest:      u64Cfg(45000000),
			RPCSeedKey:         "0xseed",
			ExtractOpcodeCount: boolCfg(true),
			FillerExtraArgs:    []string{"--verbosity=3"},
		},
		Targets: []EESTPayloadTarget{
			// Inherits everything from config.
			{Name: "inherit", FillerClient: "geth", SourceDir: "/s", OutputDir: "/o"},
			// Overrides fork, gas values, and opts out of opcode extraction.
			{Name: "override", FillerClient: "geth", SourceDir: "/s2", OutputDir: "/o2",
				Fork: "Prague", GasBenchmarkValues: []int{60}, ExtractOpcodeCount: boolCfg(false)},
		},
	}

	inherit := ep.ResolveTarget(0)
	assert.Equal(t, "ethpandaops/geth:master", inherit.FillerImage)
	assert.Equal(t, "Osaka", inherit.Fork)
	assert.Equal(t, []string{"tests/benchmark/stateful/bloatnet"}, inherit.Tests)
	assert.Equal(t, "not erc20", inherit.Filter)
	assert.Equal(t, "not repricing", inherit.Marker)
	assert.Equal(t, map[string]map[string]string{"bloated_eoa_10GB": {"addr": "0x87a6"}}, inherit.AddressStubs)
	assert.Equal(t, []int{10, 30}, inherit.GasBenchmarkValues)
	assert.Equal(t, "zfs", inherit.DataDirMethod)
	require.NotNil(t, inherit.MaxGasPerTest)
	assert.Equal(t, uint64(45000000), *inherit.MaxGasPerTest)
	require.NotNil(t, inherit.ExtractOpcodeCount)
	assert.True(t, *inherit.ExtractOpcodeCount, "inherits extract_opcode_count when unset")
	assert.Equal(t, []string{"--verbosity=3"}, inherit.FillerExtraArgs)

	override := ep.ResolveTarget(1)
	assert.Equal(t, "Prague", override.Fork, "per-target fork wins")
	assert.Equal(t, []int{60}, override.GasBenchmarkValues, "per-target gas values win")
	require.NotNil(t, override.ExtractOpcodeCount)
	assert.False(t, *override.ExtractOpcodeCount, "per-target extract_opcode_count=false wins over default")
	assert.Equal(t, "ethpandaops/geth:master", override.FillerImage, "still inherits unset fields")
	assert.Equal(t, "not erc20", override.Filter, "inherits filter when unset")
}

// TestEESTPayloadsResolveTarget_AddressStubsUnit verifies the address-stubs
// pair is hoisted as a unit: a target setting either form inherits neither,
// preserving their mutual exclusion.
func TestEESTPayloadsResolveTarget_AddressStubsUnit(t *testing.T) {
	ep := &EESTPayloadsConfig{
		Config: &EESTPayloadDefaults{
			AddressStubs: map[string]map[string]string{"global": {"addr": "0xglobal"}},
		},
		Targets: []EESTPayloadTarget{
			// Sets only the file form: must NOT inherit the global inline map.
			{Name: "file-only", FillerClient: "geth", SourceDir: "/s", OutputDir: "/o",
				AddressStubsFile: "/host/stubs.json"},
			// Sets neither: inherits the global inline map.
			{Name: "inherit", FillerClient: "geth", SourceDir: "/s2", OutputDir: "/o2"},
		},
	}

	fileOnly := ep.ResolveTarget(0)
	assert.Equal(t, "/host/stubs.json", fileOnly.AddressStubsFile)
	assert.Empty(t, fileOnly.AddressStubs, "target with file form must not inherit global inline stubs")

	inherit := ep.ResolveTarget(1)
	assert.Empty(t, inherit.AddressStubsFile)
	assert.Equal(t, map[string]map[string]string{"global": {"addr": "0xglobal"}}, inherit.AddressStubs)
}

func TestEESTPayloadsEffectiveName(t *testing.T) {
	withName := EESTPayloadTarget{Name: "compute", FillerClient: "geth"}
	assert.Equal(t, "compute", withName.EffectiveName())

	noName := EESTPayloadTarget{FillerClient: "geth"}
	assert.Equal(t, "geth", noName.EffectiveName())
}

func TestEESTPayloadsResolveFillCommand(t *testing.T) {
	def := (&EESTPayloadsConfig{}).ResolveFillCommand()
	assert.Equal(t, []string{"uv", "run", "fill-stateful"}, def)

	custom := (&EESTPayloadsConfig{FillCommand: []string{"fill-stateful"}}).ResolveFillCommand()
	assert.Equal(t, []string{"fill-stateful"}, custom)
}

func TestGetEESTPayloadsContainerRuntime(t *testing.T) {
	t.Run("builder override wins", func(t *testing.T) {
		cfg := &Config{
			Runner:  RunnerConfig{ContainerRuntime: "docker"},
			Builder: &BuilderConfig{EESTPayloads: &EESTPayloadsConfig{ContainerRuntime: "podman"}},
		}
		assert.Equal(t, "podman", cfg.GetEESTPayloadsContainerRuntime())
	})

	t.Run("falls back to runner runtime", func(t *testing.T) {
		cfg := &Config{
			Runner:  RunnerConfig{ContainerRuntime: "podman"},
			Builder: &BuilderConfig{EESTPayloads: &EESTPayloadsConfig{}},
		}
		assert.Equal(t, "podman", cfg.GetEESTPayloadsContainerRuntime())
	})
}

func TestEESTFixturesSource_HasGenesisArtifact(t *testing.T) {
	// Fixtures-only artifact → genesis is optional (not fetched).
	assert.False(t, (&EESTFixturesSource{FixturesArtifactName: "f"}).HasGenesisArtifact())
	assert.False(t, (&EESTFixturesSource{}).HasGenesisArtifact())
	// Explicitly configured genesis → fetched.
	assert.True(t, (&EESTFixturesSource{GenesisArtifactName: "benchmark_genesis"}).HasGenesisArtifact())
	assert.True(t, (&EESTFixturesSource{GenesisArtifactRunID: "123"}).HasGenesisArtifact())
}

func u64Cfg(v uint64) *uint64 { return &v }
func boolCfg(v bool) *bool    { return &v }

func TestEESTFixturesSource_UseFixturesURL(t *testing.T) {
	// Standalone URL mode.
	assert.True(t, (&EESTFixturesSource{FixturesURL: "https://x/f.tar.gz"}).UseFixturesURL())
	// With github_release set, fixtures_url is a release-URL override, not standalone.
	assert.False(t, (&EESTFixturesSource{FixturesURL: "https://x/f.tar.gz", GitHubRelease: "v1"}).UseFixturesURL())
	assert.False(t, (&EESTFixturesSource{}).UseFixturesURL())

	// A standalone fixtures_url is a valid mode and needs no github_repo.
	require.NoError(t, (&EESTFixturesSource{
		FixturesURL: "https://x/f.tar.gz", FixturesSubdir: "sub",
	}).validate())
}
