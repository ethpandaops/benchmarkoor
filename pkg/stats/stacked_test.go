package stats

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ioStatSchelkHost is the io.stat of a client container on a schelk host
// (benchmark-ci-11). The datadir is on dm-0 (dm-era) over a partition of
// nvme2n1, and the root filesystem is on md2 over nvme0n1 and nvme1n1.
const ioStatSchelkHost = `259:0 rbytes=3943170048 wbytes=184811520 rios=962688 wios=45120 dbytes=0 dios=0
253:0 rbytes=3943170048 wbytes=184811520 rios=962688 wios=45120 dbytes=0 dios=0
259:1 rbytes=97472512 wbytes=11702272 rios=1470 wios=100 dbytes=0 dios=0
259:2 rbytes=99926016 wbytes=10448896 rios=1580 wios=96 dbytes=0 dios=0
9:2 rbytes=197398528 wbytes=22151168 rios=2732 wios=180 dbytes=0 dios=0
`

// fakeSchelkSysfs builds the sysfs block devices of a schelk host:
//
//	nvme2n1 (259:0) -> nvme2n1p2 -> dm-0 (253:0)
//	ram0 (1:0)      -> dm-0 (dm-era metadata)
//	nvme0n1 (259:1) -> nvme0n1p2 -> md2 (9:2)
//	nvme1n1 (259:2) -> nvme1n1p2 -> md2 (9:2)
//	nvme3n1 (259:3), a disk that nothing holds
func fakeSchelkSysfs(t *testing.T) string {
	t.Helper()

	sysfs := t.TempDir()

	mkdir := func(parts ...string) string {
		dir := filepath.Join(append([]string{sysfs, "dev", "block"}, parts...)...)
		require.NoError(t, os.MkdirAll(dir, 0o755))

		return dir
	}

	partition := func(disk, name, holder string) {
		dir := mkdir(disk, name)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "partition"), []byte("2\n"), 0o644))
		mkdir(disk, name, "holders", holder)
	}

	mkdir("253:0", "holders")
	mkdir("9:2", "holders")
	mkdir("1:0", "holders", "dm-0")
	partition("259:0", "nvme2n1p2", "dm-0")
	partition("259:1", "nvme0n1p2", "md2")
	partition("259:2", "nvme1n1p2", "md2")
	mkdir("259:3", "holders")
	mkdir("259:3", "queue")

	return sysfs
}

func TestStackedDevicesIsLower(t *testing.T) {
	s := newStackedDevices(fakeSchelkSysfs(t))

	tests := map[string]bool{
		"253:0": false, // dm-0
		"9:2":   false, // md2
		"259:3": false, // a disk that nothing holds
		"259:0": true,  // under dm-0 through a partition
		"259:1": true,  // under md2 through a partition
		"1:0":   true,  // ram0, the whole disk is under dm-0
		"8:0":   false, // not in sysfs
	}

	for majMin, want := range tests {
		assert.Equal(t, want, s.isLower(majMin), majMin)
	}

	var nilSet *stackedDevices
	assert.False(t, nilSet.isLower("259:0"), "a nil set counts every device")
}

func TestCgroupReaderReadIOStatsSkipsLowerDevices(t *testing.T) {
	cgroupPath := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cgroupPath, "io.stat"), []byte(ioStatSchelkHost), 0o644))

	r := &cgroupReader{
		log:        logrus.New(),
		cgroupPath: cgroupPath,
		stacked:    newStackedDevices(fakeSchelkSysfs(t)),
	}

	readBytes, writeBytes, readOps, writeOps, err := r.readIOStats()
	require.NoError(t, err)

	// Only dm-0 (253:0) and md2 (9:2) count.
	assert.Equal(t, uint64(3943170048+197398528), readBytes)
	assert.Equal(t, uint64(184811520+22151168), writeBytes)
	assert.Equal(t, uint64(962688+2732), readOps)
	assert.Equal(t, uint64(45120+180), writeOps)
}

func TestCgroupReaderReadIOStatsWithoutSysfs(t *testing.T) {
	cgroupPath := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cgroupPath, "io.stat"),
		[]byte("8:0 rbytes=100 wbytes=200 rios=3 wios=4\n8:16 rbytes=10 wbytes=20 rios=1 wios=2\n"), 0o644))

	r := &cgroupReader{
		log:        logrus.New(),
		cgroupPath: cgroupPath,
		stacked:    newStackedDevices(filepath.Join(t.TempDir(), "missing")),
	}

	readBytes, writeBytes, readOps, writeOps, err := r.readIOStats()
	require.NoError(t, err)

	// Without sysfs, every device counts.
	assert.Equal(t, uint64(110), readBytes)
	assert.Equal(t, uint64(220), writeBytes)
	assert.Equal(t, uint64(4), readOps)
	assert.Equal(t, uint64(6), writeOps)
}

func TestExtractBlkioSkipsLowerDevices(t *testing.T) {
	stacked := newStackedDevices(fakeSchelkSysfs(t))

	ds := &container.StatsResponse{}
	ds.BlkioStats.IoServiceBytesRecursive = []container.BlkioStatEntry{
		{Major: 259, Minor: 0, Op: "read", Value: 1000},
		{Major: 253, Minor: 0, Op: "read", Value: 1000},
		{Major: 259, Minor: 0, Op: "write", Value: 50},
		{Major: 253, Minor: 0, Op: "write", Value: 50},
	}
	ds.BlkioStats.IoServicedRecursive = []container.BlkioStatEntry{
		{Major: 259, Minor: 0, Op: "read", Value: 10},
		{Major: 253, Minor: 0, Op: "read", Value: 10},
		{Major: 259, Minor: 0, Op: "write", Value: 5},
		{Major: 253, Minor: 0, Op: "write", Value: 5},
	}

	readBytes, writeBytes := extractBlkioBytes(ds, stacked)
	assert.Equal(t, uint64(1000), readBytes)
	assert.Equal(t, uint64(50), writeBytes)

	readOps, writeOps := extractBlkioOps(ds, stacked)
	assert.Equal(t, uint64(10), readOps)
	assert.Equal(t, uint64(5), writeOps)
}
