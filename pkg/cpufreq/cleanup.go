package cpufreq

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	// stateFilePrefix is the prefix for CPU frequency state files.
	stateFilePrefix = "benchmarkoor-cpufreq-"
	// stateFileSuffix is the suffix for CPU frequency state files.
	stateFileSuffix = ".json"
)

// StateFile represents a CPU frequency state file for orphan detection.
type StateFile struct {
	Path      string
	Timestamp time.Time
}

// SaveState saves the original CPU frequency settings to a state file.
func SaveState(cacheDir string, settings *OriginalSettings) (string, error) {
	if cacheDir == "" {
		// Use system temp directory if no cache directory specified.
		cacheDir = os.TempDir()
	}

	// Ensure cache directory exists.
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("creating cache directory: %w", err)
	}

	// Generate state file name with timestamp.
	timestamp := time.Now().Unix()
	filename := fmt.Sprintf("%s%d%s", stateFilePrefix, timestamp, stateFileSuffix)
	statePath := filepath.Join(cacheDir, filename)

	// Marshal settings to JSON.
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling settings: %w", err)
	}

	// Write state file.
	if err := os.WriteFile(statePath, data, 0644); err != nil {
		return "", fmt.Errorf("writing state file: %w", err)
	}

	return statePath, nil
}

// LoadState loads CPU frequency settings from a state file.
func LoadState(statePath string) (*OriginalSettings, error) {
	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil, fmt.Errorf("reading state file: %w", err)
	}

	var settings OriginalSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("parsing state file: %w", err)
	}

	return &settings, nil
}

// RemoveStateFile removes a state file.
func RemoveStateFile(statePath string) error {
	if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing state file: %w", err)
	}
	return nil
}

// ListOrphanedStateFiles finds state files left behind by interrupted runs.
func ListOrphanedStateFiles(cacheDir string) ([]StateFile, error) {
	if cacheDir == "" {
		cacheDir = os.TempDir()
	}

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading cache directory: %w", err)
	}

	var stateFiles []StateFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasPrefix(name, stateFilePrefix) || !strings.HasSuffix(name, stateFileSuffix) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		stateFiles = append(stateFiles, StateFile{
			Path:      filepath.Join(cacheDir, name),
			Timestamp: info.ModTime(),
		})
	}

	return stateFiles, nil
}

// RestoreFromStateFile restores CPU frequency settings from a state file and removes it.
func RestoreFromStateFile(ctx context.Context, log logrus.FieldLogger, statePath string) error {
	settings, err := LoadState(statePath)
	if err != nil {
		return fmt.Errorf("loading state: %w", err)
	}

	log.WithField("state_file", statePath).Info("Restoring CPU frequency settings from state file")

	// Restore turbo boost first.
	if settings.TurboBoost != nil {
		if err := restoreTurboBoost(settings.TurboBoost); err != nil {
			log.WithError(err).Warn("Failed to restore turbo boost")
		} else {
			log.WithField("type", settings.TurboBoost.Type).Info("Restored turbo boost setting")
		}
	}

	// Restore per-CPU settings.
	for cpuID, cpuSettings := range settings.CPUs {
		// Restore governor.
		if cpuSettings.Governor != "" {
			if err := setGovernor(cpuID, cpuSettings.Governor); err != nil {
				log.WithFields(logrus.Fields{
					"cpu":      cpuID,
					"governor": cpuSettings.Governor,
				}).WithError(err).Warn("Failed to restore governor")
			}
		}

		// Restore scaling frequencies (max first to ensure min <= max).
		if cpuSettings.ScalingMaxKHz > 0 {
			if err := setScalingMaxFreq(cpuID, cpuSettings.ScalingMaxKHz); err != nil {
				log.WithFields(logrus.Fields{
					"cpu":     cpuID,
					"max_khz": cpuSettings.ScalingMaxKHz,
				}).WithError(err).Warn("Failed to restore max frequency")
			}
		}
		if cpuSettings.ScalingMinKHz > 0 {
			if err := setScalingMinFreq(cpuID, cpuSettings.ScalingMinKHz); err != nil {
				log.WithFields(logrus.Fields{
					"cpu":     cpuID,
					"min_khz": cpuSettings.ScalingMinKHz,
				}).WithError(err).Warn("Failed to restore min frequency")
			}
		}
	}

	// Remove the state file after successful restoration.
	if err := RemoveStateFile(statePath); err != nil {
		log.WithError(err).Warn("Failed to remove state file")
	}

	log.Info("CPU frequency settings restored")

	return nil
}

// CleanupOrphanedCPUFreqState restores settings from all orphaned state files.
func CleanupOrphanedCPUFreqState(ctx context.Context, log logrus.FieldLogger, stateFiles []StateFile) error {
	for _, sf := range stateFiles {
		if err := RestoreFromStateFile(ctx, log, sf.Path); err != nil {
			log.WithError(err).WithField("state_file", sf.Path).Warn("Failed to restore from state file")
			// Continue with other files.
			continue
		}
	}
	return nil
}
