//go:build !unix

package indexstore

import "errors"

// volumeStats describes the filesystem holding the database. See the unix
// build of this file for what the fields mean.
type volumeStats struct {
	Total int64
	Used  int64
	Free  int64
}

// volumeUsage is not implemented outside unix. The caller treats an error as
// "unknown" and leaves the volume figures out of the report.
func volumeUsage(_ string) (volumeStats, error) {
	return volumeStats{}, errors.New(
		"volume usage is not supported on this platform",
	)
}
