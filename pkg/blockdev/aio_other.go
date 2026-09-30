//go:build !linux

package blockdev

import (
	"context"
	"os"
	"time"
)

// runDeepWorkload has no native AIO outside Linux. The probe runs only on
// Linux, so this path serves the tests: it uses depth goroutines.
func runDeepWorkload(
	ctx context.Context,
	f *os.File,
	w workload,
	size int64,
	depth int,
	d time.Duration,
) (*workloadResult, error) {
	return runWorkload(ctx, f, w, size, depth, d)
}
