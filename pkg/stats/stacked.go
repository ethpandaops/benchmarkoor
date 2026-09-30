package stats

import (
	"os"
	"path/filepath"
	"sync"
)

// defaultSysfsPath is the sysfs mount point on a Linux host.
const defaultSysfsPath = "/sys"

// stackedDevices finds the block devices that are lower layers of another
// block device, e.g. the NVMe disk under a device-mapper (dm-era, LVM) or md
// device. The kernel charges an I/O to the cgroup on each layer that it goes
// through. Thus io.stat lists the same I/O once for the upper device and once
// for each lower device, and only the upper devices must be counted.
type stackedDevices struct {
	sysfs string

	mu    sync.Mutex
	lower map[string]bool
}

func newStackedDevices(sysfs string) *stackedDevices {
	return &stackedDevices{
		sysfs: sysfs,
		lower: make(map[string]bool, 8),
	}
}

// isLower tells if another block device holds the device with the given
// "major:minor" number, or one of its partitions. io.stat lists a whole disk,
// not a partition, so a disk with a partition under dm-0 is a lower device.
// When sysfs cannot tell (not Linux, or no /sys), the device is counted.
//
// A disk with one partition under dm-0 and another partition in direct use
// is also a lower device, so the I/O to the direct partition is not counted.
func (s *stackedDevices) isLower(majMin string) bool {
	if s == nil {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if lower, ok := s.lower[majMin]; ok {
		return lower
	}

	lower := s.hasHolders(filepath.Join(s.sysfs, "dev", "block", majMin))
	s.lower[majMin] = lower

	return lower
}

// hasHolders tells if the device directory, or one of its partitions, has
// an entry in holders.
func (s *stackedDevices) hasHolders(dir string) bool {
	if dirHasEntries(filepath.Join(dir, "holders")) {
		return true
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		part := filepath.Join(dir, entry.Name())
		if _, err := os.Stat(filepath.Join(part, "partition")); err != nil {
			continue
		}

		if dirHasEntries(filepath.Join(part, "holders")) {
			return true
		}
	}

	return false
}

func dirHasEntries(dir string) bool {
	entries, err := os.ReadDir(dir)

	return err == nil && len(entries) > 0
}
