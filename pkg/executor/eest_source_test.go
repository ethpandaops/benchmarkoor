package executor

import (
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/eest"
	"github.com/stretchr/testify/assert"
)

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
