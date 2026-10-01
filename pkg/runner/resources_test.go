package runner

import (
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/blockdev"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/cputopology"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sixCoresSMT is a 6-core host with 2 threads per core, siblings (n, n+6).
func sixCoresSMT() []cputopology.CPU {
	cpus := make([]cputopology.CPU, 0, 12)
	for id := range 12 {
		cpus = append(cpus, cputopology.CPU{ID: id, Core: id % 6})
	}

	return cpus
}

func intPtr(v int) *int { return &v }

func TestBuildContainerResourceLimitsCpusetTopology(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		limits, resolved, err := buildContainerResourceLimits(nil, nil, nil)
		require.NoError(t, err)
		assert.Nil(t, limits)
		assert.Nil(t, resolved)
	})

	t.Run("full_cores count picks sibling pairs", func(t *testing.T) {
		cfg := &config.ResourceLimits{CpusetCount: intPtr(4), CpusetTopology: "full_cores"}

		limits, resolved, err := buildContainerResourceLimits(cfg, sixCoresSMT(), nil)
		require.NoError(t, err)
		assert.Equal(t, "full_cores", resolved.CpusetTopology)
		assert.Equal(t, limits.CpusetCpus, resolved.CpusetCpus)

		cpus, err := cputopology.ParseCPUList(resolved.CpusetCpus)
		require.NoError(t, err)
		require.Len(t, cpus, 4)
		require.NoError(t, cputopology.Check(sixCoresSMT(), cpus, cputopology.ModeFullCores))
	})

	t.Run("one_thread_per_core count picks distinct cores", func(t *testing.T) {
		cfg := &config.ResourceLimits{CpusetCount: intPtr(6), CpusetTopology: "one_thread_per_core"}

		_, resolved, err := buildContainerResourceLimits(cfg, sixCoresSMT(), nil)
		require.NoError(t, err)
		assert.Equal(t, "0,1,2,3,4,5", resolved.CpusetCpus)
	})

	t.Run("explicit cpuset is checked against the mode", func(t *testing.T) {
		cfg := &config.ResourceLimits{Cpuset: []int{0, 1, 2, 3, 4, 5}, CpusetTopology: "full_cores"}

		_, _, err := buildContainerResourceLimits(cfg, sixCoresSMT(), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "uses 1 of 2 threads")

		cfg.Cpuset = []int{0, 6, 1, 7}
		_, resolved, err := buildContainerResourceLimits(cfg, sixCoresSMT(), nil)
		require.NoError(t, err)
		assert.Equal(t, "0,6,1,7", resolved.CpusetCpus)
	})

	t.Run("any mode leaves the resolved topology empty", func(t *testing.T) {
		cfg := &config.ResourceLimits{Cpuset: []int{0, 1}}

		_, resolved, err := buildContainerResourceLimits(cfg, nil, nil)
		require.NoError(t, err)
		assert.Empty(t, resolved.CpusetTopology)
		assert.Equal(t, "0,1", resolved.CpusetCpus)
	})

	t.Run("mode without a host topology fails", func(t *testing.T) {
		cfg := &config.ResourceLimits{CpusetCount: intPtr(2), CpusetTopology: "full_cores"}
		_, _, err := buildContainerResourceLimits(cfg, nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not expose")

		cfg = &config.ResourceLimits{Cpuset: []int{0, 1}, CpusetTopology: "full_cores"}
		_, _, err = buildContainerResourceLimits(cfg, nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not expose")
	})
}

func TestBuildContainerResourceLimitsDeviceLimits(t *testing.T) {
	storage := &StorageInfo{
		DataPath: "/var/lib/docker/volumes/v/_data",
		Device:   &blockdev.Device{Name: "nvme0n1", Path: "/dev/nvme0n1"},
	}

	t.Run("limits throttle the datadir device", func(t *testing.T) {
		cfg := &config.ResourceLimits{
			DeviceReadIOps:  50000,
			DeviceWriteIOps: 15000,
			DeviceReadBps:   "500mb",
			DeviceWriteBps:  "250mb",
		}

		limits, resolved, err := buildContainerResourceLimits(cfg, nil, storage)
		require.NoError(t, err)

		assert.Equal(t, []docker.BlkioThrottleDevice{{Path: "/dev/nvme0n1", Rate: 50000}}, limits.BlkioDeviceReadIOps)
		assert.Equal(t, []docker.BlkioThrottleDevice{{Path: "/dev/nvme0n1", Rate: 15000}}, limits.BlkioDeviceWriteIOps)
		assert.Equal(t, []docker.BlkioThrottleDevice{{Path: "/dev/nvme0n1", Rate: 500 << 20}}, limits.BlkioDeviceReadBps)
		assert.Equal(t, []docker.BlkioThrottleDevice{{Path: "/dev/nvme0n1", Rate: 250 << 20}}, limits.BlkioDeviceWriteBps)

		assert.Equal(t, "/dev/nvme0n1", resolved.DevicePath)
		assert.Equal(t, uint64(50000), resolved.DeviceReadIOps)
		assert.Equal(t, uint64(15000), resolved.DeviceWriteIOps)
		assert.Equal(t, uint64(500<<20), resolved.DeviceReadBps)
		assert.Equal(t, uint64(250<<20), resolved.DeviceWriteBps)
	})

	t.Run("device_path replaces the found device", func(t *testing.T) {
		cfg := &config.ResourceLimits{DeviceReadIOps: 1000, DevicePath: "/dev/sdb"}

		limits, resolved, err := buildContainerResourceLimits(cfg, nil, storage)
		require.NoError(t, err)

		assert.Equal(t, []docker.BlkioThrottleDevice{{Path: "/dev/sdb", Rate: 1000}}, limits.BlkioDeviceReadIOps)
		assert.Empty(t, limits.BlkioDeviceWriteIOps)
		assert.Equal(t, "/dev/sdb", resolved.DevicePath)
		assert.Zero(t, resolved.DeviceWriteIOps)
	})

	t.Run("device_path works when the lookup failed", func(t *testing.T) {
		cfg := &config.ResourceLimits{DeviceWriteBps: "100mb", DevicePath: "/dev/sdb"}

		limits, _, err := buildContainerResourceLimits(cfg, nil, &StorageInfo{Error: "path not visible"})
		require.NoError(t, err)
		assert.Equal(t, []docker.BlkioThrottleDevice{{Path: "/dev/sdb", Rate: 100 << 20}}, limits.BlkioDeviceWriteBps)
	})

	t.Run("unknown device fails with the reason", func(t *testing.T) {
		cfg := &config.ResourceLimits{DeviceReadIOps: 1000}

		_, _, err := buildContainerResourceLimits(cfg, nil, &StorageInfo{Error: "zfs has no block device"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "zfs has no block device")
		assert.Contains(t, err.Error(), "device_path")

		_, _, err = buildContainerResourceLimits(cfg, nil, nil)
		require.Error(t, err)
	})

	t.Run("no device limits need no device", func(t *testing.T) {
		cfg := &config.ResourceLimits{Memory: "1g"}

		_, resolved, err := buildContainerResourceLimits(cfg, nil, nil)
		require.NoError(t, err)
		assert.Empty(t, resolved.DevicePath)
	})
}

func TestBuildContainerResourceLimitsIOCost(t *testing.T) {
	cfg := &config.ResourceLimits{
		DeviceThrottle: config.DeviceThrottleIOCost,
		DeviceReadIOps: 630000,
		DeviceReadBps:  "3408mb",
	}

	t.Run("a dm device is followed to its one disk", func(t *testing.T) {
		storage := &StorageInfo{Device: &blockdev.Device{
			Name: "dm-0", Path: "/dev/dm-0", MajMin: "253:0", Kind: "device-mapper",
			Backing: []blockdev.Device{{Name: "nvme2n1", Path: "/dev/nvme2n1", MajMin: "259:0"}},
		}}

		limits, resolved, err := buildContainerResourceLimits(cfg, nil, storage)
		require.NoError(t, err)

		// The container gets no io.max throttles: io.cost covers the disk.
		assert.Empty(t, limits.BlkioDeviceReadIOps)
		assert.Empty(t, limits.BlkioDeviceReadBps)

		assert.Equal(t, config.DeviceThrottleIOCost, resolved.DeviceThrottle)
		assert.Equal(t, "/dev/nvme2n1", resolved.DevicePath)
		assert.Equal(t, "259:0", resolved.deviceMajMin)
		assert.Equal(t, uint64(630000), resolved.DeviceReadIOps)
		assert.Equal(t, uint64(3408<<20), resolved.DeviceReadBps)
		assert.Zero(t, resolved.DeviceWriteIOps)
	})

	t.Run("a whole disk is used as it is", func(t *testing.T) {
		storage := &StorageInfo{Device: &blockdev.Device{Name: "nvme0n1", Path: "/dev/nvme0n1", MajMin: "259:1"}}

		_, resolved, err := buildContainerResourceLimits(cfg, nil, storage)
		require.NoError(t, err)
		assert.Equal(t, "/dev/nvme0n1", resolved.DevicePath)
		assert.Equal(t, "259:1", resolved.deviceMajMin)
	})

	t.Run("a stack over more than one disk fails", func(t *testing.T) {
		storage := &StorageInfo{Device: &blockdev.Device{
			Name: "md0", Path: "/dev/md0", MajMin: "9:0",
			Backing: []blockdev.Device{
				{Name: "nvme0n1", Path: "/dev/nvme0n1", MajMin: "259:0"},
				{Name: "nvme1n1", Path: "/dev/nvme1n1", MajMin: "259:1"},
			},
		}}

		_, _, err := buildContainerResourceLimits(cfg, nil, storage)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nvme0n1, nvme1n1")
		assert.Contains(t, err.Error(), "device_path")
	})

	t.Run("an unknown device fails with the reason", func(t *testing.T) {
		_, _, err := buildContainerResourceLimits(cfg, nil, &StorageInfo{Error: "path not visible"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path not visible")
	})
}
