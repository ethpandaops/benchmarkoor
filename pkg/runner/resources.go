package runner

import (
	"fmt"
	mrand "math/rand/v2"
	"strconv"
	"strings"

	"github.com/docker/go-units"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/cpufreq"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/sirupsen/logrus"
)

// selectRandomCPUs picks count random CPUs from available CPUs using Fisher-Yates shuffle.
func selectRandomCPUs(count int) ([]int, error) {
	numCPUs, err := cpu.Counts(true)
	if err != nil {
		return nil, fmt.Errorf("getting CPU count: %w", err)
	}

	if count > numCPUs {
		return nil, fmt.Errorf("requested %d CPUs but only %d available", count, numCPUs)
	}

	// Create slice of all CPU IDs.
	cpus := make([]int, numCPUs)
	for i := range cpus {
		cpus[i] = i
	}

	// Fisher-Yates shuffle (partial - only shuffle first 'count' elements).
	for i := 0; i < count; i++ {
		j := i + mrand.IntN(numCPUs-i)
		cpus[i], cpus[j] = cpus[j], cpus[i]
	}

	return cpus[:count], nil
}

// cpusetString converts a slice of CPU IDs to a comma-separated string.
func cpusetString(cpus []int) string {
	if len(cpus) == 0 {
		return ""
	}

	strs := make([]string, len(cpus))
	for i, c := range cpus {
		strs[i] = strconv.Itoa(c)
	}

	return strings.Join(strs, ",")
}

// buildContainerResourceLimits builds docker.ResourceLimits from config.ResourceLimits.
func buildContainerResourceLimits(cfg *config.ResourceLimits) (*docker.ResourceLimits, *ResolvedResourceLimits, error) {
	if cfg == nil {
		return nil, nil, nil
	}

	containerLimits := &docker.ResourceLimits{}
	resolved := &ResolvedResourceLimits{}

	// Handle CPU pinning.
	if cfg.CpusetCount != nil {
		cpus, err := selectRandomCPUs(*cfg.CpusetCount)
		if err != nil {
			return nil, nil, fmt.Errorf("selecting random CPUs: %w", err)
		}

		containerLimits.CpusetCpus = cpusetString(cpus)
		resolved.CpusetCpus = containerLimits.CpusetCpus
	} else if len(cfg.Cpuset) > 0 {
		containerLimits.CpusetCpus = cpusetString(cfg.Cpuset)
		resolved.CpusetCpus = containerLimits.CpusetCpus
	}

	// Handle memory limit.
	if cfg.Memory != "" {
		memBytes, err := units.RAMInBytes(cfg.Memory)
		if err != nil {
			return nil, nil, fmt.Errorf("parsing memory limit: %w", err)
		}

		containerLimits.MemoryBytes = memBytes
		resolved.Memory = cfg.Memory
		resolved.MemoryBytes = memBytes

		// Handle swap.
		if cfg.SwapDisabled {
			// Set memory-swap equal to memory to disable swap.
			containerLimits.MemorySwapBytes = memBytes
			// Set swappiness to 0.
			swappiness := int64(0)
			containerLimits.MemorySwappiness = &swappiness
			resolved.SwapDisabled = true
		}
	}

	// Handle blkio config.
	if cfg.BlkioConfig != nil {
		blkioCfg := cfg.BlkioConfig
		resolvedBlkio := &ResolvedBlkioConfig{}

		// Process device_read_bps.
		if len(blkioCfg.DeviceReadBps) > 0 {
			containerLimits.BlkioDeviceReadBps, resolvedBlkio.DeviceReadBps = convertBlkioDevicesBps(blkioCfg.DeviceReadBps)
		}

		// Process device_write_bps.
		if len(blkioCfg.DeviceWriteBps) > 0 {
			containerLimits.BlkioDeviceWriteBps, resolvedBlkio.DeviceWriteBps = convertBlkioDevicesBps(blkioCfg.DeviceWriteBps)
		}

		// Process device_read_iops.
		if len(blkioCfg.DeviceReadIOps) > 0 {
			containerLimits.BlkioDeviceReadIOps, resolvedBlkio.DeviceReadIOps = convertBlkioDevicesIOps(blkioCfg.DeviceReadIOps)
		}

		// Process device_write_iops.
		if len(blkioCfg.DeviceWriteIOps) > 0 {
			containerLimits.BlkioDeviceWriteIOps, resolvedBlkio.DeviceWriteIOps = convertBlkioDevicesIOps(blkioCfg.DeviceWriteIOps)
		}

		// Only set if we have any blkio config.
		if len(resolvedBlkio.DeviceReadBps) > 0 || len(resolvedBlkio.DeviceWriteBps) > 0 ||
			len(resolvedBlkio.DeviceReadIOps) > 0 || len(resolvedBlkio.DeviceWriteIOps) > 0 {
			resolved.BlkioConfig = resolvedBlkio
		}
	}

	return containerLimits, resolved, nil
}

