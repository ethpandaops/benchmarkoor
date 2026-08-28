package executor

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeStepFile(t *testing.T, dir, name, content string) *StepFile {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	return &StepFile{Path: path, Name: name}
}

// expectedHash is the digest the buffering implementation produced: every
// step's bytes concatenated in order.
func expectedHash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
	}

	// ComputeSuiteHash keeps the first 16 characters.
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func TestComputeSuiteHashMatchesConcatenatedContent(t *testing.T) {
	dir := t.TempDir()

	prepared := &PreparedSource{
		PreRunSteps: []*StepFile{writeStepFile(t, dir, "pre.request", "PRE\n")},
		Tests: []*TestWithSteps{
			{
				Name:    "one",
				Setup:   writeStepFile(t, dir, "one.setup", "SETUP\n"),
				Test:    writeStepFile(t, dir, "one.test", "TEST\n"),
				Cleanup: writeStepFile(t, dir, "one.cleanup", "CLEANUP\n"),
			},
			{
				Name: "two",
				Test: writeStepFile(t, dir, "two.test", "TEST2\n"),
			},
		},
	}

	got, err := ComputeSuiteHash(prepared)
	require.NoError(t, err)
	require.Equal(t, expectedHash("PRE\n", "SETUP\n", "TEST\n", "CLEANUP\n", "TEST2\n"), got)
}

func TestComputeSuiteHashProviderAndFileAgree(t *testing.T) {
	dir := t.TempDir()
	// linesProvider joins with "\n" and adds no trailing newline, so the
	// equivalent file content carries none either.
	content := "{\"a\":1}\n{\"b\":2}"

	fromFile, err := ComputeSuiteHash(&PreparedSource{
		PreRunSteps: []*StepFile{writeStepFile(t, dir, "pre.request", content)},
	})
	require.NoError(t, err)

	fromProvider, err := ComputeSuiteHash(&PreparedSource{
		PreRunSteps: []*StepFile{{
			Name:     "pre.request",
			Provider: &linesProvider{lines: []string{`{"a":1}`, `{"b":2}`}},
		}},
	})
	require.NoError(t, err)

	require.Equal(t, fromFile, fromProvider,
		"a step must hash the same whether it is read from disk or held in memory")
}

func TestComputeSuiteHashMissingFile(t *testing.T) {
	_, err := ComputeSuiteHash(&PreparedSource{
		PreRunSteps: []*StepFile{{Path: filepath.Join(t.TempDir(), "absent"), Name: "absent"}},
	})
	require.Error(t, err)
}

// TestComputeSuiteHashDoesNotBufferWholeStep is the regression guard. A 46 GiB
// pre-run bundle was read whole by getStepContent and the runner was
// OOM-killed at 56 GiB RSS before it replayed a single line.
func TestComputeSuiteHashDoesNotBufferWholeStep(t *testing.T) {
	const size = 256 << 20 // 256 MiB

	path := filepath.Join(t.TempDir(), "big.request")
	file, err := os.Create(path)
	require.NoError(t, err)

	chunk := strings.Repeat("z", 1<<20)
	for range size >> 20 {
		_, err = fmt.Fprint(file, chunk)
		require.NoError(t, err)
	}

	require.NoError(t, file.Close())

	prepared := &PreparedSource{
		PreRunSteps: []*StepFile{{Path: path, Name: "big.request"}},
	}

	var before, after runtime.MemStats

	runtime.GC()
	runtime.ReadMemStats(&before)

	_, err = ComputeSuiteHash(prepared)
	require.NoError(t, err)

	runtime.GC()
	runtime.ReadMemStats(&after)

	// Peak allocation must not scale with the step. Buffering 256 MiB would
	// show up here as ~256 MiB of total allocation; streaming stays tiny.
	allocated := after.TotalAlloc - before.TotalAlloc

	const maxAllocated = 16 << 20

	require.Less(t, allocated, uint64(maxAllocated),
		"hashing a %d MiB step allocated %d MiB", size>>20, allocated>>20)
}
