package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestPreRunTargetInPlace(t *testing.T) {
	schelk := PreRunTarget{DataDirMethod: "schelk", SourceDir: "/schelk/snap", OutputDir: "/tmp/out"}
	assert.True(t, schelk.IsInPlace())
	assert.Equal(t, "/schelk/snap", schelk.AdvancedDir(), "schelk advances source_dir in place")

	cp := PreRunTarget{DataDirMethod: "copy", SourceDir: "/snap", OutputDir: "/tmp/out"}
	assert.False(t, cp.IsInPlace())
	assert.Equal(t, "/tmp/out", cp.AdvancedDir(), "copy advances output_dir")
}

func TestPreRunTargetBundleParentDir(t *testing.T) {
	t.Run("defaults to the advanced datadir", func(t *testing.T) {
		schelk := PreRunTarget{DataDirMethod: "schelk", SourceDir: "/schelk/snap", OutputDir: "/tmp/out"}
		assert.Equal(t, "/schelk/snap", schelk.BundleParentDir())

		cp := PreRunTarget{DataDirMethod: "copy", SourceDir: "/snap", OutputDir: "/tmp/out"}
		assert.Equal(t, "/tmp/out", cp.BundleParentDir())
	})

	// The point of bundle_dir: an in-place target's advanced datadir is the schelk
	// scratch, so a bundle written there does not survive the next restore.
	t.Run("bundle_dir overrides it for either method", func(t *testing.T) {
		schelk := PreRunTarget{
			DataDirMethod: "schelk", SourceDir: "/schelk/snap", BundleDir: "/var/bundles/geth",
		}
		assert.Equal(t, "/var/bundles/geth", schelk.BundleParentDir())
		assert.Equal(t, "/schelk/snap", schelk.AdvancedDir(), "advanced datadir is unaffected")

		cp := PreRunTarget{
			DataDirMethod: "copy", SourceDir: "/snap", OutputDir: "/tmp/out", BundleDir: "/var/bundles/geth",
		}
		assert.Equal(t, "/var/bundles/geth", cp.BundleParentDir())
	})
}

