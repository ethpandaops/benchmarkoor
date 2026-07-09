package executor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/eest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindMetaDir(t *testing.T) {
	// Nested fixtures (e.g. a build artifact): fixtures_subdir resolves to
	// <root>/a/b/blockchain_tests; .meta is a sibling at <root>/a/b/.meta.
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	require.NoError(t, os.MkdirAll(filepath.Join(nested, "blockchain_tests"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(nested, ".meta"), 0o755))
	assert.Equal(t, filepath.Join(nested, ".meta"),
		findMetaDir(filepath.Join(nested, "blockchain_tests"), root),
		"prefers the .meta sibling of the resolved fixtures dir")

	// Root-level fallback: .meta only at the fixtures-cache root.
	root2 := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root2, "fixtures", "blockchain_tests"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root2, ".meta"), 0o755))
	assert.Equal(t, filepath.Join(root2, ".meta"),
		findMetaDir(filepath.Join(root2, "fixtures", "blockchain_tests"), root2),
		"falls back to the fixtures-cache root")

	// No .meta anywhere.
	assert.Empty(t, findMetaDir(filepath.Join(t.TempDir(), "x"), t.TempDir()))
}

func TestStatefulPreRunMissing(t *testing.T) {
	tests := []struct {
		name      string
		startHash string
		snapHash  string
		want      bool
	}{
		{
			name:      "start ahead of snapshot warns",
			startHash: "0xstart",
			snapHash:  "0xsnapshot",
			want:      true,
		},
		{
			name:      "start equals snapshot is silent",
			startHash: "0xsnapshot",
			snapHash:  "0xsnapshot",
			want:      false,
		},
		{
			name:      "empty start block is silent",
			startHash: "",
			snapHash:  "0xsnapshot",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &eest.Fixture{StartBlockHash: tt.startHash, SnapshotBlockHash: tt.snapHash}
			assert.Equal(t, tt.want, statefulPreRunMissing(f))
		})
	}
}

func TestParseGitHubArtifactURL(t *testing.T) {
	owner, repo, id, ok := parseGitHubArtifactURL(
		"https://github.com/ethpandaops/benchmarkoor/actions/runs/28947560261/artifacts/8170387928",
	)
	assert.True(t, ok)
	assert.Equal(t, "ethpandaops", owner)
	assert.Equal(t, "benchmarkoor", repo)
	assert.Equal(t, "8170387928", id)

	// A plain release / tarball URL is not an artifact URL.
	_, _, _, ok = parseGitHubArtifactURL(
		"https://github.com/ethpandaops/benchmarkoor-tests/releases/download/untagged-x/fixtures.tar.gz",
	)
	assert.False(t, ok)

	_, _, _, ok = parseGitHubArtifactURL("https://example.com/fixtures.tar.gz")
	assert.False(t, ok)
}
