package builder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSidecarRoundTrip(t *testing.T) {
	dir := t.TempDir()
	inputs := fingerprintInputs{"a": "1", "b": []string{"x", "y"}}

	require.NoError(t, writeBuildSidecar(dir, "state-actor", inputs))

	sc, err := readBuildSidecar(dir)
	require.NoError(t, err)
	require.NotNil(t, sc)
	assert.Equal(t, "state-actor", sc.Builder)
	assert.Equal(t, buildFingerprintSchema, sc.SchemaVersion)

	want, err := inputs.hash()
	require.NoError(t, err)
	assert.Equal(t, want, sc.Fingerprint)
}

func TestReadBuildSidecarAbsent(t *testing.T) {
	sc, err := readBuildSidecar(t.TempDir())
	require.NoError(t, err)
	assert.Nil(t, sc)
}

func TestDecideRebuild(t *testing.T) {
	base := fingerprintInputs{"fork": "osaka", "seed": 1}

	t.Run("no sidecar skips", func(t *testing.T) {
		dec, err := decideRebuild(t.TempDir(), base)
		require.NoError(t, err)
		assert.False(t, dec.rebuild)
		assert.Contains(t, dec.reason, "no build fingerprint")
	})

	t.Run("unchanged config skips", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, writeBuildSidecar(dir, "b", base))

		dec, err := decideRebuild(dir, base)
		require.NoError(t, err)
		assert.False(t, dec.rebuild)
		assert.Equal(t, "config unchanged", dec.reason)
	})

	t.Run("changed config rebuilds and reports keys", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, writeBuildSidecar(dir, "b", base))

		dec, err := decideRebuild(dir, fingerprintInputs{"fork": "amsterdam", "seed": 1})
		require.NoError(t, err)
		assert.True(t, dec.rebuild)
		assert.Equal(t, []string{"fork"}, dec.changed)
	})
}

func TestChangedInputKeys(t *testing.T) {
	old := map[string]any{"a": "1", "b": "2", "gone": "3"}
	cur := fingerprintInputs{"a": "1", "b": "changed", "added": "4"}

	assert.Equal(t, []string{"added", "b", "gone"}, changedInputKeys(old, cur))
}

func TestSha256File(t *testing.T) {
	empty, err := sha256File("")
	require.NoError(t, err)
	assert.Empty(t, empty)

	f := filepath.Join(t.TempDir(), "x")
	require.NoError(t, os.WriteFile(f, []byte("hello"), 0o600))

	h, err := sha256File(f)
	require.NoError(t, err)
	assert.NotEmpty(t, h)

	h2, err := sha256File(f)
	require.NoError(t, err)
	assert.Equal(t, h, h2, "hash of unchanged content must be stable")

	_, err = sha256File(filepath.Join(t.TempDir(), "missing"))
	require.Error(t, err)
}

func TestStateActorFingerprintInputs(t *testing.T) {
	seed := int64(1234)
	target := &config.StateActorTarget{Client: "geth", OutputDir: "/tmp/x", Fork: "osaka", Seed: &seed}

	specFile := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte("entities: []"), 0o600))

	in, err := stateActorFingerprintInputs(target, "img:1", specFile)
	require.NoError(t, err)

	args, ok := in["args"].([]string)
	require.True(t, ok)
	for _, a := range args {
		assert.NotContains(t, a, "--db=", "the volatile --db path must be excluded")
	}
	assert.NotEmpty(t, in["spec_sha256"])

	h1, err := in.hash()
	require.NoError(t, err)

	// A changed fork changes the fingerprint.
	target.Fork = "amsterdam"
	in2, err := stateActorFingerprintInputs(target, "img:1", specFile)
	require.NoError(t, err)
	h2, _ := in2.hash()
	assert.NotEqual(t, h1, h2)

	// Changing only the output_dir (i.e. the --db path) does not.
	target.Fork = "osaka"
	target.OutputDir = "/tmp/somewhere-else"
	in3, err := stateActorFingerprintInputs(target, "img:1", specFile)
	require.NoError(t, err)
	h3, _ := in3.hash()
	assert.Equal(t, h1, h3)
}

// writeTempFile writes content to a fresh temp file and returns its path.
func writeTempFile(t *testing.T, content string) string {
	t.Helper()

	p := filepath.Join(t.TempDir(), "f")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))

	return p
}

