//go:build unix

package indexstore

import (
	"fmt"
	"syscall"
)

// volumeUsage returns the total and available bytes of the filesystem holding
// dir. Available is what a non-root process may still write, which is the
// number that matters here.
func volumeUsage(dir string) (total, free int64, err error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return 0, 0, fmt.Errorf("statfs %s: %w", dir, err)
	}

	blockSize := uint64(stat.Bsize) //nolint:unconvert,gosec // int64 on linux, uint32 on darwin

	return int64(blockSize * stat.Blocks), //nolint:gosec // filesystem sizes fit
		int64(blockSize * stat.Bavail), //nolint:gosec // filesystem sizes fit
		nil
}
