//go:build unix

package indexstore

import (
	"fmt"
	"syscall"
)

// volumeStats describes the filesystem holding the database.
//
// Used and Free do not add up to Total, and are not meant to: a filesystem
// typically reserves a slice for root that an ordinary process cannot touch.
// Reporting Total-Free as "used" would count that reserve as occupied, so an
// empty ext4 volume would read as 5% full. Used is what files actually
// occupy, and a usage share is Used/(Used+Free), which is what df prints.
type volumeStats struct {
	Total int64
	Used  int64
	Free  int64
}

// volumeUsage returns the size, the space files occupy, and the space still
// available to a non-root process, for the filesystem holding dir.
func volumeUsage(dir string) (volumeStats, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return volumeStats{}, fmt.Errorf("statfs %s: %w", dir, err)
	}

	blockSize := uint64(stat.Bsize) //nolint:unconvert,gosec // int64 on linux, uint32 on darwin

	return volumeStats{
		Total: int64(blockSize * stat.Blocks),                //nolint:gosec // filesystem sizes fit
		Used:  int64(blockSize * (stat.Blocks - stat.Bfree)), //nolint:gosec // filesystem sizes fit
		Free:  int64(blockSize * stat.Bavail),                //nolint:gosec // filesystem sizes fit
	}, nil
}
