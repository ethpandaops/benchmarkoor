package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyStateActorFiles(t *testing.T) {
	src := t.TempDir()
	// state-actor provenance at the datadir root.
	require.NoError(t, os.WriteFile(
		filepath.Join(src, "state-actor-manifest.json"), []byte(`{"schema_version":1}`), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(src, "state-actor-spec-abc123.yaml"), []byte("entities: []\n"), 0o644))
	// Unrelated files + a dir must not be copied.
	require.NoError(t, os.WriteFile(filepath.Join(src, "geth-genesis.json"), []byte("{}"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(src, "state-actor-not-a-file"), 0o755))

	runDir := t.TempDir()

	copyStateActorFiles(logrus.New(), src, runDir, nil)

	metaDir := filepath.Join(runDir, ".state-actor")

	got, err := os.ReadFile(filepath.Join(metaDir, "state-actor-manifest.json"))
	require.NoError(t, err)
	assert.Equal(t, `{"schema_version":1}`, string(got))

	_, err = os.Stat(filepath.Join(metaDir, "state-actor-spec-abc123.yaml"))
	require.NoError(t, err)

	// Non-matching file is not copied.
	_, err = os.Stat(filepath.Join(metaDir, "geth-genesis.json"))
	assert.True(t, os.IsNotExist(err))
}

func TestCopyStateActorFiles_NoFilesNoDir(t *testing.T) {
	src := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, "geth-genesis.json"), []byte("{}"), 0o644))

	runDir := t.TempDir()

	copyStateActorFiles(logrus.New(), src, runDir, nil)

	// With no state-actor files present, the .state-actor dir is not created.
	_, err := os.Stat(filepath.Join(runDir, ".state-actor"))
	assert.True(t, os.IsNotExist(err))
}

func TestCopyStateActorFiles_MissingSource(t *testing.T) {
	// Must not panic or create anything for a missing/empty source.
	runDir := t.TempDir()

	copyStateActorFiles(logrus.New(), filepath.Join(t.TempDir(), "nope"), runDir, nil)
	copyStateActorFiles(logrus.New(), "", runDir, nil)

	_, err := os.Stat(filepath.Join(runDir, ".state-actor"))
	assert.True(t, os.IsNotExist(err))
}
