package fsutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyDir(t *testing.T) {
	src := t.TempDir()

	// Lay out a small tree: top-level file + a nested dir with a file.
	require.NoError(t, os.MkdirAll(filepath.Join(src, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "fixtures.ini"), []byte("hello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "assets", "style.css"), []byte("body{}"), 0o644))

	dst := filepath.Join(t.TempDir(), "copy")

	require.NoError(t, CopyDir(src, dst, nil))

	top, err := os.ReadFile(filepath.Join(dst, "fixtures.ini"))
	require.NoError(t, err)
	assert.Equal(t, "hello", string(top))

	nested, err := os.ReadFile(filepath.Join(dst, "assets", "style.css"))
	require.NoError(t, err)
	assert.Equal(t, "body{}", string(nested))
}

func TestCopyDir_CreatesMissingParents(t *testing.T) {
	src := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, "f.txt"), []byte("x"), 0o644))

	// Destination parents do not exist yet.
	dst := filepath.Join(t.TempDir(), "a", "b", "c")

	require.NoError(t, CopyDir(src, dst, nil))

	got, err := os.ReadFile(filepath.Join(dst, "f.txt"))
	require.NoError(t, err)
	assert.Equal(t, "x", string(got))
}

func TestCopyDir_MissingSource(t *testing.T) {
	err := CopyDir(filepath.Join(t.TempDir(), "does-not-exist"), t.TempDir(), nil)
	require.Error(t, err)
}
