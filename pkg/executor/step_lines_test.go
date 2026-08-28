package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// drain reads a source to exhaustion.
func drain(t *testing.T, src lineSource) []string {
	t.Helper()

	var got []string

	for {
		line, ok, err := src.Next()
		require.NoError(t, err)

		if !ok {
			break
		}

		got = append(got, line)
	}

	return got
}

func writeStep(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "pre_run.request")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	return path
}

func TestFileLineSourceMatchesSlice(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{"empty", "", nil},
		{"single line with newline", "{\"a\":1}\n", []string{`{"a":1}`}},
		{"single line no trailing newline", "{\"a\":1}", []string{`{"a":1}`}},
		{"blank lines skipped", "{\"a\":1}\n\n   \n{\"b\":2}\n", []string{`{"a":1}`, `{"b":2}`}},
		{"whitespace trimmed", "  {\"a\":1}  \n\t{\"b\":2}\t\n", []string{`{"a":1}`, `{"b":2}`}},
		{"no trailing newline after blanks", "{\"a\":1}\n\n{\"b\":2}", []string{`{"a":1}`, `{"b":2}`}},
		{"only blank lines", "\n\n   \n", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src, err := newFileLineSource(writeStep(t, tc.content))
			require.NoError(t, err)

			defer func() { _ = src.Close() }()

			require.Equal(t, tc.want, drain(t, src))
			// Total must agree with what was yielded, or progress and ETA lie.
			require.Equal(t, len(tc.want), src.Total())
		})
	}
}

func TestFileLineSourceHandlesLinesLargerThanBuffer(t *testing.T) {
	// One JSON-RPC line carries a whole block payload and routinely exceeds
	// the read buffer, which is what rules out bufio.Scanner here.
	big := strings.Repeat("x", stepReadBufferSize*3)
	path := writeStep(t, fmt.Sprintf("{\"a\":\"%s\"}\n{\"b\":2}\n", big))

	src, err := newFileLineSource(path)
	require.NoError(t, err)

	defer func() { _ = src.Close() }()

	got := drain(t, src)
	require.Len(t, got, 2)
	require.Equal(t, 2, src.Total())
	require.Equal(t, fmt.Sprintf("{\"a\":\"%s\"}", big), got[0])
	require.Equal(t, `{"b":2}`, got[1])
}

func TestSliceLineSource(t *testing.T) {
	src := newSliceLineSource([]string{"a", "b", "c"})
	require.Equal(t, 3, src.Total())
	require.Equal(t, []string{"a", "b", "c"}, drain(t, src))
	require.NoError(t, src.Close())

	empty := newSliceLineSource(nil)
	require.Equal(t, 0, empty.Total())
	require.Nil(t, drain(t, empty))
}

func TestFileLineSourceMissingFile(t *testing.T) {
	_, err := newFileLineSource(filepath.Join(t.TempDir(), "absent"))
	require.Error(t, err)
}

// TestFileLineSourceDoesNotBufferWholeFile is the regression guard: a 46 GiB
// pre-run bundle was read into a []string and the runner was OOM-killed at
// 56 GiB RSS. Replaying must stay flat in the size of the step, not linear.
func TestFileLineSourceDoesNotBufferWholeFile(t *testing.T) {
	const (
		lineSize = 1 << 20
		lines    = 256 // 256 MiB of step file
	)

	path := filepath.Join(t.TempDir(), "big.request")
	file, err := os.Create(path)
	require.NoError(t, err)

	payload := strings.Repeat("y", lineSize)
	for range lines {
		_, err = fmt.Fprintf(file, "{\"p\":\"%s\"}\n", payload)
		require.NoError(t, err)
	}

	require.NoError(t, file.Close())

	src, err := newFileLineSource(path)
	require.NoError(t, err)

	defer func() { _ = src.Close() }()

	require.Equal(t, lines, src.Total())

	var before, after runtime.MemStats

	runtime.GC()
	runtime.ReadMemStats(&before)

	count := 0

	for {
		_, ok, err := src.Next()
		require.NoError(t, err)

		if !ok {
			break
		}

		count++
	}

	runtime.GC()
	runtime.ReadMemStats(&after)

	require.Equal(t, lines, count)

	// Held bytes must not scale with the file. Allow generous headroom for
	// the read buffer and one in-flight line; buffering the whole file would
	// land two orders of magnitude above this.
	const maxHeld = 32 << 20

	if after.HeapAlloc > before.HeapAlloc {
		require.Less(t, after.HeapAlloc-before.HeapAlloc, uint64(maxHeld),
			"replaying a %d MiB step retained %d MiB", (lineSize*lines)>>20,
			(after.HeapAlloc-before.HeapAlloc)>>20)
	}
}
