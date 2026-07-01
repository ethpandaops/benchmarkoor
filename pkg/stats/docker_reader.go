package stats

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
)

const (
	// firstSampleTimeout bounds how long newDockerReader waits for the first
	// streamed stats sample before returning. The stream usually delivers a
	// sample within ~1s; this is a one-time wait per reader, not per ReadStats.
	firstSampleTimeout = 5 * time.Second

	// streamRetryDelay is the backoff between stats-stream reconnect attempts
	// (e.g. after a transient daemon error or a container restart).
	streamRetryDelay = 1 * time.Second

	// closeTimeout bounds how long Close waits for the streaming goroutine to
	// exit after cancellation.
	closeTimeout = 2 * time.Second
)

// dockerReader implements Reader using the Docker Stats API.
//
// A one-shot stats request (ContainerStats with stream=false) blocks on the
// daemon's next collection cycle — ~1-2s on Docker Desktop/OrbStack for macOS.
// Issuing one per ReadStats made benchmark runs crawl and, when read inside a
// timing window, inflated the measured RPC duration. Instead this reader opens a
// single streaming stats connection in the background and caches the most recent
// sample; ReadStats returns that cached snapshot without touching the daemon, so
// it is effectively instant. The trade-off is granularity: samples refresh at
// the daemon's cadence (~1/s), so per-RPC deltas for sub-second calls are coarse
// — acceptable on the macOS fallback path, where the cgroup reader is unavailable.
type dockerReader struct {
	log         logrus.FieldLogger
	containerID string

	cancel context.CancelFunc
	done   chan struct{}

	mu     sync.RWMutex
	latest *Stats

	readyOnce sync.Once
	ready     chan struct{} // closed once the first sample has been cached
}

// Ensure interface compliance.
var _ Reader = (*dockerReader)(nil)

// newDockerReader creates a streaming Docker Stats API reader and waits briefly
// for the first sample so the initial ReadStats has data.
func newDockerReader(
	log logrus.FieldLogger,
	dockerClient *client.Client,
	containerID string,
) (*dockerReader, error) {
	if dockerClient == nil {
		return nil, fmt.Errorf("docker client is nil")
	}

	ctx, cancel := context.WithCancel(context.Background())

	r := &dockerReader{
		log:         log.WithField("reader", "docker"),
		containerID: containerID,
		cancel:      cancel,
		done:        make(chan struct{}),
		ready:       make(chan struct{}),
	}

	go r.stream(ctx, dockerClient)

	select {
	case <-r.ready:
	case <-time.After(firstSampleTimeout):
		r.log.Warn("Timed out waiting for first Docker stats sample; metrics may be delayed")
	}

	return r, nil
}

// Type returns the reader implementation type.
func (r *dockerReader) Type() string {
	return "docker"
}

// Close cancels the streaming goroutine and waits briefly for it to exit.
func (r *dockerReader) Close() error {
	r.cancel()

	select {
	case <-r.done:
	case <-time.After(closeTimeout):
	}

	return nil
}

// ReadStats returns the most recent cached sample. It never blocks on the Docker
// daemon. Returns an error only if no sample has been collected yet.
func (r *dockerReader) ReadStats() (*Stats, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.latest == nil {
		return nil, fmt.Errorf("no docker stats sample available yet")
	}

	// Return a copy so callers cannot mutate the cached snapshot.
	snapshot := *r.latest

	return &snapshot, nil
}

// stream maintains a long-lived ContainerStats connection, decoding samples into
// the cache until the context is cancelled. It reconnects on transient errors
// and container restarts.
func (r *dockerReader) stream(ctx context.Context, dockerClient *client.Client) {
	defer close(r.done)

	for ctx.Err() == nil {
		resp, err := dockerClient.ContainerStats(ctx, r.containerID, true)
		if err != nil {
			if ctx.Err() != nil {
				return
			}

			r.log.WithError(err).Debug("Docker stats stream failed; retrying")

			if !sleepCtx(ctx, streamRetryDelay) {
				return
			}

			continue
		}

		r.consume(ctx, resp.Body)
		_ = resp.Body.Close()

		if ctx.Err() != nil {
			return
		}

		// The stream ended (e.g. container stop/restart); back off and retry.
		if !sleepCtx(ctx, streamRetryDelay) {
			return
		}
	}
}

// consume decodes stats samples from a single stream connection until it ends or
// the context is cancelled, updating the cache for each sample.
func (r *dockerReader) consume(ctx context.Context, body io.Reader) {
	dec := json.NewDecoder(body)

	for ctx.Err() == nil {
		var ds container.StatsResponse
		if err := dec.Decode(&ds); err != nil {
			return
		}

		r.update(&ds)
	}
}

// update caches a freshly decoded sample and signals readiness on the first one.
func (r *dockerReader) update(ds *container.StatsResponse) {
	snapshot := &Stats{
		// Memory usage in bytes.
		Memory: ds.MemoryStats.Usage,
		// CPU usage: Docker reports nanoseconds, convert to microseconds.
		CPUUsage: ds.CPUStats.CPUUsage.TotalUsage / 1000,
	}

	snapshot.DiskRead, snapshot.DiskWrite = extractBlkioBytes(ds)
	snapshot.DiskReadOps, snapshot.DiskWriteOps = extractBlkioOps(ds)

	r.mu.Lock()
	r.latest = snapshot
	r.mu.Unlock()

	r.readyOnce.Do(func() { close(r.ready) })
}

// sleepCtx sleeps for d or until ctx is cancelled. It returns false if the
// context was cancelled (caller should stop).
func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// extractBlkioBytes extracts read/write bytes from BlkioStats.
func extractBlkioBytes(stats *container.StatsResponse) (readBytes, writeBytes uint64) {
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
func extractBlkioOps(stats *container.StatsResponse) (readOps, writeOps uint64) {
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
