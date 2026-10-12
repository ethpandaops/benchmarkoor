package builder

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsPopulated(t *testing.T) {
	t.Run("missing dir is not populated", func(t *testing.T) {
		got, err := isPopulated(filepath.Join(t.TempDir(), "absent"))
		require.NoError(t, err)
		assert.False(t, got)
	})

	t.Run("empty dir is not populated", func(t *testing.T) {
		got, err := isPopulated(t.TempDir())
		require.NoError(t, err)
		assert.False(t, got)
	})

	// A build that failed before producing anything still writes its sidecars.
	// They must not make the dir look built, or the next build would skip it.
	t.Run("sidecars alone are not populated", func(t *testing.T) {
		dir := t.TempDir()
		for _, name := range []string{buildSidecarFile, eestFillResultFile, pytestReportFile} {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600))
		}

		got, err := isPopulated(dir)
		require.NoError(t, err)
		assert.False(t, got)
	})

	t.Run("any produced entry alongside sidecars is populated", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, buildSidecarFile), []byte("{}"), 0o600))
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".meta"), 0o755))

		got, err := isPopulated(dir)
		require.NoError(t, err)
		assert.True(t, got)
	})
}

// TestTailBufferConcurrent exercises the case the mutex guards: the container
// log-streaming goroutine keeps Write()-ing while the caller reads String() on
// the error path (RunInitContainer doesn't join the streaming goroutine). Run
// with -race to catch a regression.
func TestTailBufferConcurrent(t *testing.T) {
	const maxBytes = 64

	tb := newTailBuffer(maxBytes)

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()

		for i := 0; i < 2000; i++ {
			_, _ = tb.Write([]byte("a streamed container log line\n"))
		}
	}()

	for i := 0; i < 2000; i++ {
		_ = tb.String()
	}

	wg.Wait()

	// The retained tail is bounded by max regardless of how much was written.
	assert.LessOrEqual(t, len(tb.String()), maxBytes)
}
