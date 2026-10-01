package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/docker/go-units"
	"github.com/ethpandaops/benchmarkoor/pkg/blockdev"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/sirupsen/logrus"
)

// cgroupV2ControllersPath exists only on a host with the unified cgroup
// hierarchy (cgroup v2).
const cgroupV2ControllersPath = "/sys/fs/cgroup/cgroup.controllers"

// StorageInfo describes the block device that holds the client datadir.
type StorageInfo struct {
	// DataPath is the host path of the client data mount.
	DataPath   string           `json:"data_path"`
	Filesystem string           `json:"filesystem,omitempty"`
	Partition  string           `json:"partition,omitempty"`
	Device     *blockdev.Device `json:"device,omitempty"`
	// Error tells why the device is unknown.
	Error string `json:"error,omitempty"`
	// Probe is the measured capacity of the device, without throttles.
	Probe      *blockdev.ProbeResult `json:"probe,omitempty"`
	ProbeError string                `json:"probe_error,omitempty"`
}

// storageProbe is a cached probe outcome.
type storageProbe struct {
	result *blockdev.ProbeResult
	err    error
}

// inspectStorage finds the block device under the client data mount. When
// the storage probe is on, it also measures the device. A failure does not
// stop the run: the reason goes into the result, and the device limits check
// it later. It returns nil on an OS without block device support.
func (r *runner) inspectStorage(
	ctx context.Context,
	log logrus.FieldLogger,
	mount docker.Mount,
) *StorageInfo {
	hostPath, err := r.dataMountHostPath(ctx, mount)
	if err != nil {
		log.WithError(err).Warn("Failed to find the host path of the data mount")

		return &StorageInfo{Error: err.Error()}
	}

	info := &StorageInfo{DataPath: hostPath}

	loc, err := blockdev.Resolve(hostPath)
	if err != nil {
		if errors.Is(err, blockdev.ErrUnsupported) {
			return nil
		}

		log.WithError(err).WithField("path", hostPath).Warn("Failed to find the block device of the data mount")

		info.Error = err.Error()

		return info
	}

	info.Filesystem = loc.Filesystem
	info.Partition = loc.Partition
	info.Device = loc.Device

	fields := logrus.Fields{
		"path":       hostPath,
		"device":     loc.Device.Path,
		"kind":       loc.Device.Kind,
		"filesystem": loc.Filesystem,
		"size":       units.BytesSize(float64(loc.Device.SizeBytes)),
	}

	if loc.Partition != "" {
		fields["partition"] = loc.Partition
	}

	if loc.Device.Model != "" {
		fields["model"] = loc.Device.Model
	}

	if loc.Device.Scheduler != "" {
		fields["scheduler"] = loc.Device.Scheduler
	}

	if len(loc.Device.Backing) > 0 {
		names := make([]string, 0, len(loc.Device.Backing))
		for _, disk := range loc.Device.Backing {
			names = append(names, disk.Name)
		}

		fields["backing"] = strings.Join(names, ",")
	}

	log.WithFields(fields).Info("Data mount block device")

	probeCfg := r.storageProbeConfig()
	if probeCfg == nil {
		return info
	}

	probe := r.probeStorage(ctx, log, loc, *probeCfg)
	if probe.err != nil {
		info.ProbeError = probe.err.Error()

		return info
	}

	info.Probe = probe.result

	return info
}

// dataMountHostPath returns the host path of the client data mount.
func (r *runner) dataMountHostPath(ctx context.Context, mount docker.Mount) (string, error) {
	switch mount.Type {
	case "volume":
		path, err := r.containerMgr.VolumeMountpoint(ctx, mount.Source)
		if err != nil {
			return "", err
		}

		if path == "" {
			return "", fmt.Errorf("volume %s has no mountpoint", mount.Source)
		}

		return path, nil
	case "bind", "":
		return mount.Source, nil
	default:
		return "", fmt.Errorf("data mount type %q has no host path", mount.Type)
	}
}

