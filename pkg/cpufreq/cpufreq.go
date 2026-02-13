package cpufreq

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

// Manager controls CPU frequency settings for benchmark CPUs.
type Manager interface {
	Start(ctx context.Context) error
	Stop() error
	// Apply applies CPU frequency settings to the specified CPUs.
	// If cpus is empty, settings are applied to all online CPUs.
	Apply(ctx context.Context, cfg *Config, cpus []int) error
	// Restore restores original CPU frequency settings.
	Restore(ctx context.Context) error
	// GetCPUInfo returns CPU frequency info for all online CPUs.
	GetCPUInfo() ([]CPUInfo, error)
}

// Config holds CPU frequency configuration.
type Config struct {
	Frequency  string // "2000MHz", "2.4GHz", "MAX", or empty (unchanged)
	TurboBoost *bool  // nil=unchanged, true=enable, false=disable
	Governor   string // Governor name, defaults to "performance" if Frequency is set
}

// CPUInfo contains frequency information for a single CPU.
type CPUInfo struct {
	ID             int
	MinFreqKHz     uint64
	MaxFreqKHz     uint64
	CurrentFreqKHz uint64
	Governor       string
	AvailGovernors []string
	ScalingMinKHz  uint64
	ScalingMaxKHz  uint64
}

// OriginalSettings stores the original CPU frequency settings before modification.
type OriginalSettings struct {
	CPUs       map[int]*CPUSettings `json:"cpus"`
	TurboBoost *TurboBoostSettings  `json:"turbo_boost,omitempty"`
}

// CPUSettings stores settings for a single CPU.
type CPUSettings struct {
	ScalingMaxKHz uint64 `json:"scaling_max_khz"`
	ScalingMinKHz uint64 `json:"scaling_min_khz"`
	Governor      string `json:"governor"`
}

// TurboBoostSettings stores turbo boost settings.
type TurboBoostSettings struct {
	Type  string `json:"type"`  // "intel" or "amd"
	Value int    `json:"value"` // Original sysfs value
}

// NewManager creates a new CPU frequency manager.
// sysfsBasePath is the base path for CPU sysfs files (e.g. "/sys/devices/system/cpu").
func NewManager(log logrus.FieldLogger, cacheDir, sysfsBasePath string) Manager {
	return &manager{
		log:           log.WithField("component", "cpufreq"),
		cacheDir:      cacheDir,
		sysfsBasePath: sysfsBasePath,
		done:          make(chan struct{}),
	}
}

type manager struct {
	log           logrus.FieldLogger
	cacheDir      string
	sysfsBasePath string

	mu               sync.Mutex
	originalSettings *OriginalSettings
	stateFile        string
	done             chan struct{}
}

// Ensure interface compliance.
var _ Manager = (*manager)(nil)

// Start initializes the manager.
func (m *manager) Start(ctx context.Context) error {
	m.log.Debug("CPU frequency manager started")
	return nil
}

// Stop restores original settings and cleans up.
func (m *manager) Stop() error {
	close(m.done)

	// Restore original settings if any were saved.
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.originalSettings != nil {
		if err := m.restoreSettings(context.Background()); err != nil {
			m.log.WithError(err).Warn("Failed to restore CPU frequency settings")
		}
	}

	m.log.Debug("CPU frequency manager stopped")
	return nil
}

