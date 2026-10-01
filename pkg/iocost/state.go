package iocost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	stateFilePrefix = "benchmarkoor-iocost-"
	stateFileSuffix = ".json"
)

// State is the content of a state file: the original settings of the
// devices that a run changed.
type State struct {
	CgroupPath string           `json:"cgroup_path"`
	Devices    []DeviceSettings `json:"devices"`
}

// StateFile is a state file that an interrupted run left behind.
type StateFile struct {
	Path      string
	Timestamp time.Time
}

// saveStateFile writes state to a new file in cacheDir and returns its path.
func saveStateFile(cacheDir string, state *State) (string, error) {
	if cacheDir == "" {
		cacheDir = os.TempDir()
	}

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("creating cache directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling io.cost state: %w", err)
	}

	f, err := os.CreateTemp(cacheDir, stateFilePrefix+"*"+stateFileSuffix)
	if err != nil {
		return "", fmt.Errorf("creating io.cost state file: %w", err)
	}

	_, writeErr := f.Write(data)
	closeErr := f.Close()

	if writeErr != nil || closeErr != nil {
		_ = os.Remove(f.Name())

		return "", fmt.Errorf("writing io.cost state file: %w", errors.Join(writeErr, closeErr))
	}

	return f.Name(), nil
}

func removeStateFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing io.cost state file: %w", err)
	}

	return nil
}

// ListOrphanedStateFiles finds the state files of interrupted runs.
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

	files := make([]StateFile, 0, 1)

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, stateFilePrefix) ||
			!strings.HasSuffix(name, stateFileSuffix) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		files = append(files, StateFile{Path: filepath.Join(cacheDir, name), Timestamp: info.ModTime()})
	}

	return files, nil
}

// CleanupOrphanedState restores the devices in each state file, then
// removes the file. A file that fails stays, so a later cleanup can retry.
func CleanupOrphanedState(_ context.Context, log logrus.FieldLogger, files []StateFile) error {
	for _, sf := range files {
		if err := restoreFromStateFile(sf.Path); err != nil {
			log.WithError(err).WithField("state_file", sf.Path).Warn("Failed to restore the io.cost settings")

			continue
		}

		log.WithField("state_file", sf.Path).Info("Restored the io.cost settings")
	}

	return nil
}

func restoreFromStateFile(path string) error {
	//nolint:gosec // The path comes from the benchmarkoor cache directory.
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading state file: %w", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("parsing state file: %w", err)
	}

	cgroupPath := state.CgroupPath
	if cgroupPath == "" {
		cgroupPath = DefaultCgroupPath
	}

	for _, dev := range state.Devices {
		if err := restoreSettings(cgroupPath, dev); err != nil {
			return fmt.Errorf("restoring device %s: %w", dev.MajMin, err)
		}
	}

	return removeStateFile(path)
}
