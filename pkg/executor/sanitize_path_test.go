package executor

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeResultPath(t *testing.T) {
	// Short components are returned unchanged.
	short := "benchmark/stateful/bloatnet/test_x.py::test_y[fork_Amsterdam]"
	assert.Equal(t, short, sanitizeResultPath(short))

	// An over-long leaf component is truncated to the cap and suffixed with a hash.
	longLeaf := strings.Repeat("a", 400)
	name := "benchmark/bloatnet/" + longLeaf
	out := sanitizeResultPath(name)
	parts := strings.Split(out, "/")
	leaf := parts[len(parts)-1]
	assert.Len(t, leaf, maxResultPathComponent)
	assert.Equal(t, "benchmark/bloatnet/", strings.Join(parts[:len(parts)-1], "/")+"/")

	// Distinct long names map to distinct sanitized paths (hash uniqueness).
	a := sanitizeResultPath("p/" + strings.Repeat("a", 300) + "X")
	b := sanitizeResultPath("p/" + strings.Repeat("a", 300) + "Y")
	assert.NotEqual(t, a, b)

	// Deterministic.
	assert.Equal(t, out, sanitizeResultPath(name))
}

func TestResolveTestName(t *testing.T) {
	dir := t.TempDir()
	full := "benchmark/stateful/bloatnet/test_x.py::test_y[" + strings.Repeat("p", 400) + "]"
	sanitized := sanitizeResultPath(full)

	// WriteStepResults writes the marker into the (sanitized) test dir.
	testDir := dir + "/" + sanitized
	require.NoError(t, os.MkdirAll(testDir, 0o755))
	require.NoError(t, os.WriteFile(testDir+"/"+testNameMarker, []byte(full), 0o644))

	cache := map[string]string{}
	// The marker restores the full name for a shortened directory.
	assert.Equal(t, full, resolveTestName(dir, sanitized, cache))
	// Cached on second call.
	assert.Equal(t, full, resolveTestName(dir, sanitized, cache))
	// No marker → falls back to the directory path.
	assert.Equal(t, "some/dir", resolveTestName(dir, "some/dir", cache))
}
