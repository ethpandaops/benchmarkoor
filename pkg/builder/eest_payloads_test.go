package builder

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordEESTFillResult(t *testing.T) {
	out := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(out, ".meta"), 0o755))

	// EEST's index.json gives the generated (filled) count; pytest's json report
	// gives failed + error.
	require.NoError(t, os.WriteFile(filepath.Join(out, ".meta", "index.json"),
		[]byte(`{"test_count":34,"test_cases":[]}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(out, pytestReportFile),
		[]byte(`{"summary":{"passed":34,"failed":2,"error":1,"total":37}}`), 0o600))

	require.NoError(t, recordEESTFillResult(&config.EESTPayloadTarget{
		OutputDir: out, SourceDir: "/src/geth", FillerClient: "geth", Fork: "osaka",
	}, "27174ca1b2c3"))

	data, err := os.ReadFile(filepath.Join(out, eestFillResultFile))
	require.NoError(t, err)

	var got struct {
		SourceDir    string `json:"source_dir"`
		FillerClient string `json:"filler_client"`
		EESTSHA      string `json:"eest_sha"`
		SizeBytes    int64  `json:"size_bytes"`
		Filled       int    `json:"filled"`
		Failed       int    `json:"failed"`
	}
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, 34, got.Filled)
	assert.Equal(t, 3, got.Failed) // failed + error
	assert.Equal(t, "/src/geth", got.SourceDir)
	assert.Equal(t, "geth", got.FillerClient)
	assert.Equal(t, "27174ca1b2c3", got.EESTSHA)
	assert.Positive(t, got.SizeBytes) // sums the index.json + pytest report on disk

	// Missing reports → provenance still recorded, zero counts, no error.
	empty := t.TempDir()
	require.NoError(t, recordEESTFillResult(&config.EESTPayloadTarget{OutputDir: empty, FillerClient: "besu"}, ""))
	data, err = os.ReadFile(filepath.Join(empty, eestFillResultFile))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, 0, got.Filled)
	assert.Equal(t, 0, got.Failed)
	assert.Equal(t, "besu", got.FillerClient)
}

func TestCheckInputs_NonSchelkSource(t *testing.T) {
	// Force "no schelk configured" (absent state file) so source_dir is treated
	// as a plain directory and the schelk mount path is not taken.
	t.Setenv("SCHELK_STATE", filepath.Join(t.TempDir(), "absent.json"))

	b := &EESTPayloadsBuilder{log: noopLogger()}
	src := t.TempDir()

	require.NoError(t, b.checkInputs(context.Background(), &config.EESTPayloadTarget{SourceDir: src}))

	err := b.checkInputs(context.Background(), &config.EESTPayloadTarget{SourceDir: filepath.Join(src, "missing")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source_dir")
}

func TestEmbeddedFillDockerfile(t *testing.T) {
	require.NotEmpty(t, embeddedFillDockerfile, "Dockerfile.eest-filler must be embedded")
	body := string(embeddedFillDockerfile)
	assert.Contains(t, body, "FROM python:")
	assert.Contains(t, body, "WORKDIR /eest")
}

func TestResolveFillDockerfile(t *testing.T) {
	t.Run("configured path is used as-is with no-op cleanup", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "Custom.Dockerfile")
		require.NoError(t, os.WriteFile(path, []byte("FROM scratch\n"), 0o600))

		b := &EESTPayloadsBuilder{cfg: &config.EESTPayloadsConfig{FillDockerfile: path}}
		got, cleanup, err := b.resolveFillDockerfile()
		require.NoError(t, err)

		assert.Equal(t, path, got)

		cleanup()
		assert.FileExists(t, path, "cleanup must not remove a configured Dockerfile")
	})

	t.Run("embedded default is written to a temp context and cleaned up", func(t *testing.T) {
		b := &EESTPayloadsBuilder{cfg: &config.EESTPayloadsConfig{}}
		got, cleanup, err := b.resolveFillDockerfile()
		require.NoError(t, err)

		content, readErr := os.ReadFile(got) //nolint:gosec // path produced by the function under test
		require.NoError(t, readErr)
		assert.Equal(t, embeddedFillDockerfile, content, "temp Dockerfile must match the embedded one")

		cleanup()
		assert.NoFileExists(t, got, "cleanup must remove the embedded temp Dockerfile")
	})
}

func boolPtr(b bool) *bool { return &b }

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
				"--eoa-start=1000",
				"tests/benchmark/compute",
			},
			wantAbsent: []string{
				"--clean", "--gas-benchmark-values", "--fixed-opcode-count", "--max-gas-per-test",
				"--rpc-seed-key", "--address-stubs", "-k", "--extract-opcode-count",
			},
		},
		{
			name: "extract opcode count enabled",
			target: &config.EESTPayloadTarget{
				FillerClient:       "geth",
				Fork:               "Osaka",
				ExtractOpcodeCount: boolPtr(true),
				Tests:              []string{"tests/benchmark/compute"},
			},
			wantContain: []string{"--extract-opcode-count"},
		},
		{
			name: "extract opcode count disabled",
			target: &config.EESTPayloadTarget{
				FillerClient:       "geth",
				Fork:               "Osaka",
				ExtractOpcodeCount: boolPtr(false),
				Tests:              []string{"tests/benchmark/compute"},
			},
			wantAbsent: []string{"--extract-opcode-count"},
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
				EOAStart:           u64(500000),
				AddressStubsFile:   "/host/stubs.json",
				Tests:              []string{"tests/benchmark/compute", "tests/benchmark/stateful"},
				Filter:             "bn128",
			},
			wantContain: []string{
				"--gas-benchmark-values=10,30",
				"--max-gas-per-test=45000000",
				"--rpc-seed-key=0xdead",
				"--eoa-start=500000",
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
	assert.NotContains(t, got, "-m", "-m absent when marker is unset")

	// The -m marker value must immediately follow the -m flag.
	gotMarker := buildFillArgs(prefix, &config.EESTPayloadTarget{
		FillerClient: "geth", Fork: "Osaka", Tests: []string{"t"}, Marker: "repricing",
	}, "1.2.3.4", spec, "0x1")
	mIdx := slices.Index(gotMarker, "-m")
	require.GreaterOrEqual(t, mIdx, 0, "-m flag must be present")
	require.Less(t, mIdx+1, len(gotMarker))
	assert.Equal(t, "repricing", gotMarker[mIdx+1])
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
			Genesis:         "/host/genesis.json",
			FillerExtraArgs: []string{"--verbosity=5"},
		}, spec)

		assert.Contains(t, cmd, "--override.genesis=/tmp/genesis.json")
		assert.Contains(t, cmd, "--verbosity=5")
		assert.Equal(t, "--verbosity=5", cmd[len(cmd)-1], "extra args are appended last")
	})
}

func TestFillerCommand_Besu(t *testing.T) {
	spec := client.NewBesuSpec()

	cmd := fillerCommand(&config.EESTPayloadTarget{
		FillerClient:    "besu",
		Genesis:         "/host/besu-chainspec.json",
		FillerExtraArgs: []string{"--logging=DEBUG"},
	}, spec)

	assert.Contains(t, cmd, "--data-path=/data")
	// TESTING exposes testing_buildBlockV1 (required by fill-stateful).
	assert.Contains(t, cmd, "--rpc-http-api=ETH,NET,WEB3,TXPOOL,DEBUG,ADMIN,MINER,TESTING")
	assert.Contains(t, cmd, "--engine-jwt-secret=/tmp/jwtsecret")
	assert.Contains(t, cmd, "--engine-rpc-port=8551")
	assert.Contains(t, cmd, "--rpc-http-port=8545")
	// besu boots a snapshot only when its synchronizer can register the head.
	assert.Contains(t, cmd, "--p2p-enabled=true")
	// besu reads chainId from the genesis file, so it must be passed.
	assert.Contains(t, cmd, "--genesis-file=/tmp/genesis.json")
	assert.Equal(t, "--logging=DEBUG", cmd[len(cmd)-1], "extra args are appended last")
}

func TestFillerCommand_Nethermind(t *testing.T) {
	spec := client.NewNethermindSpec()

	cmd := fillerCommand(&config.EESTPayloadTarget{
		FillerClient: "nethermind",
		Genesis:      "/host/parity-chainspec.json",
	}, spec)

	assert.Contains(t, cmd, "--datadir=/data")
	// Point BaseDbPath at the datadir so nethermind reads the state-actor snapshot.
	assert.Contains(t, cmd, "--Init.BaseDbPath=/data")
	// Testing module exposes testing_buildBlockV1 (required by fill-stateful).
	assert.Contains(t, cmd, "--JsonRpc.EnabledModules=Eth,Net,Web3,Admin,Debug,Trace,TxPool,Subscribe,Testing")
	assert.Contains(t, cmd, "--JsonRpc.JwtSecretFile=/tmp/jwtsecret")
	assert.Contains(t, cmd, "--JsonRpc.EnginePort=8551")
	assert.Contains(t, cmd, "--JsonRpc.Port=8545")
	assert.Contains(t, cmd, "--Init.ChainSpecPath=/tmp/genesis.json")
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

	// The original content is preserved, and the skip backfills the fill-result
	// sidecar (it was missing) so the build summary can still show this target's
	// data instead of just "skipped".
	assert.FileExists(t, filepath.Join(dir, "leftover"))
	assert.FileExists(t, filepath.Join(dir, eestFillResultFile))
}

func TestMaterializeAddressStubs(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	stubs := map[string]map[string]string{
		"bloated_eoa_10GB": {
			"addr": "0x87a6314da5ac8832f6e7a176c8fb133b19f5be04",
			"pkey": "0x4da32d29f6dcffa26e09dc4e102033f2d105de1444fb893493ae703289275e0e",
		},
	}
	tgt := &config.EESTPayloadTarget{AddressStubs: stubs}

	path, cleanup, err := materializeAddressStubs(noopLogger(), tgt)
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm(), "stubs file must be container-readable")

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var got map[string]map[string]string
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, stubs, got, "materialized JSON must round-trip the inline map")

	// cleanup removes the temp file.
	cleanup()

	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "cleanup should remove the temp stubs file")
}
