package blockdev

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSysfs builds a sysfs tree with:
//   - nvme0n1 (259:0) with partition nvme0n1p2 (259:2)
//   - sda (8:0) with partition sda1 (8:1)
//   - dm-0 (253:0) on nvme0n1p2 and sda1
type fakeSysfs struct {
	root string
	t    *testing.T
}

func newFakeSysfs(t *testing.T) *fakeSysfs {
	t.Helper()

	f := &fakeSysfs{root: t.TempDir(), t: t}

	nvme := f.disk("devices/pci0000:00/nvme/nvme0/nvme0n1", "259:0", map[string]string{
		"size":                      "7814037168",
		"queue/rotational":          "0",
		"queue/scheduler":           "[none] mq-deadline",
		"queue/logical_block_size":  "512",
		"queue/physical_block_size": "4096",
		"queue/nr_requests":         "1023",
		"device/model":              "Samsung SSD 990 PRO 4TB   ",
		"device/firmware_rev":       "4B2QJXD7",
	})
	nvmePart := f.partition(nvme, "nvme0n1p2", "259:2")

	sda := f.disk("devices/pci0000:00/ata1/host0/sda", "8:0", map[string]string{
		"size":             "1000",
		"queue/rotational": "1",
		"queue/scheduler":  "none [mq-deadline] kyber",
		"device/model":     "WDC WD40EFRX",
		"device/vendor":    "ATA",
		"device/rev":       "0A82",
	})
	sdaPart := f.partition(sda, "sda1", "8:1")

	dm := f.disk("devices/virtual/block/dm-0", "253:0", map[string]string{
		"dm/name":          "vg0-data",
		"queue/rotational": "0",
	})
	f.mkdir(filepath.Join(dm, "slaves"))
	f.symlink(nvmePart, filepath.Join(dm, "slaves", "nvme0n1p2"))
	f.symlink(sdaPart, filepath.Join(dm, "slaves", "sda1"))

	return f
}

func (f *fakeSysfs) disk(rel, majMin string, files map[string]string) string {
	dir := filepath.Join(f.root, rel)
	name := filepath.Base(dir)

	f.write(filepath.Join(dir, "dev"), majMin)
	f.write(filepath.Join(dir, "uevent"), fmt.Sprintf("MAJOR=0\nDEVNAME=%s\nDEVTYPE=disk\n", name))

	for file, content := range files {
		f.write(filepath.Join(dir, file), content)
	}

	f.symlink(dir, filepath.Join(f.root, "dev", "block", majMin))
	f.symlink(dir, filepath.Join(f.root, "class", "block", name))
	f.symlink(dir, filepath.Join(f.root, "block", name))

	return dir
}

func (f *fakeSysfs) partition(disk, name, majMin string) string {
	dir := filepath.Join(disk, name)

	f.write(filepath.Join(dir, "dev"), majMin)
	f.write(filepath.Join(dir, "partition"), "2")
	f.symlink(dir, filepath.Join(f.root, "dev", "block", majMin))
	f.symlink(dir, filepath.Join(f.root, "class", "block", name))

	return dir
}

func (f *fakeSysfs) mkdir(dir string) {
	require.NoError(f.t, os.MkdirAll(dir, 0o755))
}

func (f *fakeSysfs) write(path, content string) {
	f.mkdir(filepath.Dir(path))
	require.NoError(f.t, os.WriteFile(path, []byte(content+"\n"), 0o600))
}

func (f *fakeSysfs) symlink(target, link string) {
	f.mkdir(filepath.Dir(link))
	require.NoError(f.t, os.Symlink(target, link))
}

const testMountInfo = `22 1 259:2 / / rw,relatime shared:1 - ext4 /dev/nvme0n1p2 rw
30 22 253:0 / /data rw,relatime shared:2 - xfs /dev/mapper/vg0-data rw
31 22 0:45 / /data/overlay/merged rw,relatime - overlay overlay rw,lowerdir=/data/src,upperdir=/data/overlay/upper,workdir=/data/overlay/work
32 22 0:46 / /mnt/btrfs rw,relatime - btrfs /dev/sda1 rw,subvol=/@
33 22 0:47 / /tank/data rw - zfs tank/data rw,xattr
34 22 0:48 / /mnt/with\040space rw - tmpfs tmpfs rw
35 22 0:49 / /mnt/nodev rw - btrfs /dev/sda1 rw,subvol=/@
36 22 0:50 / /mnt/mapper rw - btrfs /dev/mapper/vg0-data rw
`

