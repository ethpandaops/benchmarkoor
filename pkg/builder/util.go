package builder

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

// mountTempDir returns the system temp dir with symlinks resolved. It is the
// base for host files bind-mounted into builder containers. On macOS $TMPDIR is
// /var/folders/… where /var is a symlink to /private/var; Docker Desktop shares
// /private but not the /var alias, so bind-mounting a file at the unresolved
// path silently fails to appear in the container ("no such file or directory").
// Resolving to /private/var/… makes the mount work; on Linux it's a no-op.
func mountTempDir() string {
	if resolved, err := filepath.EvalSymlinks(os.TempDir()); err == nil {
		return resolved
	}

	return os.TempDir()
}

// isPopulated reports whether dir exists and contains at least one
// entry. A missing dir returns (false, nil).
func isPopulated(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}

		return false, fmt.Errorf("reading output_dir %q: %w", dir, err)
	}

	return len(entries) > 0, nil
}

// prepareOutputDir ensures dir exists. When force is true the directory is
// removed first; the populated check that gates skip-vs-build lives in each
// builder's Build, not here.
func prepareOutputDir(dir string, force bool) error {
	if force {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("removing existing output_dir %q: %w", dir, err)
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating output_dir %q: %w", dir, err)
	}

	return nil
}

// currentUserSpec returns the invoking process's user as a docker "uid:gid"
// string. Builder containers run as this user so the datadirs and fixtures
// they write are owned by the host user instead of root — avoiding
// permission-denied failures when a later non-root step (e.g. the datadir
// copy) reads that output.
func currentUserSpec() string {
	return fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
}

// randSuffix returns a 6-hex-character random string used to keep
// concurrent build container names unique.
func randSuffix() (string, error) {
	var b [3]byte

	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}

	return hex.EncodeToString(b[:]), nil
}

// containerStream returns an io.Writer that prefixes each line of streamed
// container output with "$emoji $TS $label | $name | " and writes it directly
// to stdout. EL-client output ("CLIE") uses 🟣 to match the client-log format
// `benchmarkoor run` uses (see pkg/runner clientLogPrefix); build-tool
// containers ("BULD", e.g. state-actor / fill-stateful) use 🟠 so they are easy
// to distinguish from the filler client streaming alongside them.
func containerStream(label, name string) io.Writer {
	return &containerStreamWriter{emoji: streamEmoji(label), label: label, name: name, w: os.Stdout}
}

// streamEmoji picks the leading emoji for a streamed-container line based on its
// label. Build-tool output gets 🟠; everything else (EL clients) keeps 🟣.
func streamEmoji(label string) string {
	if label == "BULD" {
		return "🟠"
	}

	return "🟣"
}

// ansiReset clears any ANSI color/style left set by a streamed line. Tools like
// pytest emit a bold/color sequence (e.g. the test-session header) without a
// trailing reset, which would otherwise bleed into the next line — including our
// prefix — making everything after it bold.
const ansiReset = "\x1b[0m"

type containerStreamWriter struct {
	emoji string
	label string
	name  string
	w     io.Writer
	buf   bytes.Buffer
}

func (w *containerStreamWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)

	for {
		i := bytes.IndexByte(w.buf.Bytes(), '\n')
		if i < 0 {
			break
		}

		line := w.buf.Next(i + 1)
		ts := time.Now().UTC().Format(config.LogTimestampFormat)
		msg := bytes.TrimRight(line, "\r\n")

		if _, err := fmt.Fprintf(w.w, "%s %s %s | %s | %s%s\n", w.emoji, ts, w.label, w.name, msg, ansiReset); err != nil {
			return len(p), err
		}
	}

	return len(p), nil
}

// tailBuffer is an io.Writer that retains at most `max` bytes of the most
// recent input. Useful for surfacing the trailing output of a failing
// container in the resulting error.
type tailBuffer struct {
	buf bytes.Buffer
	max int
}

func newTailBuffer(maxBytes int) *tailBuffer {
	return &tailBuffer{max: maxBytes}
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf.Write(p)

	if excess := t.buf.Len() - t.max; excess > 0 {
		t.buf.Next(excess)
	}

	return len(p), nil
}

func (t *tailBuffer) String() string {
	return t.buf.String()
}
