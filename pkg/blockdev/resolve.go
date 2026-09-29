package blockdev

import (
	"fmt"
	"path/filepath"
	"strings"
)

// statFunc returns the device number of the filesystem that holds path.
type statFunc func(path string) (major, minor uint32, err error)

// blockNodeFunc returns the device number of a block device node. It fails
// when path is not a block device.
type blockNodeFunc func(path string) (major, minor uint32, err error)

// resolve finds the block device that holds path. A virtual filesystem (major
// number 0) is followed to its backing store: an overlay mount to its upper
// directory, and a btrfs subvolume to its source device.
func (r resolver) resolve(path string, depth int) (*Location, error) {
	if depth > maxResolveDepth {
		return nil, fmt.Errorf("resolving %s: too many nested overlay mounts", path)
	}

	// The mount table has real paths, so a symlink in path must not hide the
	// mount. A path that cannot be resolved stays as it is.
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}

	major, minor, err := r.stat(path)
	if err != nil {
		return nil, fmt.Errorf("reading the device of %s: %w", path, err)
	}

	// The mount table is only needed to name the filesystem and to follow a
	// virtual filesystem. A missing table does not stop a direct lookup.
	mounts, mountErr := readMountInfo(r.mountInfo)

	// A btrfs subvolume has its own anonymous device number, which is not
	// the number in the mount table. The mount point finds that mount.
	mount := findMount(mounts, fmt.Sprintf("%d:%d", major, minor))
	if mount == nil && major == 0 {
		mount = findMountByPath(mounts, path)
	}

	var fsType string
	if mount != nil {
		fsType = mount.FSType
	}

	if major == 0 {
		if mountErr != nil {
			return nil, fmt.Errorf("%s is on a virtual filesystem: %w", path, mountErr)
		}

		if mount == nil {
			return nil, fmt.Errorf("%s is on a virtual filesystem (%d:%d) with no entry in %s",
				path, major, minor, r.mountInfo)
		}

		switch {
		case mount.FSType == "overlay":
			upper := mount.superOption("upperdir")
			if upper == "" {
				return nil, fmt.Errorf("%s is on an overlay mount with no upper directory", path)
			}

			loc, err := r.resolve(upper, depth+1)
			if err != nil {
				return nil, fmt.Errorf("resolving the overlay upper directory of %s: %w", path, err)
			}

			loc.Path = path
			loc.Filesystem = fmt.Sprintf("overlay (%s)", loc.Filesystem)
			// A file must not be written into the upper directory while the
			// overlay is mounted, so the probe uses its parent directory.
			loc.ProbeDir = filepath.Dir(upper)

			return loc, nil
		case strings.HasPrefix(mount.Source, "/dev/"):
			// btrfs gives each subvolume an anonymous device number. The
			// mount source names the real device.
			major, minor, err = r.sourceDevice(mount.Source)
			if err != nil {
				return nil, fmt.Errorf("reading the source device %s of %s: %w", mount.Source, path, err)
			}
		default:
			return nil, fmt.Errorf(
				"%s is on a %s filesystem (source %q) that has no single block device",
				path, mount.FSType, mount.Source)
		}
	}

	dev, partition, err := r.deviceFromSysfs(major, minor)
	if err != nil {
		return nil, err
	}

	return &Location{
		Path:       path,
		Filesystem: fsType,
		Partition:  partition,
		Device:     dev,
		ProbeDir:   path,
	}, nil
}

// sourceDevice returns the device number of a mount source such as
// "/dev/vdb1" or "/dev/mapper/vg0-data". In a container, /dev does not have
// the host device nodes, so sysfs is the fallback.
func (r resolver) sourceDevice(source string) (uint32, uint32, error) {
	major, minor, err := r.blockNode(source)
	if err == nil {
		return major, minor, nil
	}

	name := filepath.Base(source)

	// A device-mapper node has its dm name, not its kernel name.
	if strings.HasPrefix(source, "/dev/mapper/") {
		if dmDev := r.dmDeviceByName(name); dmDev != "" {
			name = dmDev
		}
	}

	majMin := readString(filepath.Join(r.sysfs, "class", "block", name, "dev"))
	if majMin == "" {
		return 0, 0, err
	}

	var sysMajor, sysMinor uint32
	if _, scanErr := fmt.Sscanf(majMin, "%d:%d", &sysMajor, &sysMinor); scanErr != nil {
		return 0, 0, fmt.Errorf("parsing %q: %w", majMin, scanErr)
	}

	return sysMajor, sysMinor, nil
}

// dmDeviceByName returns the kernel name ("dm-0") of a device-mapper device.
func (r resolver) dmDeviceByName(dmName string) string {
	matches, _ := filepath.Glob(filepath.Join(r.sysfs, "block", "dm-*", "dm", "name"))

	for _, match := range matches {
		if readString(match) == dmName {
			return filepath.Base(filepath.Dir(filepath.Dir(match)))
		}
	}

	return ""
}
