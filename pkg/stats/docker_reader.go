package stats

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
)

// dockerReader implements Reader using Docker Stats API.
type dockerReader struct {
	log         logrus.FieldLogger
	client      *client.Client
	containerID string
}

// Ensure interface compliance.
var _ Reader = (*dockerReader)(nil)

// newDockerReader creates a new Docker Stats API reader.
func newDockerReader(
	log logrus.FieldLogger,
	dockerClient *client.Client,
	containerID string,
) (*dockerReader, error) {
	if dockerClient == nil {
		return nil, fmt.Errorf("docker client is nil")
	}

	return &dockerReader{
		log:         log.WithField("reader", "docker"),
		client:      dockerClient,
		containerID: containerID,
	}, nil
}

// Type returns the reader implementation type.
func (r *dockerReader) Type() string {
	return "docker"
}

// Close releases any resources held by the reader.
func (r *dockerReader) Close() error {
	return nil
}

// ReadStats returns current resource metrics using Docker Stats API.
func (r *dockerReader) ReadStats() (*Stats, error) {
	// Use one-shot stats (stream=false) for lower overhead.
	ctx := context.Background()

	statsResp, err := r.client.ContainerStats(ctx, r.containerID, false)
	if err != nil {
		return nil, fmt.Errorf("getting container stats: %w", err)
	}
	defer func() { _ = statsResp.Body.Close() }()

	var dockerStats container.StatsResponse
	if err := json.NewDecoder(statsResp.Body).Decode(&dockerStats); err != nil {
		return nil, fmt.Errorf("decoding stats response: %w", err)
	}

	stats := &Stats{
		// Memory usage in bytes.
		Memory: dockerStats.MemoryStats.Usage,
		// CPU usage: Docker reports in nanoseconds, convert to microseconds.
		CPUUsage: dockerStats.CPUStats.CPUUsage.TotalUsage / 1000,
	}

	// Sum disk I/O from BlkioStats.
	stats.DiskRead, stats.DiskWrite = r.extractBlkioBytes(&dockerStats)
	stats.DiskReadOps, stats.DiskWriteOps = r.extractBlkioOps(&dockerStats)

	return stats, nil
}

// extractBlkioBytes extracts read/write bytes from BlkioStats.
func (r *dockerReader) extractBlkioBytes(stats *container.StatsResponse) (readBytes, writeBytes uint64) {
	for _, entry := range stats.BlkioStats.IoServiceBytesRecursive {
		switch entry.Op {
		case "Read", "read":
			readBytes += entry.Value
		case "Write", "write":
			writeBytes += entry.Value
		}
	}

	return readBytes, writeBytes
}

// extractBlkioOps extracts read/write I/O operations from BlkioStats.
func (r *dockerReader) extractBlkioOps(stats *container.StatsResponse) (readOps, writeOps uint64) {
	for _, entry := range stats.BlkioStats.IoServicedRecursive {
		switch entry.Op {
		case "Read", "read":
			readOps += entry.Value
		case "Write", "write":
			writeOps += entry.Value
		}
	}

	return readOps, writeOps
}
