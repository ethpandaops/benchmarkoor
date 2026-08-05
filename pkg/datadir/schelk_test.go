package datadir

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
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

func TestSchelkLockHeld(t *testing.T) {
	lockErr := "Error:\n   0: Another schelk process is already running " +
		"(flock on /var/lib/schelk/schelk.lock): EAGAIN: Try again"

	assert.True(t, schelkLockHeld([]byte(lockErr)))
	assert.False(t, schelkLockHeld([]byte("Volume is already mounted")))
	assert.False(t, schelkLockHeld([]byte("no such device")))

	// Naming the lock file is not contention: an unwritable or missing lock
	// path must surface immediately rather than burn the whole retry budget.
	assert.False(t, schelkLockHeld(
		[]byte("failed to open /var/lib/schelk/schelk.lock: permission denied")))
}

func TestMountWaitingForLock_NilLoggerDoesNotPanic(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "attempts")
	installFakeSchelk(t, fmt.Sprintf(`
n=$(cat %[1]s 2>/dev/null || echo 0)
n=$((n+1)); echo $n > %[1]s
if [ "$n" -lt 2 ]; then
  echo "Another schelk process is already running (flock on /var/lib/schelk/schelk.lock): EAGAIN"
  exit 1
fi
exit 0
`, counter))

	// config.go calls EnsureSchelkMounted with a nil logger, and RunSchelk
	// documents that log may be nil — the retry path must honour that.
	require.NotPanics(t, func() {
		err := mountWaitingForLock(context.Background(), nil, "schelk", time.Millisecond)
		require.NoError(t, err)
	})
}

func TestMountWaitingForLock_RetriesUntilLockClears(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "attempts")
	// Fail with the flock error for the first two attempts, then succeed.
	installFakeSchelk(t, fmt.Sprintf(`
n=$(cat %[1]s 2>/dev/null || echo 0)
n=$((n+1)); echo $n > %[1]s
if [ "$n" -lt 3 ]; then
  echo "Another schelk process is already running (flock on /var/lib/schelk/schelk.lock): EAGAIN"
  exit 1
fi
exit 0
`, counter))

	err := mountWaitingForLock(context.Background(), logrus.New(), "schelk", time.Millisecond)
	require.NoError(t, err)

	data, readErr := os.ReadFile(counter)
	require.NoError(t, readErr)
	assert.Equal(t, "3\n", string(data), "should retry until the lock clears")
}

func TestMountWaitingForLock_NonLockErrorFailsImmediately(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "attempts")
	installFakeSchelk(t, fmt.Sprintf(`
n=$(cat %[1]s 2>/dev/null || echo 0)
echo $((n+1)) > %[1]s
echo "Volume is already mounted"
exit 1
`, counter))

	err := mountWaitingForLock(context.Background(), logrus.New(), "schelk", time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Volume is already mounted")

	data, readErr := os.ReadFile(counter)
	require.NoError(t, readErr)
	assert.Equal(t, "1\n", string(data), "non-lock errors must not retry")
}

func TestSchelkStaleDevice(t *testing.T) {
	assert.True(t, schelkStaleDevice([]byte("dm-era device 'bench_era' already exists.")))
	assert.False(t, schelkStaleDevice([]byte("Another schelk process is already running")))
	assert.False(t, schelkStaleDevice([]byte("Volume is already mounted")))
}

// installFakeDmsetup puts a dmsetup shim on PATH reporting the given open count.
func installFakeDmsetup(t *testing.T, openCount string, removeLog string) {
	t.Helper()

	dir := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
case "$*" in
  *info*) echo "%s" ;;
  *remove*) echo "$*" >> %s ;;
esac
exit 0
`, openCount, removeLog)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "dmsetup"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestRepairStaleEraDevice_RemovesWhenUnused(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(statePath, []byte(`{"mount_point":"/schelk"}`), 0o600))
	t.Setenv("SCHELK_STATE", statePath)

	removeLog := filepath.Join(t.TempDir(), "removed")
	installFakeDmsetup(t, "0", removeLog)

	require.NoError(t, repairStaleEraDevice(context.Background(), logrus.New(), "bench_era"))

	data, err := os.ReadFile(removeLog)
	require.NoError(t, err, "dmsetup remove should have been called")
	assert.Contains(t, string(data), "bench_era")
}

func TestRepairStaleEraDevice_RefusesWhenDeviceInUse(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(statePath, []byte(`{"mount_point":"/schelk"}`), 0o600))
	t.Setenv("SCHELK_STATE", statePath)

	removeLog := filepath.Join(t.TempDir(), "removed")
	installFakeDmsetup(t, "1", removeLog)

	err := repairStaleEraDevice(context.Background(), logrus.New(), "bench_era")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open handle")
	assert.NoFileExists(t, removeLog, "must not remove a device that is in use")
}

func TestRepairStaleEraDevice_RefusesWhileSchelkLockHeld(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(statePath, []byte(`{"mount_point":"/schelk"}`), 0o600))
	t.Setenv("SCHELK_STATE", statePath)

	removeLog := filepath.Join(t.TempDir(), "removed")
	installFakeDmsetup(t, "0", removeLog)

	// Hold the lock as a concurrent schelk process would.
	held, err := os.OpenFile(SchelkLockPath(), os.O_RDWR|os.O_CREATE, 0o600)
	require.NoError(t, err)
	defer func() { _ = held.Close() }()
	require.NoError(t, syscall.Flock(int(held.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))

	repairErr := repairStaleEraDevice(context.Background(), logrus.New(), "bench_era")
	require.Error(t, repairErr)
	assert.Contains(t, repairErr.Error(), "lock is held")
	assert.NoFileExists(t, removeLog, "must not remove while schelk may be running")
}
