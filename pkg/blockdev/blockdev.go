// Package blockdev finds the block device under a filesystem path, reads its
// details from sysfs, and measures its I/O capacity.
package blockdev

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DefaultSysfsPath is the sysfs mount point on a Linux host.
const DefaultSysfsPath = "/sys"

// DefaultMountInfoPath lists the mounts that this process sees.
const DefaultMountInfoPath = "/proc/self/mountinfo"

// maxResolveDepth stops a loop of overlay mounts that point at each other.
const maxResolveDepth = 8

// ErrUnsupported tells that this OS has no block device lookup or probe.
var ErrUnsupported = errors.New("block device inspection is only supported on Linux")

// Device describes a block device from sysfs.
type Device struct {
	// Name is the kernel name, e.g. "nvme0n1" or "dm-0".
	Name string `json:"name"`
	// Path is the device node, e.g. "/dev/nvme0n1".
	Path string `json:"path"`
	// MajMin is the "major:minor" device number.
	MajMin string `json:"maj_min,omitempty"`
	// Kind is the device family: nvme, scsi, virtio, xen, mmc, device-mapper,
	// md, loop, ramdisk or other.
	Kind string `json:"kind,omitempty"`
	// DMName is the device-mapper name, e.g. "vg0-root".
	DMName            string `json:"dm_name,omitempty"`
	Model             string `json:"model,omitempty"`
	Vendor            string `json:"vendor,omitempty"`
	Firmware          string `json:"firmware,omitempty"`
	SizeBytes         uint64 `json:"size_bytes,omitempty"`
	Rotational        *bool  `json:"rotational,omitempty"`
	Scheduler         string `json:"scheduler,omitempty"`
	LogicalBlockSize  uint64 `json:"logical_block_size,omitempty"`
	PhysicalBlockSize uint64 `json:"physical_block_size,omitempty"`
	NrRequests        uint64 `json:"nr_requests,omitempty"`
	// Backing lists the physical disks under a device-mapper or md device.
	Backing []Device `json:"backing,omitempty"`
}

// Location is the block device that holds a filesystem path.
type Location struct {
	// Path is the path that was resolved.
	Path string `json:"path"`
	// Filesystem is the filesystem type, e.g. "ext4" or "overlay (xfs)".
	Filesystem string `json:"filesystem,omitempty"`
	// Partition is the partition that holds the filesystem, if any.
	Partition string `json:"partition,omitempty"`
	// Device is the whole-disk device. A cgroup I/O limit applies to this
	// device, because the kernel does not throttle a single partition.
	Device *Device `json:"device"`
	// ProbeDir is a directory on the same filesystem where a probe can
	// write a temporary file.
	ProbeDir string `json:"-"`
}

// resolver holds the paths that the lookup reads, so that tests can use a
// fake sysfs tree and mount table.
type resolver struct {
	sysfs     string
	mountInfo string
	stat      statFunc
	blockNode blockNodeFunc
}

// DeviceNumber returns the "major:minor" number of the block device node at
// path, e.g. "259:0" for /dev/nvme0n1.
func DeviceNumber(path string) (string, error) {
	major, minor, err := statBlockNode(path)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%d:%d", major, minor), nil
}

// Resolve finds the block device that holds path.
func Resolve(path string) (*Location, error) {
	if !supported {
		return nil, ErrUnsupported
	}

	r := resolver{
		sysfs:     DefaultSysfsPath,
		mountInfo: DefaultMountInfoPath,
		stat:      statDevice,
		blockNode: statBlockNode,
	}

	return r.resolve(path, 0)
}

// deviceFromSysfs reads the device with the given number. It returns the
// whole-disk device and, when the number is a partition, the partition name.
func (r resolver) deviceFromSysfs(major, minor uint32) (*Device, string, error) {
	link := filepath.Join(r.sysfs, "dev", "block", fmt.Sprintf("%d:%d", major, minor))

	dir, err := filepath.EvalSymlinks(link)
	if err != nil {
		return nil, "", fmt.Errorf("finding block device %d:%d in sysfs: %w", major, minor, err)
	}

	var partition string

	if fileExists(filepath.Join(dir, "partition")) {
		partition = filepath.Base(dir)
		dir = filepath.Dir(dir)
	}

	dev := readDevice(dir)
	dev.Backing = r.backingDisks(dir, make(map[string]struct{}, 4))

	return dev, partition, nil
}

