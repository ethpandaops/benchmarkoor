package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func u64(v uint64) *uint64 { return &v }

func TestPreRunsConfig_ResolveTarget_Hoisting(t *testing.T) {
	cfg := &PreRunsConfig{
		Config: &PreRunDefaults{
			Fork:               "amsterdam",
			FillerImage:        "geth:default",
			Tests:              []string{"tests/benchmark/stateful/bloatnet/test_setup_contracts.py"},
			RPCSeedKey:         "0x01",
			DataDirMethod:      "copy",
			GasBenchmarkValues: []int{30000},
			GasLimit:           u64(999),
			FundingAccounts:    []PreRunFundingAccount{{Address: "0xabc"}},
		},
		Targets: []PreRunTarget{
			// Inherits everything from config.
			{FillerClient: "geth", SourceDir: "/s/geth", OutputDir: "/o/geth"},
			// Overrides fork, gas_limit, tests.
			{
				FillerClient: "besu", SourceDir: "/s/besu", OutputDir: "/o/besu",
				Fork:     "osaka",
				GasLimit: u64(123),
				Tests:    []string{"tests/custom.py"},
			},
		},
	}

	got0 := cfg.ResolveTarget(0)
	assert.Equal(t, "amsterdam", got0.Fork)
	assert.Equal(t, "geth:default", got0.FillerImage)
	assert.Equal(t, []int{30000}, got0.GasBenchmarkValues)
	assert.Equal(t, uint64(999), got0.ResolveGasLimit())
	assert.Equal(t, []string{"tests/benchmark/stateful/bloatnet/test_setup_contracts.py"}, got0.Tests)
	require.Len(t, got0.FundingAccounts, 1)
	assert.Equal(t, "0xabc", got0.FundingAccounts[0].Address)

	got1 := cfg.ResolveTarget(1)
	assert.Equal(t, "osaka", got1.Fork, "per-target fork wins")
	assert.Equal(t, uint64(123), got1.ResolveGasLimit(), "per-target gas_limit wins")
	assert.Equal(t, []string{"tests/custom.py"}, got1.Tests, "per-target tests win")
}

func TestPreRunTarget_Defaults(t *testing.T) {
	var tgt PreRunTarget
	assert.Equal(t, DefaultPreRunGasLimit, tgt.ResolveGasLimit())
	assert.Equal(t, DefaultPreRunGasBumpMaxBlocks, tgt.ResolveGasBumpMaxBlocks())

	var acct PreRunFundingAccount
	assert.Equal(t, DefaultPreRunFundingAmountGwei, acct.ResolveAmountGwei())

	acct.AmountGwei = u64(7)
	assert.Equal(t, uint64(7), acct.ResolveAmountGwei())

	tgt.Name = "custom"
	assert.Equal(t, "custom", tgt.EffectiveName())
	tgt.Name = ""
	tgt.FillerClient = "geth"
	assert.Equal(t, "geth", tgt.EffectiveName())
}

func TestValidatePreRuns(t *testing.T) {
	base := func() *Config {
		return &Config{
			Builder: &BuilderConfig{
				PreRuns: &PreRunsConfig{
					ContainerRuntime: "docker",
					PullPolicy:       "always",
					Config: &PreRunDefaults{
						Fork:          "amsterdam",
						Tests:         []string{"tests/benchmark/stateful/bloatnet/test_setup_contracts.py"},
						DataDirMethod: "copy",
					},
					Targets: []PreRunTarget{
						{
							Name: "pre-run-geth", FillerClient: "geth",
							FillerImage: "geth:latest",
							SourceDir:   "/state/geth", OutputDir: "/prerun/geth",
						},
					},
				},
			},
		}
	}

	t.Run("valid", func(t *testing.T) {
		require.NoError(t, base().validatePreRuns())
	})

	t.Run("unsupported filler client", func(t *testing.T) {
		c := base()
		c.Builder.PreRuns.Targets[0].FillerClient = "reth-lighthouse"
		require.ErrorContains(t, c.validatePreRuns(), "cannot act as the fill-stateful filler")
	})

	t.Run("relative source_dir rejected", func(t *testing.T) {
		c := base()
		c.Builder.PreRuns.Targets[0].SourceDir = "relative/path"
		require.ErrorContains(t, c.validatePreRuns(), "must be an absolute path")
	})

	t.Run("missing tests rejected", func(t *testing.T) {
		c := base()
		c.Builder.PreRuns.Config.Tests = nil
		require.ErrorContains(t, c.validatePreRuns(), "tests is required")
	})

	t.Run("duplicate output_dir rejected", func(t *testing.T) {
		c := base()
		c.Builder.PreRuns.Targets = append(c.Builder.PreRuns.Targets, PreRunTarget{
			Name: "pre-run-besu", FillerClient: "besu", FillerImage: "besu:latest",
			SourceDir: "/state/besu", OutputDir: "/prerun/geth", // dup
		})
		require.ErrorContains(t, c.validatePreRuns(), "duplicates")
	})

	t.Run("zero gas_limit rejected", func(t *testing.T) {
		c := base()
		c.Builder.PreRuns.Config.GasLimit = u64(0)
		require.ErrorContains(t, c.validatePreRuns(), "gas_limit must be > 0")
	})

	t.Run("funding account without address rejected", func(t *testing.T) {
		c := base()
		c.Builder.PreRuns.Config.FundingAccounts = []PreRunFundingAccount{{}}
		require.ErrorContains(t, c.validatePreRuns(), "address is required")
	})
}

