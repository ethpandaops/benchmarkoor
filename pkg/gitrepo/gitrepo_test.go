package gitrepo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLooksLikeCommitHash(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{name: "full sha1", input: "e5011aa5f75d7a1722481f25408347fadfb7fd3c", expected: true},
		{name: "short hash 7 chars", input: "e5011aa", expected: true},
		{name: "uppercase hex", input: "E5011AA5F75D7A17", expected: true},
		{name: "branch name", input: "main", expected: false},
		{name: "branch with slash", input: "feature/foo", expected: false},
		{name: "tag semver", input: "v1.0.0", expected: false},
		{name: "too short 6 chars", input: "e5011a", expected: false},
		{name: "empty string", input: "", expected: false},
		{name: "hex with non-hex char", input: "e5011gg", expected: false},
		{name: "7 char all digits", input: "1234567", expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, looksLikeCommitHash(tt.input))
		})
	}
}

func TestHashRepoURLStable(t *testing.T) {
	a := hashRepoURL("https://example.com/repo.git")
	b := hashRepoURL("https://example.com/repo.git")
	c := hashRepoURL("https://example.com/other.git")
	assert.Equal(t, a, b, "same URL must hash the same")
	assert.NotEqual(t, a, c, "different URLs must differ")
	assert.Len(t, a, 16, "8-byte hex")
}

// git runs a git command in dir, failing the test on error.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

func TestCloneOrUpdate(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Build a tiny local upstream repo with a branch.
	upstream := t.TempDir()
	git(t, upstream, "init", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(upstream, "README"), []byte("hello"), 0o644))
	git(t, upstream, "add", ".")
	git(t, upstream, "commit", "-m", "init")

	cache := t.TempDir()
	ctx := context.Background()
	log := logrus.New()

	// First call clones.
	path, err := CloneOrUpdate(ctx, log, upstream, "main", cache)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(path, "README"))
	assert.Equal(t, cache, filepath.Dir(path), "checkout lives directly under the cache dir")

	sha, err := HeadSHA(ctx, path)
	require.NoError(t, err)
	assert.Len(t, sha, 40)

	// Second call reuses the same cached path.
	path2, err := CloneOrUpdate(ctx, log, upstream, "main", cache)
	require.NoError(t, err)
	assert.Equal(t, path, path2)
}