// Apply applies CPU frequency settings to the specified CPUs.
func (m *manager) Apply(ctx context.Context, cfg *Config, cpus []int) error {
	if cfg == nil {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Determine target CPUs.
	if len(cpus) == 0 {
		var err error
		cpus, err = getOnlineCPUs(m.sysfsBasePath)
		if err != nil {
			return fmt.Errorf("getting online CPUs: %w", err)
		}
	}

	m.log.WithField("cpus", cpus).Debug("Applying CPU frequency settings")

	// Save original settings before making changes.
	if m.originalSettings == nil {
		original, err := m.captureOriginalSettings(cpus)
		if err != nil {
			return fmt.Errorf("capturing original settings: %w", err)
		}
		m.originalSettings = original

		// Persist state file for crash recovery.
		stateFile, err := SaveState(m.cacheDir, original)
		if err != nil {
			m.log.WithError(err).Warn("Failed to save CPU frequency state file")
		} else {
			m.stateFile = stateFile
			m.log.WithField("state_file", stateFile).Debug("Saved CPU frequency state")
		}
	}

	// Parse frequency if specified.
	var targetFreqKHz uint64
	if cfg.Frequency != "" {
		var err error
		targetFreqKHz, err = ParseFrequency(cfg.Frequency)
		if err != nil {
			return fmt.Errorf("parsing frequency %q: %w", cfg.Frequency, err)
		}
	}

	// Apply governor first (some systems require this before frequency changes).
	if cfg.Governor != "" {
		for _, cpuID := range cpus {
			if err := setGovernor(m.sysfsBasePath, cpuID, cfg.Governor); err != nil {
				return fmt.Errorf("setting governor for CPU %d: %w", cpuID, err)
			}
			m.log.WithFields(logrus.Fields{
				"cpu":      cpuID,
				"governor": cfg.Governor,
			}).Debug("Set CPU governor")
		}
	}

	// Apply frequency.
	if cfg.Frequency != "" {
		for _, cpuID := range cpus {
			// Get CPU's actual max frequency.
			cpuMaxKHz, err := getCPUInfoMaxFreq(m.sysfsBasePath, cpuID)
			if err != nil {
				return fmt.Errorf("getting max frequency for CPU %d: %w", cpuID, err)
			}

			// Handle "MAX" special value.
			freqKHz := targetFreqKHz
			if strings.ToUpper(cfg.Frequency) == "MAX" {
				freqKHz = cpuMaxKHz
			}

			// Validate frequency is within bounds.
			cpuMinKHz, err := getCPUInfoMinFreq(m.sysfsBasePath, cpuID)
			if err != nil {
				return fmt.Errorf("getting min frequency for CPU %d: %w", cpuID, err)
			}

			if freqKHz < cpuMinKHz || freqKHz > cpuMaxKHz {
				return fmt.Errorf(
					"frequency %d kHz out of range for CPU %d (min: %d, max: %d)",
					freqKHz, cpuID, cpuMinKHz, cpuMaxKHz,
				)
			}

			// Set both min and max to the target frequency for a fixed frequency.
			if err := setScalingMinFreq(m.sysfsBasePath, cpuID, freqKHz); err != nil {
				// Try setting max first if min fails (some systems require max >= min).
				if setErr := setScalingMaxFreq(m.sysfsBasePath, cpuID, freqKHz); setErr != nil {
					return fmt.Errorf("setting frequency for CPU %d: %w", cpuID, setErr)
				}
				if err := setScalingMinFreq(m.sysfsBasePath, cpuID, freqKHz); err != nil {
					return fmt.Errorf("setting min frequency for CPU %d: %w", cpuID, err)
				}
			} else {
				if err := setScalingMaxFreq(m.sysfsBasePath, cpuID, freqKHz); err != nil {
					return fmt.Errorf("setting max frequency for CPU %d: %w", cpuID, err)
				}
			}

			m.log.WithFields(logrus.Fields{
				"cpu":      cpuID,
				"freq_khz": freqKHz,
			}).Debug("Set CPU frequency")
		}
	}

	// Apply turbo boost setting.
	if cfg.TurboBoost != nil {
		if err := setTurboBoost(m.sysfsBasePath, *cfg.TurboBoost); err != nil {
			// Turbo boost control might not be available, log and continue.
			m.log.WithError(err).Warn("Failed to set turbo boost")
		} else {
			m.log.WithField("enabled", *cfg.TurboBoost).Info("Set turbo boost")
		}
	}

	return nil
}

// Restore restores original CPU frequency settings.
func (m *manager) Restore(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.restoreSettings(ctx)
}

// restoreSettings restores original settings (must be called with lock held).
func (m *manager) restoreSettings(ctx context.Context) error {
	if m.originalSettings == nil {
		return nil
	}

	m.log.Info("Restoring original CPU frequency settings")

	// Restore turbo boost first.
	if m.originalSettings.TurboBoost != nil {
		if err := restoreTurboBoost(m.sysfsBasePath, m.originalSettings.TurboBoost); err != nil {
			m.log.WithError(err).Warn("Failed to restore turbo boost")
		}
	}

	// Restore per-CPU settings.
	for cpuID, settings := range m.originalSettings.CPUs {
		// Restore governor.
		if settings.Governor != "" {
			if err := setGovernor(m.sysfsBasePath, cpuID, settings.Governor); err != nil {
				m.log.WithFields(logrus.Fields{
					"cpu":      cpuID,
					"governor": settings.Governor,
				}).WithError(err).Warn("Failed to restore governor")
			}
		}

		// Restore scaling frequencies (max first to ensure min <= max).
		if settings.ScalingMaxKHz > 0 {
			if err := setScalingMaxFreq(m.sysfsBasePath, cpuID, settings.ScalingMaxKHz); err != nil {
				m.log.WithFields(logrus.Fields{
					"cpu":     cpuID,
					"max_khz": settings.ScalingMaxKHz,
				}).WithError(err).Warn("Failed to restore max frequency")
			}
		}
		if settings.ScalingMinKHz > 0 {
			if err := setScalingMinFreq(m.sysfsBasePath, cpuID, settings.ScalingMinKHz); err != nil {
				m.log.WithFields(logrus.Fields{
					"cpu":     cpuID,
					"min_khz": settings.ScalingMinKHz,
				}).WithError(err).Warn("Failed to restore min frequency")
			}
		}
	}

	// Clean up state file.
	if m.stateFile != "" {
		if err := RemoveStateFile(m.stateFile); err != nil {
			m.log.WithError(err).Warn("Failed to remove state file")
		}
		m.stateFile = ""
	}

	m.originalSettings = nil
	m.log.Info("CPU frequency settings restored")

	return nil
}

// GetCPUInfo returns CPU frequency info for all online CPUs.
func (m *manager) GetCPUInfo() ([]CPUInfo, error) {
	cpus, err := getOnlineCPUs(m.sysfsBasePath)
	if err != nil {
		return nil, fmt.Errorf("getting online CPUs: %w", err)
	}

	infos := make([]CPUInfo, 0, len(cpus))
	for _, cpuID := range cpus {
		info, err := getCPUInfo(m.sysfsBasePath, cpuID)
		if err != nil {
			m.log.WithFields(logrus.Fields{
				"cpu": cpuID,
			}).WithError(err).Warn("Failed to get CPU info")
			continue
		}
		infos = append(infos, *info)
	}

	return infos, nil
}

// captureOriginalSettings captures current CPU frequency settings for the given CPUs.
func (m *manager) captureOriginalSettings(cpus []int) (*OriginalSettings, error) {
	original := &OriginalSettings{
		CPUs: make(map[int]*CPUSettings, len(cpus)),
	}

	for _, cpuID := range cpus {
		settings := &CPUSettings{}

		// Capture current governor.
		gov, err := getGovernor(m.sysfsBasePath, cpuID)
		if err != nil {
			m.log.WithField("cpu", cpuID).WithError(err).Warn("Failed to get governor")
		} else {
			settings.Governor = gov
		}

		// Capture current scaling frequencies.
		minKHz, err := getScalingMinFreq(m.sysfsBasePath, cpuID)
		if err != nil {
			m.log.WithField("cpu", cpuID).WithError(err).Warn("Failed to get scaling min freq")
		} else {
			settings.ScalingMinKHz = minKHz
		}

		maxKHz, err := getScalingMaxFreq(m.sysfsBasePath, cpuID)
		if err != nil {
			m.log.WithField("cpu", cpuID).WithError(err).Warn("Failed to get scaling max freq")
		} else {
			settings.ScalingMaxKHz = maxKHz
		}

		original.CPUs[cpuID] = settings
	}

	// Capture turbo boost state.
	turboSettings, err := captureTurboBoostSettings(m.sysfsBasePath)
	if err != nil {
		m.log.WithError(err).Debug("Turbo boost settings not available")
	} else {
		original.TurboBoost = turboSettings
	}

	return original, nil
}

// ParseFrequency parses a frequency string and returns the value in kHz.
// Supported formats: "2000MHz", "2.4GHz", "2400000KHz", "2400000", "MAX".
func ParseFrequency(freq string) (uint64, error) {
	freq = strings.TrimSpace(freq)
	if freq == "" {
		return 0, fmt.Errorf("empty frequency string")
	}

	// Handle "MAX" special value.
	if strings.ToUpper(freq) == "MAX" {
		return 0, nil // Caller should handle MAX specially.
	}

	// Parse with unit suffix.
	re := regexp.MustCompile(`(?i)^(\d+(?:\.\d+)?)\s*(mhz|ghz|khz)?$`)
	matches := re.FindStringSubmatch(freq)
	if matches == nil {
		return 0, fmt.Errorf("invalid frequency format: %s", freq)
	}

	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("parsing frequency value: %w", err)
	}

	unit := strings.ToLower(matches[2])
	var kHz uint64

	switch unit {
	case "ghz":
		kHz = uint64(value * 1_000_000)
	case "mhz":
		kHz = uint64(value * 1_000)
	case "khz":
		kHz = uint64(value)
	case "":
		// Assume kHz if no unit specified.
		kHz = uint64(value)
	default:
		return 0, fmt.Errorf("unknown frequency unit: %s", unit)
	}

	if kHz == 0 {
		return 0, fmt.Errorf("frequency must be greater than 0")
	}

	return kHz, nil
}

// FormatFrequency formats a frequency in kHz to a human-readable string.
func FormatFrequency(kHz uint64) string {
	if kHz >= 1_000_000 {
		return fmt.Sprintf("%.2f GHz", float64(kHz)/1_000_000)
	}
	if kHz >= 1_000 {
		return fmt.Sprintf("%.0f MHz", float64(kHz)/1_000)
	}
	return fmt.Sprintf("%d kHz", kHz)
}
