package builder

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildFillArgs(t *testing.T) {
	spec := client.NewGethSpec()
	prefix := []string{"uv", "run", "fill-stateful"}

	tests := []struct {
		name        string
		target      *config.EESTPayloadTarget
		wantContain []string
		wantAbsent  []string
	}{
		{
			name: "minimal",
			target: &config.EESTPayloadTarget{
				FillerClient: "geth",
				Fork:         "Osaka",
				Tests:        []string{"tests/benchmark/compute"},
			},
			wantContain: []string{
				"uv", "run", "fill-stateful", "-v",
				"--rpc-endpoint=http://10.0.0.5:8545",
				"--engine-endpoint=http://10.0.0.5:8551",
				"--engine-jwt-secret-file=" + fillJWTPath,
				"--fork=Osaka",
				"--snapshot-block=0xabc",
				"--output=" + fillOutputPath,
				"tests/benchmark/compute",
			},
			wantAbsent: []string{
				"--clean", "--gas-benchmark-values", "--fixed-opcode-count", "--max-gas-per-test",
				"--rpc-seed-key", "--address-stubs", "-k",
			},
		},
		{
			name: "fixed opcode count values",
			target: &config.EESTPayloadTarget{
				FillerClient:     "geth",
				Fork:             "Osaka",
				FixedOpcodeCount: &[]float64{0.5, 1, 2},
				Tests:            []string{"tests/benchmark/compute"},
			},
			wantContain: []string{"--fixed-opcode-count=0.5,1,2"},
			wantAbsent:  []string{"--gas-benchmark-values"},
		},
		{
			name: "fixed opcode count bare (default json)",
			target: &config.EESTPayloadTarget{
				FillerClient:     "geth",
				Fork:             "Osaka",
				FixedOpcodeCount: &[]float64{},
				Tests:            []string{"tests/benchmark/compute"},
			},
			wantContain: []string{"--fixed-opcode-count"},
			wantAbsent:  []string{"--fixed-opcode-count=", "--gas-benchmark-values"},
		},
		{
			name: "full",
			target: &config.EESTPayloadTarget{
				FillerClient:       "geth",
				Fork:               "Osaka",
				GasBenchmarkValues: []int{10, 30},
				MaxGasPerTest:      u64(45000000),
				RPCSeedKey:         "0xdead",
				AddressStubsFile:   "/host/stubs.json",
				Tests:              []string{"tests/benchmark/compute", "tests/benchmark/stateful"},
				Filter:             "bn128",
			},
			wantContain: []string{
				"--gas-benchmark-values=10,30",
				"--max-gas-per-test=45000000",
				"--rpc-seed-key=0xdead",
				"--address-stubs=" + fillStubsPath,
				"tests/benchmark/compute",
				"tests/benchmark/stateful",
				"-k", "bn128",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildFillArgs(prefix, tt.target, "10.0.0.5", spec, "0xabc")

			for _, want := range tt.wantContain {
				assert.Contains(t, got, want)
			}

			for _, absent := range tt.wantAbsent {
				for _, arg := range got {
					assert.NotContains(t, arg, absent)
				}
			}
		})
	}

	// The -k filter value must immediately follow the -k flag.
	got := buildFillArgs(prefix, &config.EESTPayloadTarget{
		FillerClient: "geth", Fork: "Osaka", Tests: []string{"t"}, Filter: "expr",
	}, "1.2.3.4", spec, "0x1")
	idx := slices.Index(got, "-k")
	require.GreaterOrEqual(t, idx, 0, "-k flag must be present")
	require.Less(t, idx+1, len(got))
	assert.Equal(t, "expr", got[idx+1])
}

func TestFillerGethCommand(t *testing.T) {
	spec := client.NewGethSpec()

	t.Run("without genesis", func(t *testing.T) {
		cmd := fillerGethCommand(&config.EESTPayloadTarget{FillerClient: "geth"}, spec)

		assert.Contains(t, cmd, "--datadir=/data")
		assert.Contains(t, cmd, "--http.api=admin,debug,eth,miner,net,txpool,web3,testing,engine")
		assert.Contains(t, cmd, "--authrpc.jwtsecret=/tmp/jwtsecret")
		assert.Contains(t, cmd, "--authrpc.port=8551")
		assert.Contains(t, cmd, "--http.port=8545")
		assert.Contains(t, cmd, "--gcmode=archive")

		for _, arg := range cmd {
			assert.NotContains(t, arg, "--override.genesis", "no genesis flag when genesis_file unset")
		}
	})

	t.Run("with genesis and extra args", func(t *testing.T) {
		cmd := fillerGethCommand(&config.EESTPayloadTarget{
			FillerClient:    "geth",
			GenesisFile:     "/host/genesis.json",
			FillerExtraArgs: []string{"--verbosity=5"},
		}, spec)

		assert.Contains(t, cmd, "--override.genesis=/tmp/genesis.json")
		assert.Contains(t, cmd, "--verbosity=5")
		assert.Equal(t, "--verbosity=5", cmd[len(cmd)-1], "extra args are appended last")
	})
}

func TestEESTPayloadsBuilder_Targets(t *testing.T) {
	cfg := &config.EESTPayloadsConfig{
		FillImage: "fill:latest",
		Targets: []config.EESTPayloadTarget{
			{Name: "compute", FillerClient: "geth", OutputDir: "/srv/c"},
			{FillerClient: "geth", OutputDir: "/srv/g"},
		},
	}

	b := NewEESTPayloadsBuilder(noopLogger(), cfg, "docker", &fakeMgr{}, t.TempDir())

	got := b.Targets()
	require.Len(t, got, 2)
	assert.Equal(t, TargetInfo{Name: "compute", Client: "geth", OutputDir: "/srv/c"}, got[0])
	assert.Equal(t, TargetInfo{Name: "geth", Client: "geth", OutputDir: "/srv/g"}, got[1])
}

func TestEESTPayloadsBuilder_BuildUnknownTarget(t *testing.T) {
	cfg := &config.EESTPayloadsConfig{
		FillImage: "fill:latest",
		Targets:   []config.EESTPayloadTarget{{FillerClient: "geth", OutputDir: "/srv/g"}},
	}

	b := NewEESTPayloadsBuilder(noopLogger(), cfg, "docker", &fakeMgr{}, t.TempDir())

	_, err := b.Build(context.Background(), "nope", BuildOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no target named")
}

func TestEESTPayloadsBuilder_BuildSkipsPopulatedDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "leftover"), []byte("x"), 0o644))

	// fakeMgr panics on the orchestration methods; reaching them would fail
	// the test, proving the skip path returns before any container work.
	cfg := &config.EESTPayloadsConfig{
		FillImage: "fill:latest",
		Targets: []config.EESTPayloadTarget{{
			Name: "compute", FillerClient: "geth", SourceDir: "/snap", OutputDir: dir,
			Fork: "Osaka", FillerImage: "geth:master", Tests: []string{"tests/benchmark/compute"},
		}},
	}

	b := NewEESTPayloadsBuilder(noopLogger(), cfg, "docker", &fakeMgr{}, t.TempDir())

	skipped, err := b.Build(context.Background(), "compute", BuildOptions{})
	require.NoError(t, err)
	assert.True(t, skipped, "Build should report skipped=true when output_dir is non-empty")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "leftover", entries[0].Name())
}