type devNum struct{ major, minor uint32 }

func newTestResolver(t *testing.T, stats map[string]devNum, nodes map[string]devNum) resolver {
	t.Helper()

	mountInfo := filepath.Join(t.TempDir(), "mountinfo")
	require.NoError(t, os.WriteFile(mountInfo, []byte(testMountInfo), 0o600))

	lookup := func(m map[string]devNum) func(string) (uint32, uint32, error) {
		return func(path string) (uint32, uint32, error) {
			n, ok := m[path]
			if !ok {
				return 0, 0, fmt.Errorf("stat %s: %w", path, os.ErrNotExist)
			}

			return n.major, n.minor, nil
		}
	}

	return resolver{
		sysfs:     newFakeSysfs(t).root,
		mountInfo: mountInfo,
		stat:      lookup(stats),
		blockNode: lookup(nodes),
	}
}

func TestResolve(t *testing.T) {
	stats := map[string]devNum{
		"/var/lib/docker/volumes/v/_data": {259, 2},
		"/data/copy":                      {253, 0},
		"/data/overlay/merged":            {0, 45},
		"/data/overlay/upper":             {253, 0},
		"/mnt/btrfs/x":                    {0, 46},
		"/mnt/btrfs/subvol/x":             {0, 90},
		"/mnt/nodev/x":                    {0, 91},
		"/mnt/mapper/x":                   {0, 92},
		"/tank/data/x":                    {0, 47},
		"/unknown":                        {0, 99},
	}
	nodes := map[string]devNum{"/dev/sda1": {8, 1}}

	r := newTestResolver(t, stats, nodes)

	t.Run("partition resolves to the whole disk", func(t *testing.T) {
		loc, err := r.resolve("/var/lib/docker/volumes/v/_data", 0)
		require.NoError(t, err)

		assert.Equal(t, "ext4", loc.Filesystem)
		assert.Equal(t, "nvme0n1p2", loc.Partition)
		assert.Equal(t, "/var/lib/docker/volumes/v/_data", loc.ProbeDir)

		dev := loc.Device
		assert.Equal(t, "nvme0n1", dev.Name)
		assert.Equal(t, "/dev/nvme0n1", dev.Path)
		assert.Equal(t, "259:0", dev.MajMin)
		assert.Equal(t, "nvme", dev.Kind)
		assert.Equal(t, "Samsung SSD 990 PRO 4TB", dev.Model)
		assert.Equal(t, "4B2QJXD7", dev.Firmware)
		assert.Equal(t, uint64(7814037168*512), dev.SizeBytes)
		require.NotNil(t, dev.Rotational)
		assert.False(t, *dev.Rotational)
		assert.Equal(t, "none", dev.Scheduler)
		assert.Equal(t, uint64(512), dev.LogicalBlockSize)
		assert.Equal(t, uint64(4096), dev.PhysicalBlockSize)
		assert.Equal(t, uint64(1023), dev.NrRequests)
		assert.Empty(t, dev.Backing)
	})

	t.Run("device-mapper lists its backing disks", func(t *testing.T) {
		loc, err := r.resolve("/data/copy", 0)
		require.NoError(t, err)

		assert.Equal(t, "xfs", loc.Filesystem)
		assert.Empty(t, loc.Partition)
		assert.Equal(t, "dm-0", loc.Device.Name)
		assert.Equal(t, "device-mapper", loc.Device.Kind)
		assert.Equal(t, "vg0-data", loc.Device.DMName)

		require.Len(t, loc.Device.Backing, 2)
		assert.Equal(t, "nvme0n1", loc.Device.Backing[0].Name)
		assert.Equal(t, "sda", loc.Device.Backing[1].Name)
		assert.Equal(t, "ATA", loc.Device.Backing[1].Vendor)
		assert.Equal(t, "0A82", loc.Device.Backing[1].Firmware)
		assert.Equal(t, "mq-deadline", loc.Device.Backing[1].Scheduler)
		require.NotNil(t, loc.Device.Backing[1].Rotational)
		assert.True(t, *loc.Device.Backing[1].Rotational)
	})

	t.Run("overlay follows the upper directory", func(t *testing.T) {
		loc, err := r.resolve("/data/overlay/merged", 0)
		require.NoError(t, err)

		assert.Equal(t, "/data/overlay/merged", loc.Path)
		assert.Equal(t, "overlay (xfs)", loc.Filesystem)
		assert.Equal(t, "/data/overlay", loc.ProbeDir)
		assert.Equal(t, "dm-0", loc.Device.Name)
	})

	t.Run("btrfs subvolume uses the mount source", func(t *testing.T) {
		loc, err := r.resolve("/mnt/btrfs/x", 0)
		require.NoError(t, err)

		assert.Equal(t, "btrfs", loc.Filesystem)
		assert.Equal(t, "sda1", loc.Partition)
		assert.Equal(t, "sda", loc.Device.Name)
		assert.Equal(t, "scsi", loc.Device.Kind)
	})

	t.Run("btrfs subvolume with its own device number", func(t *testing.T) {
		// stat gives 0:90, which the mount table does not list. The mount
		// point /mnt/btrfs holds the path.
		loc, err := r.resolve("/mnt/btrfs/subvol/x", 0)
		require.NoError(t, err)

		assert.Equal(t, "btrfs", loc.Filesystem)
		assert.Equal(t, "sda", loc.Device.Name)
	})

	t.Run("source node missing from /dev uses sysfs", func(t *testing.T) {
		r := r
		r.blockNode = func(string) (uint32, uint32, error) { return 0, 0, os.ErrNotExist }

		loc, err := r.resolve("/mnt/nodev/x", 0)
		require.NoError(t, err)
		assert.Equal(t, "sda", loc.Device.Name)
		assert.Equal(t, "sda1", loc.Partition)

		loc, err = r.resolve("/mnt/mapper/x", 0)
		require.NoError(t, err)
		assert.Equal(t, "dm-0", loc.Device.Name)
	})

	t.Run("zfs has no single block device", func(t *testing.T) {
		_, err := r.resolve("/tank/data/x", 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "zfs filesystem")
	})

	t.Run("unknown virtual device", func(t *testing.T) {
		r := r
		r.mountInfo = filepath.Join(t.TempDir(), "empty")
		require.NoError(t, os.WriteFile(r.mountInfo, nil, 0o600))

		_, err := r.resolve("/unknown", 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no entry")
	})

	t.Run("missing path", func(t *testing.T) {
		_, err := r.resolve("/missing", 0)
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}

func TestParseMountInfo(t *testing.T) {
	entries := parseMountInfo(testMountInfo + "garbage line\n")
	require.Len(t, entries, 8)

	overlay := findMount(entries, "0:45")
	require.NotNil(t, overlay)
	assert.Equal(t, "overlay", overlay.FSType)
	assert.Equal(t, "/data/overlay/upper", overlay.superOption("upperdir"))
	assert.Empty(t, overlay.superOption("missing"))

	tmpfs := findMount(entries, "0:48")
	require.NotNil(t, tmpfs)
	assert.Equal(t, "/mnt/with space", tmpfs.MountPoint)

	assert.Nil(t, findMount(entries, "1:1"))

	byPath := map[string]string{
		"/mnt/btrfs/a/b":  "/mnt/btrfs",
		"/mnt/btrfs":      "/mnt/btrfs",
		"/mnt/btrfsx":     "/",
		"/data/overlay/x": "/data",
		"/":               "/",
	}

	for path, want := range byPath {
		got := findMountByPath(entries, path)
		require.NotNil(t, got, path)
		assert.Equal(t, want, got.MountPoint, path)
	}
}

func TestActiveScheduler(t *testing.T) {
	tests := map[string]string{
		"none [mq-deadline] kyber": "mq-deadline",
		"[none] mq-deadline":       "none",
		"none":                     "none",
		"":                         "",
	}

	for in, want := range tests {
		assert.Equal(t, want, activeScheduler(in), in)
	}
}

func TestProbe(t *testing.T) {
	// The test uses buffered I/O, because the temp dir can be on tmpfs,
	// which rejects O_DIRECT. The numbers are not checked, only the flow.
	open := func(path string) (*os.File, error) {
		//nolint:gosec // Test file in a temp dir.
		return os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	}

	dir := t.TempDir()

	result, err := probe(context.Background(), logrus.New(), ProbeConfig{
		Dir:      dir,
		FileSize: minProbeFileSize,
		Duration: 50 * time.Millisecond,
		IODepth:  4,
	}, open)
	require.NoError(t, err)

	assert.Positive(t, result.RandReadIOPS)
	assert.Positive(t, result.RandWriteIOPS)
	assert.Positive(t, result.SeqReadBps)
	assert.Positive(t, result.SeqWriteBps)
	assert.Equal(t, 75, result.RandReadPercent)
	assert.Equal(t, 50, result.SeqReadPercent)

	for name, lat := range map[string]*LatencyResult{"read": result.QD1RandRead, "write": result.QD1RandWrite} {
		require.NotNil(t, lat, name)
		assert.Positive(t, lat.IOPS, name)
		assert.LessOrEqual(t, lat.P50Us, lat.P99Us, name)
	}

	assert.Equal(t, int64(minProbeFileSize), result.FileSizeBytes)
	assert.Equal(t, 4, result.IODepth)
	assert.Equal(t, randBlockSize, result.RandBlockSize)
	assert.Equal(t, seqBlockSize, result.SeqBlockSize)

	// The probe removes its file.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestProbeRejectsTinyFile(t *testing.T) {
	_, err := probe(context.Background(), logrus.New(), ProbeConfig{
		Dir:      t.TempDir(),
		FileSize: 1 << 20,
	}, openDirect)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "minimum")
}

func TestRandomBufferAlignment(t *testing.T) {
	for _, size := range []int{randBlockSize, seqBlockSize} {
		buf := randomBuffer(size, 1)
		assert.Len(t, buf, size)
		assert.Zero(t, alignOffset(buf))
	}
}

func TestDeviceKind(t *testing.T) {
	tests := map[string]string{
		"nvme0n1": "nvme",
		"sda":     "scsi",
		"vdb":     "virtio",
		"dm-0":    "device-mapper",
		"md127":   "md",
		"loop3":   "loop",
		"ram0":    "ramdisk",
		"zd0":     "other",
	}

	for name, want := range tests {
		assert.Equal(t, want, deviceKind(name), name)
	}
}

func TestRunWorkloadMixesReadsAndWrites(t *testing.T) {
	dir := t.TempDir()

	//nolint:gosec // Test file in a temp dir.
	f, err := os.OpenFile(filepath.Join(dir, "probe"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	require.NoError(t, err)

	t.Cleanup(func() { _ = f.Close() })

	const size = 8 << 20

	require.NoError(t, f.Truncate(size))

	res, err := runWorkload(context.Background(), f,
		workload{"rand_mixed", randBlockSize, true, 75, 0}, size, 4, 50*time.Millisecond)
	require.NoError(t, err)

	total := res.readsPerSec + res.writesPerSec
	require.Positive(t, total)
	assert.InDelta(t, 0.75, res.readsPerSec/total, 0.05, "about 75% of the operations are reads")
	assert.Nil(t, res.latency, "a deep workload has no latency result")

	res, err = runWorkload(context.Background(), f,
		workload{"qd1_rand_write", randBlockSize, true, 0, 0}, size, 1, 20*time.Millisecond)
	require.NoError(t, err)

	assert.Zero(t, res.readsPerSec)
	require.NotNil(t, res.latency)
	assert.InDelta(t, res.writesPerSec, res.latency.IOPS, 1e-9)
}

func TestLatencyHistogramPercentile(t *testing.T) {
	var h latencyHistogram

	assert.Zero(t, h.percentile(0.5), "an empty histogram")

	// 98 operations of 10 µs, one of 500 µs and one above the top bucket.
	for range 98 {
		h.add(10 * time.Microsecond)
	}

	h.add(500 * time.Microsecond)
	h.add(time.Second)

	assert.Equal(t, 10.0, h.percentile(0.50))
	assert.Equal(t, 10.0, h.percentile(0.98))
	assert.Equal(t, 500.0, h.percentile(0.99))
	assert.Equal(t, float64(maxLatencyUs), h.percentile(1))
}

func TestRunWorkloadStopsAfterIOBytes(t *testing.T) {
	dir := t.TempDir()

	//nolint:gosec // Test file in a temp dir.
	f, err := os.OpenFile(filepath.Join(dir, "probe"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	require.NoError(t, err)

	t.Cleanup(func() { _ = f.Close() })

	const size = 8 << 20

	require.NoError(t, f.Truncate(size))

	start := time.Now()
	res, err := runWorkload(context.Background(), f,
		workload{"rand_mixed", randBlockSize, true, 75, 1 << 20}, size, 4, 10*time.Second)
	require.NoError(t, err)

	assert.Less(t, time.Since(start), 5*time.Second, "the workload stops after 1 MiB, not after 10s")
	assert.Positive(t, res.readsPerSec+res.writesPerSec)
}