// convertBlkioDevicesBps converts config blkio devices with bps rates to docker and resolved formats.
func convertBlkioDevicesBps(devices []config.ThrottleDevice) ([]docker.BlkioThrottleDevice, []ResolvedThrottleDevice) {
	dockerDevices := make([]docker.BlkioThrottleDevice, len(devices))
	resolvedDevices := make([]ResolvedThrottleDevice, len(devices))

	for i, dev := range devices {
		// Parse rate using RAMInBytes (validation already done in config.Validate).
		rate, _ := units.RAMInBytes(dev.Rate)

		dockerDevices[i] = docker.BlkioThrottleDevice{
			Path: dev.Path,
			Rate: uint64(rate),
		}
		resolvedDevices[i] = ResolvedThrottleDevice{
			Path: dev.Path,
			Rate: uint64(rate),
		}
	}

	return dockerDevices, resolvedDevices
}

// convertBlkioDevicesIOps converts config blkio devices with IOPS rates to docker and resolved formats.
func convertBlkioDevicesIOps(devices []config.ThrottleDevice) ([]docker.BlkioThrottleDevice, []ResolvedThrottleDevice) {
	dockerDevices := make([]docker.BlkioThrottleDevice, len(devices))
	resolvedDevices := make([]ResolvedThrottleDevice, len(devices))

	for i, dev := range devices {
		// Parse rate as integer (validation already done in config.Validate).
		rate, _ := strconv.ParseUint(dev.Rate, 10, 64)

		dockerDevices[i] = docker.BlkioThrottleDevice{
			Path: dev.Path,
			Rate: rate,
		}
		resolvedDevices[i] = ResolvedThrottleDevice{
			Path: dev.Path,
			Rate: rate,
		}
	}

	return dockerDevices, resolvedDevices
}

// hasCPUFreqSettings returns true if the resource limits have any CPU frequency settings.
func hasCPUFreqSettings(cfg *config.ResourceLimits) bool {
	if cfg == nil {
		return false
	}
	return cfg.CPUFreq != "" || cfg.CPUTurboBoost != nil || cfg.CPUGovernor != ""
}

// buildCPUFreqConfig builds a cpufreq.Config from resource limits.
func buildCPUFreqConfig(cfg *config.ResourceLimits) *cpufreq.Config {
	if cfg == nil {
		return nil
	}

	cpufreqCfg := &cpufreq.Config{
		Frequency:  cfg.CPUFreq,
		TurboBoost: cfg.CPUTurboBoost,
		Governor:   cfg.CPUGovernor,
	}

	// Default governor to "performance" if frequency is set but governor isn't.
	if cpufreqCfg.Frequency != "" && cpufreqCfg.Governor == "" {
		cpufreqCfg.Governor = "performance"
	}

	return cpufreqCfg
}

// logCPUFreqInfo logs CPU frequency information for the target CPUs.
func logCPUFreqInfo(log logrus.FieldLogger, mgr cpufreq.Manager, targetCPUs []int) {
	infos, err := mgr.GetCPUInfo()
	if err != nil {
		log.WithError(err).Warn("Failed to get CPU frequency info")
		return
	}

	// Filter to target CPUs if specified.
	targetSet := make(map[int]struct{}, len(targetCPUs))
	for _, cpuID := range targetCPUs {
		targetSet[cpuID] = struct{}{}
	}

	for _, info := range infos {
		// Skip CPUs not in target set if targets were specified.
		if len(targetCPUs) > 0 {
			if _, ok := targetSet[info.ID]; !ok {
				continue
			}
		}

		log.WithFields(logrus.Fields{
			"cpu":         info.ID,
			"min_freq":    cpufreq.FormatFrequency(info.MinFreqKHz),
			"max_freq":    cpufreq.FormatFrequency(info.MaxFreqKHz),
			"current":     cpufreq.FormatFrequency(info.CurrentFreqKHz),
			"governor":    info.Governor,
			"scaling_min": cpufreq.FormatFrequency(info.ScalingMinKHz),
			"scaling_max": cpufreq.FormatFrequency(info.ScalingMaxKHz),
		}).Info("CPU frequency info")
	}
}
