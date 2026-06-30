package datadir

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

func pathPresent(p string) bool {
	_, err := os.Stat(p)

	return err == nil
}

func overlayFixture(t *testing.T) (base, merged, data string) {
	t.Helper()

	base, err := os.MkdirTemp(t.TempDir(), "benchmarkoor-overlay-test-")
	if err != nil {
		t.Fatal(err)
	}

	merged = filepath.Join(base, "merged")
	for _, d := range []string{merged, filepath.Join(base, "upper"), filepath.Join(base, "work")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	data = filepath.Join(merged, "underlying_data")
	if err := os.WriteFile(data, []byte("must survive"), 0o644); err != nil {
		t.Fatal(err)
	}

	return base, merged, data
}

// When the overlay is still mounted, the reaper must not delete the base
// directory, since that would recurse through the live mount.
func TestOrphanReaperSkipsRemovalWhenStillMounted(t *testing.T) {
	base, merged, data := overlayFixture(t)

	original := overlayMountCheck
	overlayMountCheck = func(path string) bool { return path == merged }
	t.Cleanup(func() { overlayMountCheck = original })

	var buf bytes.Buffer
	log := logrus.New()
	log.SetOutput(&buf)

	m := OrphanedOverlayMount{MountPoint: merged, BaseDir: base, Type: "overlayfs"}

	if err := CleanupOrphanedOverlayMounts(context.Background(), log, []OrphanedOverlayMount{m}); err != nil {
		t.Fatalf("reaper returned error: %v", err)
	}

	if !pathPresent(base) {
		t.Fatal("base dir was deleted while still mounted")
	}

	if !pathPresent(data) {
		t.Fatal("underlying data was deleted while still mounted")
	}

	if !strings.Contains(buf.String(), "still mounted") {
		t.Fatalf("expected a skip-because-mounted log, got:\n%s", buf.String())
	}
}

// When the overlay is not mounted, the reaper still cleans up the orphan.
func TestOrphanReaperRemovesWhenUnmounted(t *testing.T) {
	base, merged, _ := overlayFixture(t)

	original := overlayMountCheck
	overlayMountCheck = func(string) bool { return false }
	t.Cleanup(func() { overlayMountCheck = original })

	m := OrphanedOverlayMount{MountPoint: merged, BaseDir: base, Type: "overlayfs"}

	if err := CleanupOrphanedOverlayMounts(context.Background(), testLogger(), []OrphanedOverlayMount{m}); err != nil {
		t.Fatalf("reaper returned error: %v", err)
	}

	if pathPresent(base) {
		t.Fatal("base dir should have been removed when not mounted")
	}
}

func testLogger() logrus.FieldLogger {
	l := logrus.New()
	l.SetOutput(bytes.NewBuffer(nil))

	return l
}

// The kernel octal-escapes whitespace and backslashes in /proc/mounts. The
// still-mounted gate must decode those back to the literal path it compares
// against, otherwise it silently fails open on any temp path with a space and
// deletes through a live mount.
func TestUnescapeMountField(t *testing.T) {
	cases := map[string]string{
		"/tmp/plain/merged":              "/tmp/plain/merged",
		`/mnt/fast\040disk/bench/merged`: "/mnt/fast disk/bench/merged",
		`/mnt/a\011b/merged`:             "/mnt/a\tb/merged",
		`/mnt/a\012b/merged`:             "/mnt/a\nb/merged",
		`/mnt/back\134slash/merged`:      `/mnt/back\slash/merged`,
		`/mnt/two\040\040spaces/merged`:  "/mnt/two  spaces/merged",
		`/mnt/trailing\back`:             `/mnt/trailing\back`, // not a 3-octal-digit escape
	}

	for in, want := range cases {
		if got := unescapeMountField(in); got != want {
			t.Errorf("unescapeMountField(%q) = %q, want %q", in, got, want)
		}
	}
}
