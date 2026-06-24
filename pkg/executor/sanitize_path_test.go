package executor

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
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
