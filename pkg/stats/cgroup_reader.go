package stats

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
)

// cgroupReader implements Reader using cgroup v2 filesystem.
type cgroupReader struct {
	log        logrus.FieldLogger
	cgroupPath string
}

// Ensure interface compliance.
var _ Reader = (*cgroupReader)(nil)

// newCgroupReader creates a new cgroup v2 stats reader.
func newCgroupReader(log logrus.FieldLogger, cgroupPath string) (*cgroupReader, error) {
	return &cgroupReader{
		log:        log.WithField("reader", "cgroup"),
		cgroupPath: cgroupPath,
	}, nil
}

// Type returns the reader implementation type.
func (r *cgroupReader) Type() string {
	return "cgroup"
}

// Close releases any resources held by the reader.
func (r *cgroupReader) Close() error {
	return nil
}

// ReadStats returns current resource metrics by reading cgroup v2 files.
func (r *cgroupReader) ReadStats() (*Stats, error) {
	stats := &Stats{}

	// Read memory.current
	memory, err := r.readSingleValue("memory.current")
	if err != nil {
		r.log.WithError(err).Debug("Failed to read memory.current")
	} else {
		stats.Memory = memory
	}

	// Read cpu.stat for usage_usec
	cpuUsage, err := r.readCPUUsage()
	if err != nil {
		r.log.WithError(err).Debug("Failed to read cpu.stat")
	} else {
		stats.CPUUsage = cpuUsage
	}

	// Read io.stat for disk I/O
	diskRead, diskWrite, readOps, writeOps, err := r.readIOStats()
	if err != nil {
		r.log.WithError(err).Debug("Failed to read io.stat")
	} else {
		stats.DiskRead = diskRead
		stats.DiskWrite = diskWrite
		stats.DiskReadOps = readOps
		stats.DiskWriteOps = writeOps
	}

	return stats, nil
}

// readSingleValue reads a single uint64 value from a cgroup file.
func (r *cgroupReader) readSingleValue(filename string) (uint64, error) {
	path := filepath.Join(r.cgroupPath, filename)

	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", filename, err)
	}

	value, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing %s: %w", filename, err)
	}

	return value, nil
}

// readCPUUsage reads usage_usec from cpu.stat.
// Format: usage_usec 12345
func (r *cgroupReader) readCPUUsage() (uint64, error) {
	path := filepath.Join(r.cgroupPath, "cpu.stat")

	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("opening cpu.stat: %w", err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "usage_usec ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return strconv.ParseUint(parts[1], 10, 64)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("scanning cpu.stat: %w", err)
	}

	return 0, fmt.Errorf("usage_usec not found in cpu.stat")
}

// readIOStats reads io.stat and sums rbytes/wbytes/rios/wios across all devices.
// Format: 8:0 rbytes=1234 wbytes=5678 rios=10 wios=20 ...
func (r *cgroupReader) readIOStats() (readBytes, writeBytes, readOps, writeOps uint64, err error) {
	path := filepath.Join(r.cgroupPath, "io.stat")

	file, err := os.Open(path)
	if err != nil {
		// io.stat may not exist if no I/O has occurred.
		if os.IsNotExist(err) {
			return 0, 0, 0, 0, nil
		}

		return 0, 0, 0, 0, fmt.Errorf("opening io.stat: %w", err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		// Parse each key=value pair on the line.
		for _, part := range strings.Fields(line) {
			if strings.HasPrefix(part, "rbytes=") {
				if v, parseErr := strconv.ParseUint(part[7:], 10, 64); parseErr == nil {
					readBytes += v
				}
			} else if strings.HasPrefix(part, "wbytes=") {
				if v, parseErr := strconv.ParseUint(part[7:], 10, 64); parseErr == nil {
					writeBytes += v
				}
			} else if strings.HasPrefix(part, "rios=") {
				if v, parseErr := strconv.ParseUint(part[5:], 10, 64); parseErr == nil {
					readOps += v
				}
			} else if strings.HasPrefix(part, "wios=") {
				if v, parseErr := strconv.ParseUint(part[5:], 10, 64); parseErr == nil {
					writeOps += v
				}
			}
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return 0, 0, 0, 0, fmt.Errorf("scanning io.stat: %w", scanErr)
	}

	return readBytes, writeBytes, readOps, writeOps, nil
}

// detectCgroupPath finds the cgroup v2 path for a container.
// Checks Docker and Podman paths for both systemd and cgroupfs drivers.
func detectCgroupPath(containerID string) string {
	// Common cgroup v2 base path.
	cgroupBase := "/sys/fs/cgroup"

	// Candidate paths in priority order.
	candidates := []string{
		// Docker systemd: /sys/fs/cgroup/system.slice/docker-{id}.scope
		filepath.Join(cgroupBase, "system.slice", "docker-"+containerID+".scope"),
		// Docker cgroupfs: /sys/fs/cgroup/docker/{id}
		filepath.Join(cgroupBase, "docker", containerID),
		// Podman systemd (rootful): /sys/fs/cgroup/machine.slice/libpod-{id}.scope/container
		filepath.Join(cgroupBase, "machine.slice", "libpod-"+containerID+".scope", "container"),
		// Podman systemd (rootful, no sub-cgroup): /sys/fs/cgroup/machine.slice/libpod-{id}.scope
		filepath.Join(cgroupBase, "machine.slice", "libpod-"+containerID+".scope"),
		// Podman cgroupfs: /sys/fs/cgroup/libpod_parent/libpod-{id}
		filepath.Join(cgroupBase, "libpod_parent", "libpod-"+containerID),
	}

	for _, path := range candidates {
		if isValidCgroupPath(path) {
			return path
		}
	}

	// Try short container ID (first 12 characters) for systemd paths.
	if len(containerID) >= 12 {
		shortID := containerID[:12]

		shortCandidates := []string{
			filepath.Join(cgroupBase, "system.slice", "docker-"+shortID+".scope"),
			filepath.Join(cgroupBase, "machine.slice", "libpod-"+shortID+".scope", "container"),
			filepath.Join(cgroupBase, "machine.slice", "libpod-"+shortID+".scope"),
		}

		for _, path := range shortCandidates {
			if isValidCgroupPath(path) {
				return path
			}
		}
	}

	return ""
}

// isValidCgroupPath checks if a path is a valid cgroup v2 directory
// by verifying that essential cgroup files exist.
func isValidCgroupPath(path string) bool {
	// Check if directory exists.
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}

	// Check for at least one of the essential cgroup v2 files.
	essentialFiles := []string{"memory.current", "cpu.stat", "cgroup.controllers"}
	for _, file := range essentialFiles {
		if _, err := os.Stat(filepath.Join(path, file)); err == nil {
			return true
		}
	}

	return false
}
