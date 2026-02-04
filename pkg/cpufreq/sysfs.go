package cpufreq

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// sysfs paths for CPU frequency control.
	sysfsCPUPath   = "/sys/devices/system/cpu"
	cpufreqSubdir  = "cpufreq"
	cpuOnlinePath  = "/sys/devices/system/cpu/online"
	cpuPresentPath = "/sys/devices/system/cpu/present"

	// Intel turbo boost control.
	intelNoTurboPath = "/sys/devices/system/cpu/intel_pstate/no_turbo"
	// AMD turbo boost control.
	amdBoostPath = "/sys/devices/system/cpu/cpufreq/boost"
)

// cpufreq sysfs files.
const (
	scalingMinFreqFile   = "scaling_min_freq"
	scalingMaxFreqFile   = "scaling_max_freq"
	scalingCurFreqFile   = "scaling_cur_freq"
	scalingGovernorFile  = "scaling_governor"
	scalingAvailGovsFile = "scaling_available_governors"
	cpuinfoMinFreqFile   = "cpuinfo_min_freq"
	cpuinfoMaxFreqFile   = "cpuinfo_max_freq"
	cpuinfoCurFreqFile   = "cpuinfo_cur_freq"
)

// getOnlineCPUs returns the list of online CPU IDs.
func getOnlineCPUs() ([]int, error) {
	// Try to read online CPUs file first.
	data, err := os.ReadFile(cpuOnlinePath)
	if err == nil {
		return parseCPURange(strings.TrimSpace(string(data)))
	}

	// Fall back to present CPUs if online file doesn't exist.
	data, err = os.ReadFile(cpuPresentPath)
	if err != nil {
		return nil, fmt.Errorf("reading CPU online/present: %w", err)
	}

	return parseCPURange(strings.TrimSpace(string(data)))
}

// parseCPURange parses CPU range strings like "0-7" or "0,2,4-6".
func parseCPURange(rangeStr string) ([]int, error) {
	if rangeStr == "" {
		return nil, nil
	}

	var cpus []int
	parts := strings.Split(rangeStr, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			// Range like "0-7".
			bounds := strings.Split(part, "-")
			if len(bounds) != 2 {
				return nil, fmt.Errorf("invalid CPU range: %s", part)
			}

			start, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
			if err != nil {
				return nil, fmt.Errorf("parsing CPU range start: %w", err)
			}

			end, err := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil {
				return nil, fmt.Errorf("parsing CPU range end: %w", err)
			}

			for i := start; i <= end; i++ {
				cpus = append(cpus, i)
			}
		} else {
			// Single CPU like "0".
			cpuID, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("parsing CPU ID: %w", err)
			}
			cpus = append(cpus, cpuID)
		}
	}

	return cpus, nil
}

// cpufreqPath returns the path to a cpufreq file for a given CPU.
func cpufreqPath(cpuID int, filename string) string {
	return filepath.Join(sysfsCPUPath, fmt.Sprintf("cpu%d", cpuID), cpufreqSubdir, filename)
}

// readSysfsUint64 reads a uint64 value from a sysfs file.
func readSysfsUint64(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", path, err)
	}

	value, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing %s: %w", path, err)
	}

	return value, nil
}