// backingDisks returns the physical disks under a stacked device (dm or md).
// It follows the "slaves" links down to the leaf disks.
func (r resolver) backingDisks(dir string, seen map[string]struct{}) []Device {
	entries, err := os.ReadDir(filepath.Join(dir, "slaves"))
	if err != nil || len(entries) == 0 {
		return nil
	}

	disks := make([]Device, 0, len(entries))

	for _, entry := range entries {
		slave, err := filepath.EvalSymlinks(filepath.Join(dir, "slaves", entry.Name()))
		if err != nil {
			continue
		}

		if fileExists(filepath.Join(slave, "partition")) {
			slave = filepath.Dir(slave)
		}

		if lower := r.backingDisks(slave, seen); len(lower) > 0 {
			disks = append(disks, lower...)

			continue
		}

		name := filepath.Base(slave)
		if _, ok := seen[name]; ok {
			continue
		}

		seen[name] = struct{}{}

		disks = append(disks, *readDevice(slave))
	}

	return disks
}

// readDevice reads the details of the whole-disk device at a sysfs directory.
// A missing file leaves its field empty.
func readDevice(dir string) *Device {
	name := filepath.Base(dir)
	dev := &Device{
		Name: name,
		Path: "/dev/" + name,
		Kind: deviceKind(name),
	}

	if devName := ueventValue(filepath.Join(dir, "uevent"), "DEVNAME"); devName != "" {
		dev.Path = "/dev/" + devName
	}

	dev.MajMin = readString(filepath.Join(dir, "dev"))
	dev.DMName = readString(filepath.Join(dir, "dm", "name"))
	dev.Model = readString(filepath.Join(dir, "device", "model"))
	dev.Vendor = readString(filepath.Join(dir, "device", "vendor"))

	// A virtio disk gives a numeric vendor ID such as "0x0000", not a name.
	if strings.HasPrefix(dev.Vendor, "0x") {
		dev.Vendor = ""
	}

	dev.Firmware = readString(filepath.Join(dir, "device", "firmware_rev"))
	if dev.Firmware == "" {
		dev.Firmware = readString(filepath.Join(dir, "device", "rev"))
	}

	// The size file counts 512-byte sectors, whatever the block size is.
	if sectors, ok := readUint(filepath.Join(dir, "size")); ok {
		dev.SizeBytes = sectors * 512
	}

	if rot, ok := readUint(filepath.Join(dir, "queue", "rotational")); ok {
		rotational := rot == 1
		dev.Rotational = &rotational
	}

	dev.Scheduler = activeScheduler(readString(filepath.Join(dir, "queue", "scheduler")))
	dev.LogicalBlockSize, _ = readUint(filepath.Join(dir, "queue", "logical_block_size"))
	dev.PhysicalBlockSize, _ = readUint(filepath.Join(dir, "queue", "physical_block_size"))
	dev.NrRequests, _ = readUint(filepath.Join(dir, "queue", "nr_requests"))

	return dev
}

// deviceKind names the device family from the kernel name.
func deviceKind(name string) string {
	switch {
	case strings.HasPrefix(name, "nvme"):
		return "nvme"
	case strings.HasPrefix(name, "sd"):
		return "scsi"
	case strings.HasPrefix(name, "vd"):
		return "virtio"
	case strings.HasPrefix(name, "xvd"):
		return "xen"
	case strings.HasPrefix(name, "mmcblk"):
		return "mmc"
	case strings.HasPrefix(name, "dm-"):
		return "device-mapper"
	case strings.HasPrefix(name, "md"):
		return "md"
	case strings.HasPrefix(name, "loop"):
		return "loop"
	case strings.HasPrefix(name, "ram"):
		// A brd RAM disk, e.g. the dm-era metadata device of schelk.
		return "ramdisk"
	default:
		return "other"
	}
}

// activeScheduler returns the selected entry of a sysfs scheduler list such
// as "none [mq-deadline] kyber".
func activeScheduler(list string) string {
	for field := range strings.FieldsSeq(list) {
		if strings.HasPrefix(field, "[") && strings.HasSuffix(field, "]") {
			return strings.Trim(field, "[]")
		}
	}

	return list
}

// ueventValue returns the value of key in a sysfs uevent file.
func ueventValue(path, key string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, key+"="); ok {
			return strings.TrimSpace(value)
		}
	}

	return ""
}

func readString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}

func readUint(path string) (uint64, bool) {
	value, err := strconv.ParseUint(readString(path), 10, 64)
	if err != nil {
		return 0, false
	}

	return value, true
}

func fileExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}