// storageProbeConfig returns the probe settings, or nil when the probe is off.
// The config is validated at load time.
func (r *runner) storageProbeConfig() *blockdev.ProbeConfig {
	if r.cfg.FullConfig == nil || r.cfg.FullConfig.Runner.StorageProbe == nil ||
		!r.cfg.FullConfig.Runner.StorageProbe.Enabled {
		return nil
	}

	pc := r.cfg.FullConfig.Runner.StorageProbe
	cfg := &blockdev.ProbeConfig{IODepth: pc.IODepth}

	if pc.FileSize != "" {
		cfg.FileSize, _ = units.RAMInBytes(pc.FileSize)
	}

	if pc.Duration != "" {
		cfg.Duration, _ = time.ParseDuration(pc.Duration)
	}

	return cfg
}

// probeStorage measures a device once per filesystem in this process. Later
// instances on the same filesystem use the cached outcome. The lock also
// keeps two probes from running at the same time.
func (r *runner) probeStorage(
	ctx context.Context,
	log logrus.FieldLogger,
	loc *blockdev.Location,
	cfg blockdev.ProbeConfig,
) storageProbe {
	key := strings.Join([]string{loc.Device.Name, loc.Partition, loc.Filesystem}, "|")

	r.storageProbesMu.Lock()
	defer r.storageProbesMu.Unlock()

	if cached, ok := r.storageProbes[key]; ok {
		log.WithField("device", loc.Device.Path).Debug("Using the cached disk probe result")

		return cached
	}

	cfg.Dir = loc.ProbeDir

	start := time.Now()
	result, err := blockdev.Probe(ctx, log, cfg)

	if err != nil {
		// A cancelled probe says nothing about the device, so it is not cached.
		if ctx.Err() != nil {
			return storageProbe{err: err}
		}

		log.WithError(err).WithField("device", loc.Device.Path).Warn("Disk probe failed")
	} else {
		fields := logrus.Fields{
			"device":          loc.Device.Path,
			"rand_read_iops":  int64(result.RandReadIOPS),
			"rand_write_iops": int64(result.RandWriteIOPS),
			"seq_read":        units.BytesSize(result.SeqReadBps) + "/s",
			"seq_write":       units.BytesSize(result.SeqWriteBps) + "/s",
			"took":            time.Since(start).Round(time.Millisecond).String(),
		}

		if lat := result.QD1RandRead; lat != nil {
			fields["qd1_read_iops"] = int64(lat.IOPS)
			fields["qd1_read_p99_us"] = int64(lat.P99Us)
		}

		if lat := result.QD1RandWrite; lat != nil {
			fields["qd1_write_iops"] = int64(lat.IOPS)
			fields["qd1_write_p99_us"] = int64(lat.P99Us)
		}

		log.WithFields(fields).Info("Disk probe result")
	}

	probe := storageProbe{result: result, err: err}
	r.storageProbes[key] = probe

	return probe
}

// applyDeviceLimits adds the device_* resource limits to the container
// limits, as throttles on the block device that holds the datadir. An
// explicit device_path replaces the device that the lookup found. With
// io.cost, the container gets no throttles: the runner sets a model on the
// whole disk instead (see applyIOCostLimits).
func applyDeviceLimits(
	cfg *config.ResourceLimits,
	storage *StorageInfo,
	containerLimits *docker.ResourceLimits,
	resolved *ResolvedResourceLimits,
) error {
	if !cfg.HasDeviceLimits() {
		return nil
	}

	resolveDeviceRates(cfg, resolved)

	if cfg.UsesIOCost() {
		return applyIOCostLimits(cfg, storage, resolved)
	}

	path := cfg.DevicePath
	if path == "" {
		if storage == nil || storage.Device == nil {
			return unknownDeviceError(storage)
		}

		path = storage.Device.Path
	}

	resolved.DevicePath = path

	for _, t := range []struct {
		list *[]docker.BlkioThrottleDevice
		rate uint64
	}{
		{&containerLimits.BlkioDeviceReadBps, resolved.DeviceReadBps},
		{&containerLimits.BlkioDeviceWriteBps, resolved.DeviceWriteBps},
		{&containerLimits.BlkioDeviceReadIOps, resolved.DeviceReadIOps},
		{&containerLimits.BlkioDeviceWriteIOps, resolved.DeviceWriteIOps},
	} {
		if t.rate != 0 {
			*t.list = append(*t.list, docker.BlkioThrottleDevice{Path: path, Rate: t.rate})
		}
	}

	return nil
}

