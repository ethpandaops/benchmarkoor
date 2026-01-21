package stats

import (
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
)

// Stats contains a snapshot of container resource metrics.
type Stats struct {
	Memory       uint64 // Current memory usage (bytes)
	CPUUsage     uint64 // CPU usage (microseconds, cumulative)
	DiskRead     uint64 // Disk read (bytes, cumulative)
	DiskWrite    uint64 // Disk write (bytes, cumulative)
	DiskReadOps  uint64 // Disk read operations (cumulative)
	DiskWriteOps uint64 // Disk write operations (cumulative)
}

// Delta represents the difference between two Stats snapshots.
type Delta struct {
	MemoryDelta    int64  // Can be negative if memory freed
	CPUDeltaUsec   uint64 // Always positive (cumulative)
	DiskReadBytes  uint64 // Disk read bytes delta
	DiskWriteBytes uint64 // Disk write bytes delta
	DiskReadOps    uint64 // Read I/O operations delta
	DiskWriteOps   uint64 // Write I/O operations delta
}

// Reader is the interface for reading container resource stats.
// Implemented by cgroupReader (cgroup v2) and dockerReader (Docker Stats API).
type Reader interface {
	// ReadStats returns current resource metrics for the container.
	ReadStats() (*Stats, error)
	// Close releases any resources held by the reader.
	Close() error
	// Type returns the reader implementation type for logging.
	Type() string // "cgroup" or "docker"
}

// NewReader creates the best available reader for the container.
// Priority: 1) Cgroup v2 (low overhead), 2) Docker Stats API (fallback)
func NewReader(
	log logrus.FieldLogger,
	dockerClient *client.Client,
	containerID string,
) (Reader, error) {
	// Try cgroup v2 first (Linux with native Docker).
	if cgroupPath := detectCgroupPath(containerID); cgroupPath != "" {
		log.WithField("path", cgroupPath).Info("Using cgroup v2 stats reader")

		return newCgroupReader(log, cgroupPath)
	}

	// Fallback to Docker Stats API.
	log.Info("Cgroup not available, using Docker Stats API reader")

	return newDockerReader(log, dockerClient, containerID)
}

// ComputeDelta calculates the difference between after and before stats.
func ComputeDelta(before, after *Stats) *Delta {
	if before == nil || after == nil {
		return nil
	}

	delta := &Delta{
		MemoryDelta: int64(after.Memory) - int64(before.Memory),
	}

	// CPU is cumulative, so after should be >= before.
	if after.CPUUsage >= before.CPUUsage {
		delta.CPUDeltaUsec = after.CPUUsage - before.CPUUsage
	}

	// Disk metrics are cumulative.
	if after.DiskRead >= before.DiskRead {
		delta.DiskReadBytes = after.DiskRead - before.DiskRead
	}

	if after.DiskWrite >= before.DiskWrite {
		delta.DiskWriteBytes = after.DiskWrite - before.DiskWrite
	}

	if after.DiskReadOps >= before.DiskReadOps {
		delta.DiskReadOps = after.DiskReadOps - before.DiskReadOps
	}

	if after.DiskWriteOps >= before.DiskWriteOps {
		delta.DiskWriteOps = after.DiskWriteOps - before.DiskWriteOps
	}

	return delta
}
