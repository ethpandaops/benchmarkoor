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
