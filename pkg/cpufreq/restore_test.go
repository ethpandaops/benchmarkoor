package cpufreq

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
)

func testLog() logrus.FieldLogger {
	l := logrus.New()
	l.SetOutput(io.Discard)

	return l
}

func pathExists(p string) bool {
	_, err := os.Stat(p)

	return err == nil
}

// buildFakeSysfs creates a writable stand-in for /sys/devices/system/cpu so the
// real Apply/Restore exercise actual file I/O on any OS.
func buildFakeSysfs(t *testing.T, cpus []int, governor string) string {
	t.Helper()

	base := t.TempDir()

	for _, id := range cpus {
		dir := filepath.Join(base, fmt.Sprintf("cpu%d", id), cpufreqSubdir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}

		files := map[string]string{
			scalingGovernorFile:  governor,
			scalingMinFreqFile:   "800000",
			scalingMaxFreqFile:   "3000000",
			cpuinfoMinFreqFile:   "800000",
			cpuinfoMaxFreqFile:   "3000000",
			scalingCurFreqFile:   "1500000",
			scalingAvailGovsFile: "performance schedutil powersave",
		}

		for name, val := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(val), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	return base
}

func governorOf(t *testing.T, base string, id int) string {
	t.Helper()

	g, err := getGovernor(base, id)
	if err != nil {
		t.Fatalf("reading governor for cpu%d: %v", id, err)
	}

	return g
}

// Originals must be captured for every CPU touched, even across multiple Apply
// calls with different CPU sets, so all of them are restored.
func TestApplyCapturesCPUsAcrossCalls(t *testing.T) {
	base := buildFakeSysfs(t, []int{0, 1, 2, 3}, "schedutil")
	mgr := NewManager(testLog(), t.TempDir(), base)
	ctx := context.Background()

	if err := mgr.Apply(ctx, &Config{Governor: "performance"}, []int{0, 1}); err != nil {
		t.Fatalf("Apply #1: %v", err)
	}

	if err := mgr.Apply(ctx, &Config{Governor: "performance"}, []int{2, 3}); err != nil {
		t.Fatalf("Apply #2: %v", err)
	}

	if err := mgr.Restore(ctx); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	for _, id := range []int{0, 1, 2, 3} {
		if g := governorOf(t, base, id); g != "schedutil" {
			t.Fatalf("cpu%d governor = %q, want schedutil", id, g)
		}
	}
}

// A restore that cannot write every CPU must report the failure and keep the
// recovery state so a later attempt can finish, rather than silently stranding
// CPUs in the benchmark governor.
func TestRestoreKeepsRecoveryStateOnPartialFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires non-root: the test blocks one write via chmod 0444")
	}

	base := buildFakeSysfs(t, []int{0, 1}, "schedutil")
	mgr := NewManager(testLog(), t.TempDir(), base).(*manager)
	ctx := context.Background()

	if err := mgr.Apply(ctx, &Config{Governor: "performance"}, []int{0, 1}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	stateFile := mgr.stateFile
	if stateFile == "" || !pathExists(stateFile) {
		t.Fatalf("setup: expected a recovery state file, got %q", stateFile)
	}

	// Block cpu1's governor restore.
	govPath1 := cpufreqPath(base, 1, scalingGovernorFile)
	if err := os.Chmod(govPath1, 0o444); err != nil {
		t.Fatal(err)
	}

	if err := mgr.Restore(ctx); err == nil {
		t.Fatal("Restore should report an error when a CPU cannot be restored")
	}

	// Recovery state is preserved for a retry.
	if mgr.originalSettings == nil {
		t.Fatal("originalSettings should be kept after a partial restore")
	}

	if !pathExists(stateFile) {
		t.Fatal("state file should be kept after a partial restore")
	}

	// Unblock and retry: the restore now completes and cleans up.
	if err := os.Chmod(govPath1, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := mgr.Restore(ctx); err != nil {
		t.Fatalf("retry Restore: %v", err)
	}

	if g := governorOf(t, base, 1); g != "schedutil" {
		t.Fatalf("cpu1 governor = %q after retry, want schedutil", g)
	}

	if mgr.originalSettings != nil {
		t.Fatal("originalSettings should be cleared after a full restore")
	}

	if pathExists(stateFile) {
		t.Fatal("state file should be removed after a full restore")
	}
}
