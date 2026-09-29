//go:build linux

package blockdev

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// supported is true on the OS that has sysfs and O_DIRECT.
const supported = true

// statDevice returns the device number of the filesystem that holds path.
func statDevice(path string) (uint32, uint32, error) {
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		return 0, 0, err
	}

	return unix.Major(st.Dev), unix.Minor(st.Dev), nil
}

// statBlockNode returns the device number of the block device node at path.
func statBlockNode(path string) (uint32, uint32, error) {
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		return 0, 0, err
	}

	if st.Mode&unix.S_IFMT != unix.S_IFBLK {
		return 0, 0, fmt.Errorf("%s is not a block device", path)
	}

	return unix.Major(st.Rdev), unix.Minor(st.Rdev), nil
}

// openDirect creates a new file for direct I/O, which bypasses the page cache.
func openDirect(path string) (*os.File, error) {
	//nolint:gosec // The probe creates its own file in a directory it controls.
	return os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL|unix.O_DIRECT, 0o600)
}

// freeBytes returns the space that an unprivileged user can use in dir.
func freeBytes(dir string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, err
	}

	//nolint:gosec // Bsize is never negative.
	return st.Bavail * uint64(st.Bsize), nil
}
