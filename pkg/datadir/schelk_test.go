package datadir

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// installFakeSchelk writes a shell shim that runs the given body and
// wires PATH + BENCHMARKOOR_SCHELK_BIN so RunSchelk picks it up. The
// shim's PATH keeps the original system PATH appended so shell
// builtins like `sleep` remain available inside the shim.
func installFakeSchelk(t *testing.T, body string) {
	t.Helper()

	dir := t.TempDir()
	binPath := filepath.Join(dir, "schelk")
	script := fmt.Sprintf("#!/bin/sh\n%s\n", body)
	require.NoError(t, os.WriteFile(binPath, []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BENCHMARKOOR_SCHELK_BIN", binPath)
}

// writeSchelkState writes a state.json with the given mount_point and points
// SCHELK_STATE at it.
func writeSchelkState(t *testing.T, mountPoint string) {
	t.Helper()

	statePath := filepath.Join(t.TempDir(), "state.json")
	data := fmt.Sprintf(`{"mount_point":%q,"is_mounted":true}`, mountPoint)
	require.NoError(t, os.WriteFile(statePath, []byte(data), 0o600))
	t.Setenv("SCHELK_STATE", statePath)
}

func TestSchelkDir(t *testing.T) {
	t.Run("no state file is not a schelk dir", func(t *testing.T) {
		t.Setenv("SCHELK_STATE", filepath.Join(t.TempDir(), "absent.json"))

		mp, isSchelk, err := SchelkDir("/some/dir")
		require.NoError(t, err)
		assert.False(t, isSchelk)
		assert.Empty(t, mp)
	})

	t.Run("output_dir under the mount is a schelk dir", func(t *testing.T) {
		mount := t.TempDir()
		writeSchelkState(t, mount)

		mp, isSchelk, err := SchelkDir(filepath.Join(mount, "eth", "geth"))
		require.NoError(t, err)
		assert.True(t, isSchelk)
		assert.Equal(t, mount, mp)
	})

	t.Run("output_dir outside the mount is not a schelk dir", func(t *testing.T) {
		mount := t.TempDir()
		writeSchelkState(t, mount)

		mp, isSchelk, err := SchelkDir(filepath.Join(t.TempDir(), "geth"))
		require.NoError(t, err)
		assert.False(t, isSchelk)
		assert.Equal(t, mount, mp)
	})
}

func TestEnsureSchelkMounted_NoMountPoint(t *testing.T) {
	writeSchelkState(t, "")

	err := EnsureSchelkMounted(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no mount_point")
}

func TestRestoreSchelk_NoMountPoint(t *testing.T) {
	writeSchelkState(t, "")

	err := RestoreSchelk(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no mount_point")
}

func TestRestoreSchelk_NoStateFile(t *testing.T) {
	t.Setenv("SCHELK_STATE", filepath.Join(t.TempDir(), "absent.json"))

	err := RestoreSchelk(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestSchelkPromote(t *testing.T) {
	t.Run("invokes promote -y", func(t *testing.T) {
		installFakeSchelk(t, `[ "$1" = promote ] && [ "$2" = "-y" ] && exit 0 || exit 3`)
		require.NoError(t, SchelkPromote(context.Background(), nil))
	})

	t.Run("surfaces failure with output", func(t *testing.T) {
		installFakeSchelk(t, "echo nope >&2\nexit 1")

		err := SchelkPromote(context.Background(), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "promote")
		assert.Contains(t, err.Error(), "nope")
	})
}

func TestRunSchelk_HappyPath(t *testing.T) {
	installFakeSchelk(t, "echo hello\nexit 0")

	output, err := RunSchelk(context.Background(), nil, "anyarg")
	require.NoError(t, err)
	assert.Contains(t, string(output), "hello")
}

func TestRunSchelk_NonZeroExitSurfacesOutput(t *testing.T) {
	installFakeSchelk(t, "echo boom >&2\nexit 7")

	output, err := RunSchelk(context.Background(), nil, "arg")
	require.Error(t, err)
	assert.Contains(t, string(output), "boom")
}

func TestRunSchelk_FinishesWithinGraceAfterCancel(t *testing.T) {
	// Shim sleeps 200ms then exits cleanly. ctx is cancelled immediately,
	// but the 2s grace window must let schelk finish naturally — so
	// RunSchelk should return success, not a kill error.
	installFakeSchelk(t, "sleep 0.2\necho done\nexit 0")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	output, err := runSchelkWithGrace(ctx, nil, 2*time.Second, "arg")
	require.NoError(t, err)
	assert.Contains(t, string(output), "done")
}

func TestRunSchelk_KilledWhenGraceExpires(t *testing.T) {
	// Shim ignores SIGTERM and sleeps longer than the 100ms grace, so
	// only SIGKILL after grace expiry can stop it. The call should
	// return shortly after the grace window with a "did not complete"
	// error — not block for the full sleep.
	installFakeSchelk(t, "trap '' TERM\nsleep 5\nexit 0")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_, err := runSchelkWithGrace(ctx, nil, 100*time.Millisecond, "arg")
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not complete within")
	assert.Less(t, elapsed, 2*time.Second, "should return shortly after grace window")
}