// writeSysfsUint64 writes a uint64 value to a sysfs file.
func writeSysfsUint64(path string, value uint64) error {
	data := []byte(strconv.FormatUint(value, 10))
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// readSysfsString reads a string value from a sysfs file.
func readSysfsString(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// writeSysfsString writes a string value to a sysfs file.
func writeSysfsString(path, value string) error {
	if err := os.WriteFile(path, []byte(value), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// getCPUInfoMaxFreq returns the maximum hardware frequency for a CPU in kHz.
func getCPUInfoMaxFreq(cpuID int) (uint64, error) {
	return readSysfsUint64(cpufreqPath(cpuID, cpuinfoMaxFreqFile))
}

// getCPUInfoMinFreq returns the minimum hardware frequency for a CPU in kHz.
func getCPUInfoMinFreq(cpuID int) (uint64, error) {
	return readSysfsUint64(cpufreqPath(cpuID, cpuinfoMinFreqFile))
}

// getCPUInfoCurFreq returns the current frequency for a CPU in kHz.
func getCPUInfoCurFreq(cpuID int) (uint64, error) {
	return readSysfsUint64(cpufreqPath(cpuID, cpuinfoCurFreqFile))
}

// getScalingMaxFreq returns the current scaling maximum frequency for a CPU in kHz.
func getScalingMaxFreq(cpuID int) (uint64, error) {
	return readSysfsUint64(cpufreqPath(cpuID, scalingMaxFreqFile))
}

// getScalingMinFreq returns the current scaling minimum frequency for a CPU in kHz.
func getScalingMinFreq(cpuID int) (uint64, error) {
	return readSysfsUint64(cpufreqPath(cpuID, scalingMinFreqFile))
}

// getScalingCurFreq returns the current scaling frequency for a CPU in kHz.
func getScalingCurFreq(cpuID int) (uint64, error) {
	return readSysfsUint64(cpufreqPath(cpuID, scalingCurFreqFile))
}

// setScalingMaxFreq sets the scaling maximum frequency for a CPU in kHz.
func setScalingMaxFreq(cpuID int, kHz uint64) error {
	return writeSysfsUint64(cpufreqPath(cpuID, scalingMaxFreqFile), kHz)
}

// setScalingMinFreq sets the scaling minimum frequency for a CPU in kHz.
func setScalingMinFreq(cpuID int, kHz uint64) error {
	return writeSysfsUint64(cpufreqPath(cpuID, scalingMinFreqFile), kHz)
}

// getGovernor returns the current governor for a CPU.
func getGovernor(cpuID int) (string, error) {
	return readSysfsString(cpufreqPath(cpuID, scalingGovernorFile))
}

// setGovernor sets the governor for a CPU.
func setGovernor(cpuID int, governor string) error {
	return writeSysfsString(cpufreqPath(cpuID, scalingGovernorFile), governor)
}

// getAvailableGovernors returns the list of available governors for a CPU.
func getAvailableGovernors(cpuID int) ([]string, error) {
	data, err := readSysfsString(cpufreqPath(cpuID, scalingAvailGovsFile))
	if err != nil {
		return nil, err
	}

	govs := strings.Fields(data)
	return govs, nil
}

// getCPUInfo returns comprehensive CPU frequency info for a single CPU.
func getCPUInfo(cpuID int) (*CPUInfo, error) {
	info := &CPUInfo{ID: cpuID}

	// Get hardware frequency bounds.
	if minKHz, err := getCPUInfoMinFreq(cpuID); err == nil {
		info.MinFreqKHz = minKHz
	}
	if maxKHz, err := getCPUInfoMaxFreq(cpuID); err == nil {
		info.MaxFreqKHz = maxKHz
	}

	// Get current frequency.
	if curKHz, err := getScalingCurFreq(cpuID); err == nil {
		info.CurrentFreqKHz = curKHz
	} else if curKHz, err := getCPUInfoCurFreq(cpuID); err == nil {
		info.CurrentFreqKHz = curKHz
	}

	// Get scaling bounds.
	if minKHz, err := getScalingMinFreq(cpuID); err == nil {
		info.ScalingMinKHz = minKHz
	}
	if maxKHz, err := getScalingMaxFreq(cpuID); err == nil {
		info.ScalingMaxKHz = maxKHz
	}

	// Get governor info.
	if gov, err := getGovernor(cpuID); err == nil {
		info.Governor = gov
	}
	if govs, err := getAvailableGovernors(cpuID); err == nil {
		info.AvailGovernors = govs
	}

	return info, nil
}

// TurboBoostType represents the type of turbo boost control available.
type TurboBoostType string

const (
	TurboBoostIntel TurboBoostType = "intel"
	TurboBoostAMD   TurboBoostType = "amd"
	TurboBoostNone  TurboBoostType = "none"
)

// detectTurboBoostType detects which turbo boost control is available.
func detectTurboBoostType() TurboBoostType {
	if _, err := os.Stat(intelNoTurboPath); err == nil {
		return TurboBoostIntel
	}
	if _, err := os.Stat(amdBoostPath); err == nil {
		return TurboBoostAMD
	}
	return TurboBoostNone
}

// GetTurboBoostEnabled returns whether turbo boost is currently enabled.
func GetTurboBoostEnabled() (bool, TurboBoostType, error) {
	turboType := detectTurboBoostType()

	switch turboType {
	case TurboBoostIntel:
		// Intel: 0 = turbo enabled, 1 = turbo disabled.
		val, err := readSysfsUint64(intelNoTurboPath)
		if err != nil {
			return false, turboType, err
		}
		return val == 0, turboType, nil

	case TurboBoostAMD:
		// AMD: 1 = turbo enabled, 0 = turbo disabled.
		val, err := readSysfsUint64(amdBoostPath)
		if err != nil {
			return false, turboType, err
		}
		return val == 1, turboType, nil

	default:
		return false, TurboBoostNone, fmt.Errorf("turbo boost control not available")
	}
}

// setTurboBoost enables or disables turbo boost.
func setTurboBoost(enabled bool) error {
	turboType := detectTurboBoostType()

	switch turboType {
	case TurboBoostIntel:
		// Intel: 0 = turbo enabled, 1 = turbo disabled.
		var val uint64
		if !enabled {
			val = 1
		}
		return writeSysfsUint64(intelNoTurboPath, val)

	case TurboBoostAMD:
		// AMD: 1 = turbo enabled, 0 = turbo disabled.
		var val uint64
		if enabled {
			val = 1
		}
		return writeSysfsUint64(amdBoostPath, val)

	default:
		return fmt.Errorf("turbo boost control not available")
	}
}

// captureTurboBoostSettings captures the current turbo boost settings.
func captureTurboBoostSettings() (*TurboBoostSettings, error) {
	turboType := detectTurboBoostType()

	switch turboType {
	case TurboBoostIntel:
		val, err := readSysfsUint64(intelNoTurboPath)
		if err != nil {
			return nil, err
		}
		return &TurboBoostSettings{
			Type:  string(TurboBoostIntel),
			Value: int(val),
		}, nil

	case TurboBoostAMD:
		val, err := readSysfsUint64(amdBoostPath)
		if err != nil {
			return nil, err
		}
		return &TurboBoostSettings{
			Type:  string(TurboBoostAMD),
			Value: int(val),
		}, nil

	default:
		return nil, fmt.Errorf("turbo boost control not available")
	}
}

// restoreTurboBoost restores turbo boost to the original setting.
func restoreTurboBoost(settings *TurboBoostSettings) error {
	if settings == nil {
		return nil
	}

	switch TurboBoostType(settings.Type) {
	case TurboBoostIntel:
		return writeSysfsUint64(intelNoTurboPath, uint64(settings.Value))
	case TurboBoostAMD:
		return writeSysfsUint64(amdBoostPath, uint64(settings.Value))
	default:
		return fmt.Errorf("unknown turbo boost type: %s", settings.Type)
	}
}

// IsCPUFreqSupported checks if CPU frequency control is supported on this system.
func IsCPUFreqSupported() bool {
	// Check if cpufreq subsystem is available.
	cpus, err := getOnlineCPUs()
	if err != nil || len(cpus) == 0 {
		return false
	}

	// Check if we can read from the first CPU's cpufreq directory.
	path := cpufreqPath(cpus[0], scalingGovernorFile)
	_, err = os.Stat(path)
	return err == nil
}

// HasWriteAccess checks if we have write access to CPU frequency sysfs files.
func HasWriteAccess() error {
	cpus, err := getOnlineCPUs()
	if err != nil {
		return fmt.Errorf("getting online CPUs: %w", err)
	}

	if len(cpus) == 0 {
		return fmt.Errorf("no online CPUs found")
	}

	// Try to open the scaling_max_freq file for writing.
	path := cpufreqPath(cpus[0], scalingMaxFreqFile)
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("no write permission to %s (requires root)", path)
		}
		return fmt.Errorf("accessing %s: %w", path, err)
	}
	_ = file.Close()

	return nil
}

// ValidateGovernor checks if the specified governor is available on the system.
func ValidateGovernor(governor string) error {
	cpus, err := getOnlineCPUs()
	if err != nil {
		return fmt.Errorf("getting online CPUs: %w", err)
	}

	if len(cpus) == 0 {
		return fmt.Errorf("no online CPUs found")
	}

	availGovs, err := getAvailableGovernors(cpus[0])
	if err != nil {
		return fmt.Errorf("getting available governors: %w", err)
	}

	for _, avail := range availGovs {
		if avail == governor {
			return nil
		}
	}

	return fmt.Errorf("governor %q not available (available: %s)", governor, strings.Join(availGovs, ", "))
}

// ValidateFrequency validates that the specified frequency is within system bounds.
func ValidateFrequency(freqKHz uint64) error {
	cpus, err := getOnlineCPUs()
	if err != nil {
		return fmt.Errorf("getting online CPUs: %w", err)
	}

	if len(cpus) == 0 {
		return fmt.Errorf("no online CPUs found")
	}

	// Check frequency against first CPU (assume all CPUs have same bounds).
	minKHz, err := getCPUInfoMinFreq(cpus[0])
	if err != nil {
		return fmt.Errorf("getting min frequency: %w", err)
	}

	maxKHz, err := getCPUInfoMaxFreq(cpus[0])
	if err != nil {
		return fmt.Errorf("getting max frequency: %w", err)
	}

	if freqKHz < minKHz || freqKHz > maxKHz {
		return fmt.Errorf(
			"frequency %d kHz out of range (min: %d, max: %d)",
			freqKHz, minKHz, maxKHz,
		)
	}

	return nil
}