// resolveDeviceRates copies the device_* rates into resolved, with the bps
// sizes in bytes.
func resolveDeviceRates(cfg *config.ResourceLimits, resolved *ResolvedResourceLimits) {
	// The config validation parsed the sizes already.
	if cfg.DeviceReadBps != "" {
		bps, _ := units.RAMInBytes(cfg.DeviceReadBps)
		resolved.DeviceReadBps = uint64(bps) //nolint:gosec // Validated as positive.
	}

	if cfg.DeviceWriteBps != "" {
		bps, _ := units.RAMInBytes(cfg.DeviceWriteBps)
		resolved.DeviceWriteBps = uint64(bps) //nolint:gosec // Validated as positive.
	}

	resolved.DeviceReadIOps = cfg.DeviceReadIOps
	resolved.DeviceWriteIOps = cfg.DeviceWriteIOps
}

// applyIOCostLimits picks the disk for the io.cost model. io.cost works only
// on a whole blk-mq disk, so a device-mapper or md device is followed to the
// one disk under it. The runner writes the model before the client starts.
func applyIOCostLimits(
	cfg *config.ResourceLimits,
	storage *StorageInfo,
	resolved *ResolvedResourceLimits,
) error {
	resolved.DeviceThrottle = config.DeviceThrottleIOCost

	if cfg.DevicePath != "" {
		majMin, err := blockdev.DeviceNumber(cfg.DevicePath)
		if err != nil {
			return fmt.Errorf("reading the device number of device_path %s: %w", cfg.DevicePath, err)
		}

		resolved.DevicePath = cfg.DevicePath
		resolved.deviceMajMin = majMin

		return nil
	}

	if storage == nil || storage.Device == nil {
		return unknownDeviceError(storage)
	}

	disk, err := ioCostDisk(storage.Device)
	if err != nil {
		return err
	}

	resolved.DevicePath = disk.Path
	resolved.deviceMajMin = disk.MajMin

	return nil
}

// ioCostDisk returns the one physical disk under dev. A stack over more than
// one disk, such as md RAID0, has no single disk to model.
func ioCostDisk(dev *blockdev.Device) (*blockdev.Device, error) {
	disk := dev

	switch len(dev.Backing) {
	case 0:
	case 1:
		disk = &dev.Backing[0]
	default:
		names := make([]string, 0, len(dev.Backing))
		for _, b := range dev.Backing {
			names = append(names, b.Name)
		}

		return nil, fmt.Errorf("device_throttle %q needs one disk, but %s is on %d disks (%s); "+
			"set resource_limits.device_path to name one disk",
			config.DeviceThrottleIOCost, dev.Path, len(dev.Backing), strings.Join(names, ", "))
	}

	if disk.MajMin == "" {
		return nil, fmt.Errorf("device_throttle %q: the device number of %s is unknown",
			config.DeviceThrottleIOCost, disk.Path)
	}

	return disk, nil
}

// unknownDeviceError explains why the device_* limits have no device.
func unknownDeviceError(storage *StorageInfo) error {
	reason := "block device lookup is only supported on Linux"
	if storage != nil && storage.Error != "" {
		reason = storage.Error
	}

	return fmt.Errorf("resource_limits.device_* limits need the block device of the client datadir, "+
		"which is unknown: %s (set resource_limits.device_path to name the device)", reason)
}

// isCgroupV2 reports if the host uses the unified cgroup hierarchy. Only
// cgroup v2 throttles buffered writes, at writeback time.
func isCgroupV2() bool {
	_, err := os.Stat(cgroupV2ControllersPath)

	return err == nil
}