// TestStateActorFingerprint_FieldSensitivity locks fingerprint completeness:
// mutating any output-affecting input must change the hash, so dropping a field
// from the fingerprint fails a test.
func TestStateActorFingerprint_FieldSensitivity(t *testing.T) {
	seed, chain := int64(1), int64(1337)
	gas, ts := uint64(30_000_000), uint64(1000)
	arch, bt, gd := true, true, 5

	base := func() *config.StateActorTarget {
		return &config.StateActorTarget{
			Client: "geth", OutputDir: "/tmp/x", Fork: "osaka", TargetSize: "256MB",
			Seed: &seed, ChainID: &chain, GasLimit: &gas, Timestamp: &ts,
			ExtraData: "abc", Archive: &arch, BinaryTrie: &bt, GroupDepth: &gd,
		}
	}

	spec := writeTempFile(t, "spec: 1")
	spec2 := writeTempFile(t, "spec: 2")

	baseIn, err := stateActorFingerprintInputs(base(), "img:1", spec)
	require.NoError(t, err)
	baseHash, err := baseIn.hash()
	require.NoError(t, err)

	cases := []struct {
		name     string
		image    string
		specPath string
		mut      func(*config.StateActorTarget)
	}{
		{name: "client", mut: func(t *config.StateActorTarget) { t.Client = "reth" }},
		{name: "fork", mut: func(t *config.StateActorTarget) { t.Fork = "amsterdam" }},
		{name: "target_size", mut: func(t *config.StateActorTarget) { t.TargetSize = "512MB" }},
		{name: "seed", mut: func(t *config.StateActorTarget) { s := int64(2); t.Seed = &s }},
		{name: "chain_id", mut: func(t *config.StateActorTarget) { c := int64(2); t.ChainID = &c }},
		{name: "gas_limit", mut: func(t *config.StateActorTarget) { g := uint64(1); t.GasLimit = &g }},
		{name: "timestamp", mut: func(t *config.StateActorTarget) { v := uint64(2); t.Timestamp = &v }},
		{name: "extra_data", mut: func(t *config.StateActorTarget) { t.ExtraData = "xyz" }},
		{name: "archive", mut: func(t *config.StateActorTarget) { f := false; t.Archive = &f }},
		{name: "binary_trie", mut: func(t *config.StateActorTarget) { f := false; t.BinaryTrie = &f }},
		{name: "group_depth", mut: func(t *config.StateActorTarget) { g := 9; t.GroupDepth = &g }},
		{name: "image", image: "img:2"},
		{name: "spec_content", specPath: spec2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tgt := base()
			if tc.mut != nil {
				tc.mut(tgt)
			}

			image, specPath := "img:1", spec
			if tc.image != "" {
				image = tc.image
			}
			if tc.specPath != "" {
				specPath = tc.specPath
			}

			in, err := stateActorFingerprintInputs(tgt, image, specPath)
			require.NoError(t, err)
			h, err := in.hash()
			require.NoError(t, err)
			assert.NotEqual(t, baseHash, h, "changing %s must change the fingerprint", tc.name)
		})
	}
}