func TestPreRunTargetPredeployActivationTS(t *testing.T) {
	tests := []struct {
		name   string
		target PreRunTarget
		wantTS uint64
		wantOK bool
	}{
		{
			name:   "eip override (parity/nethermind chainspec)",
			target: PreRunTarget{Fork: "amsterdam", GenesisEIPOverride: &GenesisEIPOverride{Timestamp: 100, EIPs: []uint64{8282}}},
			wantTS: 100, wantOK: true,
		},
		{
			name:   "fork override (geth-format genesis)",
			target: PreRunTarget{Fork: "amsterdam", GenesisForkOverride: map[string]uint64{"amsterdam": 200}},
			wantTS: 200, wantOK: true,
		},
		{
			name: "eip override wins when both are set",
			target: PreRunTarget{
				Fork:                "amsterdam",
				GenesisEIPOverride:  &GenesisEIPOverride{Timestamp: 100, EIPs: []uint64{8282}},
				GenesisForkOverride: map[string]uint64{"amsterdam": 200},
			},
			wantTS: 100, wantOK: true,
		},
		{
			name:   "fork override for a different fork does not schedule the target fork",
			target: PreRunTarget{Fork: "amsterdam", GenesisForkOverride: map[string]uint64{"osaka": 200}},
			wantTS: 0, wantOK: false,
		},
		{
			name:   "eip override with no eips schedules nothing",
			target: PreRunTarget{Fork: "amsterdam", GenesisEIPOverride: &GenesisEIPOverride{Timestamp: 100}},
			wantTS: 0, wantOK: false,
		},
		{
			name:   "zero timestamp leaves no pre-fork window",
			target: PreRunTarget{Fork: "amsterdam", GenesisForkOverride: map[string]uint64{"amsterdam": 0}},
			wantTS: 0, wantOK: false,
		},
		{
			name:   "no override at all",
			target: PreRunTarget{Fork: "amsterdam"},
			wantTS: 0, wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, ok := tt.target.PredeployActivationTS()
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantTS, ts)
		})
	}
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

	// A replay target advancing a non-filler (reth) from the geth fill target's
	// bundle: valid, and the non-filler client is allowed (no fill needed).
	withReplay := func(rf string) *Config {
		c := base()
		c.Builder.PreRuns.Targets = append(c.Builder.PreRuns.Targets, PreRunTarget{
			Name: "pre-run-reth", FillerClient: "reth", FillerImage: "reth:latest",
			SourceDir: "/state/reth", OutputDir: "/prerun/reth", ReplayFrom: rf,
		})

		return c
	}

	t.Run("valid replay from earlier target", func(t *testing.T) {
		require.NoError(t, withReplay("pre-run-geth").validatePreRuns())
	})

	t.Run("valid replay from absolute path", func(t *testing.T) {
		require.NoError(t, withReplay("/some/pre_run_bundle/pre-run.request").validatePreRuns())
	})

	t.Run("replay from relative path rejected", func(t *testing.T) {
		require.ErrorContains(t, withReplay("some/relative.request").validatePreRuns(), "absolute path")
	})

	t.Run("replay from a replay target rejected", func(t *testing.T) {
		c := withReplay("pre-run-geth")
		c.Builder.PreRuns.Targets = append(c.Builder.PreRuns.Targets, PreRunTarget{
			Name: "pre-run-besu", FillerClient: "besu", FillerImage: "besu:latest",
			SourceDir: "/state/besu", OutputDir: "/prerun/besu", ReplayFrom: "pre-run-reth",
		})
		require.ErrorContains(t, c.validatePreRuns(), "is itself a replay target")
	})

	t.Run("replay from later target rejected", func(t *testing.T) {
		// geth fill target references reth replay target declared after it — but
		// reth is a replay target, so this trips the replay-target check first.
		c := base()
		c.Builder.PreRuns.Targets[0].ReplayFrom = "pre-run-reth"
		c.Builder.PreRuns.Targets = append(c.Builder.PreRuns.Targets, PreRunTarget{
			Name: "pre-run-reth", FillerClient: "reth", FillerImage: "reth:latest",
			SourceDir: "/state/reth", OutputDir: "/prerun/reth", ReplayFrom: "pre-run-geth",
		})
		require.Error(t, c.validatePreRuns())
	})

	// withPredeploy returns a valid predeploy config, mutated by fn, for the
	// osaka→amsterdam fork-crossing deployment.
	validKey := "0x" + strings.Repeat("11", 32)
	withPredeploy := func(fn func(*PreRunPredeploy, *PreRunTarget)) *Config {
		c := base()
		tgt := &c.Builder.PreRuns.Targets[0]
		tgt.GenesisEIPOverride = &GenesisEIPOverride{Timestamp: 1_800_000_000, EIPs: []uint64{7928, 8282}}
		p := &PreRunPredeploy{
			PreFork:     "osaka",
			DeployerKey: validKey,
			Contracts:   []PreRunPredeployContract{{Code: "0x60006000fd"}},
		}
		if fn != nil {
			fn(p, tgt)
		}

		tgt.Predeploy = p

		return c
	}

	t.Run("valid predeploy", func(t *testing.T) {
		require.NoError(t, withPredeploy(nil).validatePreRuns())
	})

	t.Run("schelk target without output_dir is valid (in place)", func(t *testing.T) {
		c := base()
		c.Builder.PreRuns.Config.DataDirMethod = "schelk"
		tgt := &c.Builder.PreRuns.Targets[0]
		tgt.SourceDir = "/schelk/snap"
		tgt.OutputDir = ""
		require.NoError(t, c.validatePreRuns())

		resolved := c.Builder.PreRuns.ResolveTarget(0)
		assert.True(t, resolved.IsInPlace())
		assert.Equal(t, "/schelk/snap", resolved.AdvancedDir())
	})

	t.Run("relative bundle_dir rejected", func(t *testing.T) {
		c := base()
		c.Builder.PreRuns.Targets[0].BundleDir = "relative/bundles"
		require.ErrorContains(t, c.validatePreRuns(), "bundle_dir must be an absolute path")
	})

	t.Run("absolute bundle_dir accepted", func(t *testing.T) {
		c := base()
		c.Builder.PreRuns.Targets[0].BundleDir = "/var/bundles/geth"
		require.NoError(t, c.validatePreRuns())

		resolved := c.Builder.PreRuns.ResolveTarget(0)
		assert.Equal(t, "/var/bundles/geth", resolved.BundleParentDir())
	})

	t.Run("non-schelk target still requires output_dir", func(t *testing.T) {
		c := base()
		c.Builder.PreRuns.Targets[0].OutputDir = ""
		require.ErrorContains(t, c.validatePreRuns(), "output_dir is required")
	})

	t.Run("predeploy without pre_fork rejected", func(t *testing.T) {
		c := withPredeploy(func(p *PreRunPredeploy, _ *PreRunTarget) { p.PreFork = "" })
		require.ErrorContains(t, c.validatePreRuns(), "pre_fork is required")
	})

	t.Run("predeploy with bad deployer key rejected", func(t *testing.T) {
		c := withPredeploy(func(p *PreRunPredeploy, _ *PreRunTarget) { p.DeployerKey = "0xdeadbeef" })
		require.ErrorContains(t, c.validatePreRuns(), "deployer_key")
	})

	t.Run("predeploy without contracts rejected", func(t *testing.T) {
		c := withPredeploy(func(p *PreRunPredeploy, _ *PreRunTarget) { p.Contracts = nil })
		require.ErrorContains(t, c.validatePreRuns(), "contracts is required")
	})

	t.Run("predeploy with odd-hex contract code rejected", func(t *testing.T) {
		c := withPredeploy(func(p *PreRunPredeploy, _ *PreRunTarget) {
			p.Contracts = []PreRunPredeployContract{{Code: "0x123"}}
		})
		require.ErrorContains(t, c.validatePreRuns(), "code")
	})

	// The to+data form deploys through an existing contract (a CREATE2 factory),
	// so the address follows the callee's scheme and must be declared.
	t.Run("predeploy via deployer call accepted", func(t *testing.T) {
		c := withPredeploy(func(p *PreRunPredeploy, _ *PreRunTarget) {
			p.Contracts = []PreRunPredeployContract{{
				To:      "0x4e59b44847b379578588920cA78FbF26c0B4956C",
				Data:    "0x" + strings.Repeat("ab", 64),
				Address: "0x0000bFF46984e3725691FA540a8C7589300D8282",
			}}
		})
		require.NoError(t, c.validatePreRuns())
		assert.True(t, c.Builder.PreRuns.Targets[0].Predeploy.Contracts[0].IsCall())
	})

	t.Run("predeploy deployer call without address rejected", func(t *testing.T) {
		c := withPredeploy(func(p *PreRunPredeploy, _ *PreRunTarget) {
			p.Contracts = []PreRunPredeployContract{{
				To:   "0x4e59b44847b379578588920cA78FbF26c0B4956C",
				Data: "0x" + strings.Repeat("ab", 64),
			}}
		})
		require.ErrorContains(t, c.validatePreRuns(), "address is required")
	})

	t.Run("predeploy mixing code and to rejected", func(t *testing.T) {
		c := withPredeploy(func(p *PreRunPredeploy, _ *PreRunTarget) {
			p.Contracts = []PreRunPredeployContract{{
				Code:    "0x60006000fd",
				To:      "0x4e59b44847b379578588920cA78FbF26c0B4956C",
				Data:    "0xab",
				Address: "0x0000bFF46984e3725691FA540a8C7589300D8282",
			}}
		})
		require.ErrorContains(t, c.validatePreRuns(), "not both")
	})

	t.Run("predeploy with address but no to rejected", func(t *testing.T) {
		c := withPredeploy(func(p *PreRunPredeploy, _ *PreRunTarget) {
			p.Contracts = []PreRunPredeployContract{{
				Code:    "0x60006000fd",
				Address: "0x0000bFF46984e3725691FA540a8C7589300D8282",
			}}
		})
		require.ErrorContains(t, c.validatePreRuns(), "require to")
	})

	t.Run("predeploy deployer call with malformed address rejected", func(t *testing.T) {
		c := withPredeploy(func(p *PreRunPredeploy, _ *PreRunTarget) {
			p.Contracts = []PreRunPredeployContract{{
				To:      "0x4e59b44847b379578588920cA78FbF26c0B4956C",
				Data:    "0xab",
				Address: "0xdeadbeef",
			}}
		})
		require.ErrorContains(t, c.validatePreRuns(), "40 hex char")
	})

	t.Run("predeploy without any genesis override rejected", func(t *testing.T) {
		c := withPredeploy(func(_ *PreRunPredeploy, tgt *PreRunTarget) { tgt.GenesisEIPOverride = nil })
		require.ErrorContains(t, c.validatePreRuns(), "genesis_eip_override")
	})

	t.Run("predeploy with zero activation timestamp rejected", func(t *testing.T) {
		c := withPredeploy(func(_ *PreRunPredeploy, tgt *PreRunTarget) {
			tgt.GenesisEIPOverride = &GenesisEIPOverride{Timestamp: 0, EIPs: []uint64{8282}}
		})
		require.ErrorContains(t, c.validatePreRuns(), "positive timestamp")
	})

	// genesis_fork_override is the geth-format counterpart of genesis_eip_override
	// (parity/nethermind only), so it must schedule a predeploy's target fork too.
	t.Run("predeploy with genesis_fork_override accepted", func(t *testing.T) {
		c := withPredeploy(func(_ *PreRunPredeploy, tgt *PreRunTarget) {
			tgt.GenesisEIPOverride = nil
			tgt.GenesisForkOverride = map[string]uint64{"amsterdam": 1_800_000_000}
		})
		require.NoError(t, c.validatePreRuns())

		resolved := c.Builder.PreRuns.ResolveTarget(0)
		ts, ok := resolved.PredeployActivationTS()
		assert.True(t, ok)
		assert.Equal(t, uint64(1_800_000_000), ts)
	})

	t.Run("predeploy with zero genesis_fork_override timestamp rejected", func(t *testing.T) {
		c := withPredeploy(func(_ *PreRunPredeploy, tgt *PreRunTarget) {
			tgt.GenesisEIPOverride = nil
			tgt.GenesisForkOverride = map[string]uint64{"amsterdam": 0}
		})
		require.ErrorContains(t, c.validatePreRuns(), "positive timestamp")
	})

	t.Run("predeploy with genesis_fork_override for another fork rejected", func(t *testing.T) {
		// Scheduling osaka says nothing about when the amsterdam target fork
		// activates, so there is no boundary for the deploy blocks to precede.
		c := withPredeploy(func(_ *PreRunPredeploy, tgt *PreRunTarget) {
			tgt.GenesisEIPOverride = nil
			tgt.GenesisForkOverride = map[string]uint64{"osaka": 1_800_000_000}
		})
		require.ErrorContains(t, c.validatePreRuns(), "genesis_fork_override.amsterdam")
	})

	t.Run("predeploy hoisted from config block", func(t *testing.T) {
		// Predeploy set on the config defaults, not the target, resolves onto it.
		// genesis_eip_override lives on the target (it is not a hoistable default).
		c := base()
		tgt := &c.Builder.PreRuns.Targets[0]
		tgt.GenesisEIPOverride = &GenesisEIPOverride{Timestamp: 1_800_000_000, EIPs: []uint64{8282}}
		c.Builder.PreRuns.Config.Predeploy = &PreRunPredeploy{
			PreFork: "osaka", DeployerKey: validKey,
			Contracts: []PreRunPredeployContract{{Code: "0x00"}},
		}
		require.NoError(t, c.validatePreRuns())

		resolved := c.Builder.PreRuns.ResolveTarget(0)
		require.NotNil(t, resolved.Predeploy)
		assert.Equal(t, "osaka", resolved.Predeploy.PreFork)
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
	// nethermind fills; geth/besu/reth/ethrex advance by replaying its bundle
	// (replay_from), so the runner boots every client from its own advanced datadir.
	require.Len(t, cfg.Builder.PreRuns.Targets, 5)

	nm := cfg.Builder.PreRuns.ResolveTarget(0)
	assert.Equal(t, "nethermind", nm.FillerClient)
	assert.False(t, nm.IsReplay(), "the first target fills")

	// Every other target replays nethermind's bundle.
	for i := 1; i < len(cfg.Builder.PreRuns.Targets); i++ {
		rt := cfg.Builder.PreRuns.ResolveTarget(i)
		assert.Truef(t, rt.IsReplay(), "target %d (%s) is a replay target", i, rt.FillerClient)
		assert.Equalf(t, "pre-run-nethermind", rt.ReplayFrom, "target %d replays nethermind", i)
	}

	// Env-expanded absolute output dirs and the gas-bump target resolve.
	assert.True(t, filepath.IsAbs(nm.OutputDir), "output_dir expands to an absolute path")
	assert.Equal(t, uint64(1_000_000_000_000), nm.ResolveGasLimit())
	require.Len(t, nm.FundingAccounts, 1)

	// eest_payloads builds on the pre-run output.
	require.NotNil(t, cfg.Builder.EESTPayloads)
	ep := cfg.Builder.EESTPayloads.ResolveTarget(0)
	assert.Contains(t, ep.SourceDir, "pre-runs", "eest_payloads source_dir points at the pre-run output")
}
