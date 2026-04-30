package docker

import (
	"os"
	"strconv"
	"strings"
)

// DefaultMaxNofile is the fallback nofile (open-file-descriptor) limit
// applied to containers when /proc/sys/fs/nr_open cannot be read (e.g.
// non-Linux hosts). Matches the common Linux kernel default cap.
const DefaultMaxNofile uint64 = 1048576

// HostMaxNofile returns the highest RLIMIT_NOFILE value that containers
// on this host are allowed to set. On Linux it reads
// /proc/sys/fs/nr_open, the kernel-wide cap above which RLIMIT_NOFILE
// cannot be raised. On any other platform — or if the file is missing,
// empty, or unparseable — it returns DefaultMaxNofile. EL clients tend
// to keep many file descriptors open; bumping nofile to the kernel's
// hard ceiling avoids spurious "too many open files" failures during
// long benchmark runs.
func HostMaxNofile() uint64 {
	data, err := os.ReadFile("/proc/sys/fs/nr_open")
	if err != nil {
		return DefaultMaxNofile
	}

	v, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || v == 0 {
		return DefaultMaxNofile
	}

	return v
}
