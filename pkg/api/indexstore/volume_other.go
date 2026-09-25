//go:build !unix

package indexstore

import "errors"

// volumeUsage is not implemented outside unix. The caller treats an error as
// "unknown" and leaves the volume figures out of the report.
func volumeUsage(_ string) (total, free int64, err error) {
	return 0, 0, errors.New("volume usage is not supported on this platform")
}