// TestEESTFingerprint_FieldSensitivity is the eest equivalent of the above.
func TestEESTFingerprint_FieldSensitivity(t *testing.T) {
	genesis1, genesis2 := writeTempFile(t, "g1"), writeTempFile(t, "g2")
	stubs1, stubs2 := writeTempFile(t, "s1"), writeTempFile(t, "s2")
	maxGas, foc := uint64(100), []float64{1, 2}

	cfg := func() *config.EESTPayloadsConfig {
		return &config.EESTPayloadsConfig{FillImage: "fill:1", EESTRepo: "https://example.com/a.git"}
	}
	base := func() *config.EESTPayloadTarget {
		return &config.EESTPayloadTarget{
			FillerClient: "besu", FillerImage: "f:1", Fork: "osaka",
			Tests: []string{"a"}, Filter: "bn128", Marker: "m",
			GasBenchmarkValues: []int{10}, FixedOpcodeCount: &foc, MaxGasPerTest: &maxGas,
			RPCSeedKey: "0x1", DataDirMethod: "copy", FillerExtraArgs: []string{"--x"},
			GenesisForkOverride: map[string]uint64{"amsterdam": 1},
			Genesis:             genesis1, AddressStubsFile: stubs1, SourceDir: "/src",
		}
	}

	b := &EESTPayloadsBuilder{cfg: cfg()}
	baseIn, err := b.eestFingerprintInputs(base(), "sha1", "srcfp1")
	require.NoError(t, err)
	baseHash, err := baseIn.hash()
	require.NoError(t, err)

	// hashOf applies target/cfg/sha/srcFP overrides and returns the hash.
	hashOf := func(t *testing.T, tgt *config.EESTPayloadTarget, c *config.EESTPayloadsConfig, sha, srcFP string) string {
		t.Helper()
		in, err := (&EESTPayloadsBuilder{cfg: c}).eestFingerprintInputs(tgt, sha, srcFP)
		require.NoError(t, err)
		h, err := in.hash()
		require.NoError(t, err)

		return h
	}

	targetCases := []struct {
		name string
		mut  func(*config.EESTPayloadTarget)
	}{
		{"filler_client", func(t *config.EESTPayloadTarget) { t.FillerClient = "geth" }},
		{"filler_image", func(t *config.EESTPayloadTarget) { t.FillerImage = "f:2" }},
		{"fork", func(t *config.EESTPayloadTarget) { t.Fork = "amsterdam" }},
		{"tests", func(t *config.EESTPayloadTarget) { t.Tests = []string{"b"} }},
		{"filter", func(t *config.EESTPayloadTarget) { t.Filter = "modexp" }},
		{"marker", func(t *config.EESTPayloadTarget) { t.Marker = "n" }},
		{"gas_benchmark_values", func(t *config.EESTPayloadTarget) { t.GasBenchmarkValues = []int{20} }},
		{"fixed_opcode_count", func(t *config.EESTPayloadTarget) { f := []float64{3}; t.FixedOpcodeCount = &f }},
		{"max_gas_per_test", func(t *config.EESTPayloadTarget) { g := uint64(200); t.MaxGasPerTest = &g }},
		{"rpc_seed_key", func(t *config.EESTPayloadTarget) { t.RPCSeedKey = "0x2" }},
		{"datadir_method", func(t *config.EESTPayloadTarget) { t.DataDirMethod = "overlayfs" }},
		{"filler_extra_args", func(t *config.EESTPayloadTarget) { t.FillerExtraArgs = []string{"--y"} }},
		{"genesis_fork_override", func(t *config.EESTPayloadTarget) { t.GenesisForkOverride = map[string]uint64{"osaka": 2} }},
		{"genesis_content", func(t *config.EESTPayloadTarget) { t.Genesis = genesis2 }},
		{"address_stubs_content", func(t *config.EESTPayloadTarget) { t.AddressStubsFile = stubs2 }},
		{"source_dir", func(t *config.EESTPayloadTarget) { t.SourceDir = "/other" }},
	}

	for _, tc := range targetCases {
		t.Run(tc.name, func(t *testing.T) {
			tgt := base()
			tc.mut(tgt)
			assert.NotEqual(t, baseHash, hashOf(t, tgt, cfg(), "sha1", "srcfp1"),
				"changing %s must change the fingerprint", tc.name)
		})
	}

	t.Run("eest_sha", func(t *testing.T) {
		assert.NotEqual(t, baseHash, hashOf(t, base(), cfg(), "sha2", "srcfp1"))
	})
	t.Run("source_fingerprint", func(t *testing.T) {
		assert.NotEqual(t, baseHash, hashOf(t, base(), cfg(), "sha1", "srcfp2"))
	})

	cfgCases := []struct {
		name string
		mut  func(*config.EESTPayloadsConfig)
	}{
		{"fill_image", func(c *config.EESTPayloadsConfig) { c.FillImage = "fill:2" }},
		{"eest_repo", func(c *config.EESTPayloadsConfig) { c.EESTRepo = "https://example.com/b.git" }},
		{"fill_command", func(c *config.EESTPayloadsConfig) { c.FillCommand = []string{"custom", "cmd"} }},
	}

	for _, tc := range cfgCases {
		t.Run(tc.name, func(t *testing.T) {
			c := cfg()
			tc.mut(c)
			assert.NotEqual(t, baseHash, hashOf(t, base(), c, "sha1", "srcfp1"),
				"changing %s must change the fingerprint", tc.name)
		})
	}
}

func TestEESTFingerprintInputs(t *testing.T) {
	b := &EESTPayloadsBuilder{cfg: &config.EESTPayloadsConfig{FillImage: "fill:1"}}
	target := &config.EESTPayloadTarget{
		FillerClient: "besu",
		Fork:         "osaka",
		Tests:        []string{"tests/benchmark/compute"},
		Filter:       "bn128",
	}

	in, err := b.eestFingerprintInputs(target, "sha1", "srcfp1")
	require.NoError(t, err)
	assert.Equal(t, "srcfp1", in["source_fingerprint"])
	assert.Equal(t, "sha1", in["eest_sha"])

	h1, err := in.hash()
	require.NoError(t, err)

	cases := []struct {
		name    string
		sha     string
		srcFP   string
		mutate  func()
		restore func()
	}{
		{name: "cascade: changed source fingerprint", sha: "sha1", srcFP: "srcfp2"},
		{name: "moved EEST ref (new SHA)", sha: "sha2", srcFP: "srcfp1"},
		{
			name: "changed filter", sha: "sha1", srcFP: "srcfp1",
			mutate:  func() { target.Filter = "modexp" },
			restore: func() { target.Filter = "bn128" },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.mutate != nil {
				tc.mutate()
				defer tc.restore()
			}

			in2, err := b.eestFingerprintInputs(target, tc.sha, tc.srcFP)
			require.NoError(t, err)
			h2, _ := in2.hash()
			assert.NotEqual(t, h1, h2)
		})
	}
}
