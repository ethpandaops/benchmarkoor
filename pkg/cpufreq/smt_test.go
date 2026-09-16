package cpufreq

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestApplyRestoresSMTSiblings(t *testing.T) {
	base := buildFakeSysfs(t, []int{0, 1, 2}, "schedutil")
	dir := filepath.Join(base, "cpu0", "topology")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "thread_siblings_list"), []byte("0,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(testLog(), t.TempDir(), base)
	ctx := context.Background()
	if err := mgr.Apply(ctx, &Config{Governor: "performance", Frequency: "1600MHz"}, []int{0}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{0, 2} {
		if got := governorOf(t, base, id); got != "performance" {
			t.Fatalf("CPU %d governor = %s, want performance", id, got)
		}
	}
	if got := governorOf(t, base, 1); got != "schedutil" {
		t.Fatalf("unrelated CPU governor = %s", got)
	}
	if err := mgr.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{0, 1, 2} {
		if got := governorOf(t, base, id); got != "schedutil" {
			t.Fatalf("CPU %d was not restored: %s", id, got)
		}
		for file, want := range map[string]uint64{scalingMinFreqFile: 800000, scalingMaxFreqFile: 3000000} {
			got, err := readSysfsUint64(cpufreqPath(base, id, file))
			if err != nil || got != want {
				t.Fatalf("CPU %d %s = %d, error %v, want %d", id, file, got, err, want)
			}
		}
	}
}