// TestRestorePreRunFillEnvKeyCasing verifies fill_env keys keep their original
// (case-sensitive) casing after load, since Viper lowercases all map keys and
// env-var names like BLOATNET_RECEIVER_CONTRACT_COUNT must survive verbatim.
func TestRestorePreRunFillEnvKeyCasing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	yaml := `
builder:
  pre_runs:
    container_runtime: docker
    pull_policy: always
    config:
      fork: amsterdam
      datadir_method: copy
      tests: [tests/benchmark/stateful/bloatnet/test_setup_contracts.py]
      fill_env:
        BLOATNET_RECEIVER_CONTRACT_COUNT: "5"
        Mixed_Case_Var: "x"
    targets:
      - name: pre-run-geth
        filler_client: geth
        filler_image: geth:latest
        source_dir: /state/geth
        output_dir: /prerun/geth
        fill_env:
          TARGET_ONLY_VAR: "y"
`
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))

	cfg, err := Load(path)
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateBuilder())

	tgt := cfg.Builder.PreRuns.ResolveTarget(0)
	// Per-target fill_env wins as a unit; assert its key casing survived.
	assert.Equal(t, map[string]string{"TARGET_ONLY_VAR": "y"}, tgt.FillEnv)

	// Config-block casing survived too (used when a target sets no fill_env).
	assert.Equal(t, "5", cfg.Builder.PreRuns.Config.FillEnv["BLOATNET_RECEIVER_CONTRACT_COUNT"])
	assert.Equal(t, "x", cfg.Builder.PreRuns.Config.FillEnv["Mixed_Case_Var"])
}

// TestLoad_PreRunsExampleConfig loads the shipped pre-runs example config and
// validates it end-to-end, so the template stays in sync with the schema.
func TestLoad_PreRunsExampleConfig(t *testing.T) {
	path := filepath.Join(
		"..", "..", "examples", "configuration",
		"config.state-actor-eest.simple.amsterdam.stateful.pre-runs.yaml",
	)

	cfg, err := Load(path)
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateBuilder())

	require.NotNil(t, cfg.Builder.PreRuns)
	// Only clients implementing testing_buildBlockV1 can act as the fill-stateful
	// filler that produces a pre-run datadir: geth, besu, nethermind.
	assert.Len(t, cfg.Builder.PreRuns.Targets, 3)

	// Env-expanded absolute output dirs and the gas-bump target resolve.
	geth := cfg.Builder.PreRuns.ResolveTarget(0)
	assert.True(t, filepath.IsAbs(geth.OutputDir), "output_dir expands to an absolute path")
	assert.Equal(t, uint64(1_000_000_000_000), geth.ResolveGasLimit())
	require.Len(t, geth.FundingAccounts, 1)

	// eest_payloads builds on the pre-run output.
	require.NotNil(t, cfg.Builder.EESTPayloads)
	ep := cfg.Builder.EESTPayloads.ResolveTarget(0)
	assert.Contains(t, ep.SourceDir, "pre-runs", "eest_payloads source_dir points at the pre-run output")
}
