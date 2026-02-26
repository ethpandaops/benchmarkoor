package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/api/storage"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

func setupLocalReader(t *testing.T, paths map[string]string) storage.Reader {
	t.Helper()

	cfg := &config.APILocalStorageConfig{
		Enabled:        true,
		DiscoveryPaths: paths,
	}

	return storage.NewLocalReader(cfg)
}

func TestLocalReader_DiscoveryPaths(t *testing.T) {
	t.Parallel()

	dir1 := t.TempDir()
	dir2 := t.TempDir()
	dir3 := t.TempDir()

	reader := setupLocalReader(t, map[string]string{
		"charlie": dir1,
		"alpha":   dir2,
		"bravo":   dir3,
	})

	got := reader.DiscoveryPaths()

	assert.Equal(t, []string{"alpha", "bravo", "charlie"}, got)
}

func TestLocalReader_ListRunIDs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("returns run directory names", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		runsDir := filepath.Join(dir, "runs")
		require.NoError(t, os.MkdirAll(filepath.Join(runsDir, "run-aaa"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(runsDir, "run-bbb"), 0o755))

		// Place a regular file that should be ignored (not a directory).
		require.NoError(t, os.WriteFile(
			filepath.Join(runsDir, "not-a-dir.txt"), []byte("skip"), 0o644,
		))

		reader := setupLocalReader(t, map[string]string{"dp": dir})

		ids, err := reader.ListRunIDs(ctx, "dp")
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"run-aaa", "run-bbb"}, ids)
	})

	t.Run("missing runs directory returns nil", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir() // no "runs" sub-directory created

		reader := setupLocalReader(t, map[string]string{"dp": dir})

		ids, err := reader.ListRunIDs(ctx, "dp")
		require.NoError(t, err)
		assert.Nil(t, ids)
	})
}

func TestLocalReader_GetRunFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("reads existing file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		runDir := filepath.Join(dir, "runs", "run1")
		require.NoError(t, os.MkdirAll(runDir, 0o755))

		content := []byte(`{"name":"test"}`)
		require.NoError(t, os.WriteFile(
			filepath.Join(runDir, "config.json"), content, 0o644,
		))

		reader := setupLocalReader(t, map[string]string{"dp": dir})

		data, err := reader.GetRunFile(ctx, "dp", "run1", "config.json")
		require.NoError(t, err)
		assert.Equal(t, content, data)
	})

	t.Run("missing file returns nil nil", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		runDir := filepath.Join(dir, "runs", "run1")
		require.NoError(t, os.MkdirAll(runDir, 0o755))

		reader := setupLocalReader(t, map[string]string{"dp": dir})

		data, err := reader.GetRunFile(ctx, "dp", "run1", "no-such-file.json")
		require.NoError(t, err)
		assert.Nil(t, data)
	})
}

func TestLocalReader_GetSuiteFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("reads existing file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		suiteDir := filepath.Join(dir, "suites", "hash1")
		require.NoError(t, os.MkdirAll(suiteDir, 0o755))

		content := []byte(`{"suite":"summary"}`)
		require.NoError(t, os.WriteFile(
			filepath.Join(suiteDir, "summary.json"), content, 0o644,
		))

		reader := setupLocalReader(t, map[string]string{"dp": dir})

		data, err := reader.GetSuiteFile(ctx, "dp", "hash1", "summary.json")
		require.NoError(t, err)
		assert.Equal(t, content, data)
	})

	t.Run("missing file returns nil nil", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		suiteDir := filepath.Join(dir, "suites", "hash1")
		require.NoError(t, os.MkdirAll(suiteDir, 0o755))

		reader := setupLocalReader(t, map[string]string{"dp": dir})

		data, err := reader.GetSuiteFile(ctx, "dp", "hash1", "no-such-file.json")
		require.NoError(t, err)
		assert.Nil(t, data)
	})
}

func TestLocalReader_UnknownDiscoveryPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()

	reader := setupLocalReader(t, map[string]string{"known": dir})

	t.Run("ListRunIDs", func(t *testing.T) {
		t.Parallel()

		ids, err := reader.ListRunIDs(ctx, "unknown")
		assert.Nil(t, ids)
		assert.ErrorContains(t, err, "unknown discovery path")
	})

	t.Run("GetRunFile", func(t *testing.T) {
		t.Parallel()

		data, err := reader.GetRunFile(ctx, "unknown", "run1", "f.json")
		assert.Nil(t, data)
		assert.ErrorContains(t, err, "unknown discovery path")
	})

	t.Run("GetSuiteFile", func(t *testing.T) {
		t.Parallel()

		data, err := reader.GetSuiteFile(ctx, "unknown", "hash1", "f.json")
		assert.Nil(t, data)
		assert.ErrorContains(t, err, "unknown discovery path")
	})
}
