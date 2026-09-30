//go:build linux

package blockdev

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newAIOTestFile creates a file of size bytes. It uses buffered I/O, because
// the temp dir can be on tmpfs, which rejects O_DIRECT. Native AIO then runs
// synchronously, but the submit and completion flow is the same.
func newAIOTestFile(t *testing.T, size int64) *os.File {
	t.Helper()

	//nolint:gosec // Test file in a temp dir.
	f, err := os.OpenFile(filepath.Join(t.TempDir(), "probe"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	require.NoError(t, err)

	t.Cleanup(func() { _ = f.Close() })

	require.NoError(t, f.Truncate(size))

	return f
}

func TestRunAIOWorkloadRandomMix(t *testing.T) {
	const size = 8 << 20

	f := newAIOTestFile(t, size)

	res, err := runAIOWorkload(context.Background(), f,
		workload{"rand_mixed", randBlockSize, true, 75, 0}, size, 16, 100*time.Millisecond)
	require.NoError(t, err)

	total := res.readsPerSec + res.writesPerSec
	require.Positive(t, total)
	assert.InDelta(t, 0.75, res.readsPerSec/total, 0.05, "about 75% of the operations are reads")
	assert.Nil(t, res.latency)
}

func TestRunAIOWorkloadSequentialMix(t *testing.T) {
	const size = 64 << 20

	f := newAIOTestFile(t, size)

	res, err := runAIOWorkload(context.Background(), f,
		workload{"seq_mixed", seqBlockSize, false, 50, 0}, size, 8, 100*time.Millisecond)
	require.NoError(t, err)

	assert.Positive(t, res.readsPerSec)
	assert.Positive(t, res.writesPerSec)
}

func TestRunAIOWorkloadCancelled(t *testing.T) {
	const size = 8 << 20

	f := newAIOTestFile(t, size)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := runAIOWorkload(ctx, f, workload{"rand_mixed", randBlockSize, true, 75, 0}, size, 16, time.Second)
	require.ErrorIs(t, err, context.Canceled)
}

func TestRunAIOWorkloadBadFile(t *testing.T) {
	const size = 8 << 20

	f := newAIOTestFile(t, size)
	require.NoError(t, f.Close())

	_, err := runAIOWorkload(context.Background(), f,
		workload{"rand_mixed", randBlockSize, true, 75, 0}, size, 4, 50*time.Millisecond)
	require.Error(t, err, "a closed file fails the submit")
}

func TestRunAIOWorkloadStopsAfterIOBytes(t *testing.T) {
	const size = 8 << 20

	f := newAIOTestFile(t, size)

	start := time.Now()
	res, err := runAIOWorkload(context.Background(), f,
		workload{"rand_mixed", randBlockSize, true, 75, 1 << 20}, size, 16, 10*time.Second)
	require.NoError(t, err)

	assert.Less(t, time.Since(start), 5*time.Second, "the workload stops after 1 MiB, not after 10s")
	assert.Positive(t, res.readsPerSec+res.writesPerSec)
}
