package builder

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/sirupsen/logrus"
)

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

// logWriter returns an io.Writer that forwards each line written to it to
// the supplied logger at the given level. Used to stream container
// stdout/stderr without writing the bytes directly to the global
// stdout/stderr.
func logWriter(log logrus.FieldLogger, level logrus.Level) io.Writer {
	return &lineLogger{log: log, level: level}
}

type lineLogger struct {
	log   logrus.FieldLogger
	level logrus.Level
	buf   bytes.Buffer
}

func (w *lineLogger) Write(p []byte) (int, error) {
	w.buf.Write(p)

	for {
		i := bytes.IndexByte(w.buf.Bytes(), '\n')
		if i < 0 {
			break
		}

		line := w.buf.Next(i + 1)
		msg := string(bytes.TrimRight(line, "\r\n"))

		switch w.level {
		case logrus.ErrorLevel:
			w.log.Error(msg)
		case logrus.WarnLevel:
			w.log.Warn(msg)
		case logrus.DebugLevel:
			w.log.Debug(msg)
		default:
			w.log.Info(msg)
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
