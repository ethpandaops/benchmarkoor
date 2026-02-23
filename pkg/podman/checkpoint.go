package podman

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/sirupsen/logrus"
)

// CheckpointManager extends ContainerManager with checkpoint/restore support.
type CheckpointManager interface {
	docker.ContainerManager

	// CheckpointContainer checkpoints a running container and exports
	// the checkpoint data to exportPath. The container stops after
	// checkpointing. waitAfterTCPDrop controls how long to wait after
	// dropping TCP connections for fd cleanup before the actual checkpoint.
	CheckpointContainer(
		ctx context.Context, containerID string, exportPath string,
		waitAfterTCPDrop time.Duration,
	) error

	// RestoreContainer creates and starts a new container from a checkpoint
	// export file. Returns the new container's ID.
	RestoreContainer(ctx context.Context, exportPath string, opts *RestoreOptions) (string, error)

	// ReadFileFromImage extracts a file from an OCI image by running a
	// throwaway container. Used to read config files that need patching
	// before the real container starts.
	ReadFileFromImage(ctx context.Context, imageName, filePath string) ([]byte, error)
}

// RestoreOptions configures how a container is restored from a checkpoint.
type RestoreOptions struct {
	Name        string // New container name.
	NetworkName string // Network to attach the restored container to.
}

// Ensure interface compliance.
var _ CheckpointManager = (*manager)(nil)

// CheckpointContainer checkpoints a running container and exports the state
// to the given file path. The container stops as part of the checkpoint.
func (m *manager) CheckpointContainer(
	ctx context.Context,
	containerID string,
	exportPath string,
	waitAfterTCPDrop time.Duration,
) error {
	m.log.WithField("container", containerID[:12]).Info("Checkpointing container")

	// Drop all non-LISTEN TCP connections before checkpointing. CRIU
	// cannot restore TCP sockets bound to the old container IP, and
	// restored containers get a new address. Killing connections here
	// avoids the need for --tcp-established / --tcp-close workarounds.
	if err := m.dropTCPConnections(ctx, containerID, waitAfterTCPDrop); err != nil {
		m.log.WithError(err).Warn("Failed to drop TCP connections before checkpoint")
	}

	checkpointStart := time.Now()

	fileLocks := true
	keep := true
	tcpEstablished := true
	printStats := true

	report, err := containers.Checkpoint(m.conn, containerID, &containers.CheckpointOptions{
		Export:         &exportPath,
		FileLocks:      &fileLocks,
		Keep:           &keep,
		TCPEstablished: &tcpEstablished,
		PrintStats:     &printStats,
	})
	if err != nil {
		m.logCRIUDumpLog(containerID)

		return fmt.Errorf("checkpointing container %s: %w", containerID[:12], err)
	}

	fields := logrus.Fields{
		"export":           exportPath,
		"duration":         time.Since(checkpointStart).Round(time.Millisecond),
		"runtime_duration": time.Duration(report.RuntimeDuration) * time.Microsecond,
	}

	if s := report.CRIUStatistics; s != nil {
		fields["freezing_time"] = time.Duration(s.FreezingTime) * time.Microsecond
		fields["frozen_time"] = time.Duration(s.FrozenTime) * time.Microsecond
		fields["memdump_time"] = time.Duration(s.MemdumpTime) * time.Microsecond
		fields["memwrite_time"] = time.Duration(s.MemwriteTime) * time.Microsecond
		fields["pages_scanned"] = s.PagesScanned
		fields["pages_written"] = s.PagesWritten
	}

	m.log.WithFields(fields).Info("Container checkpointed successfully")

	return nil
}

// RestoreContainer restores a container from a checkpoint export file. It
// creates a new container with the given name and mounts, then starts it from
// the checkpointed state (the process resumes mid-execution).
func (m *manager) RestoreContainer(
	ctx context.Context,
	exportPath string,
	opts *RestoreOptions,
) (string, error) {
	m.log.WithField("name", opts.Name).Info("Restoring container from checkpoint")

	restoreStart := time.Now()

	fileLocks := true
	keep := true
	tcpEstablished := true
	tcpClose := true
	ignoreVolumes := true
	printStats := true

	restoreOpts := &containers.RestoreOptions{
		ImportArchive:  &exportPath,
		Name:           &opts.Name,
		FileLocks:      &fileLocks,
		Keep:           &keep,
		TCPEstablished: &tcpEstablished,
		TCPClose:       &tcpClose,
		IgnoreVolumes:  &ignoreVolumes,
		PrintStats:     &printStats,
	}

	report, err := containers.Restore(m.conn, "", restoreOpts)
	if err != nil {
		m.logCRIURestoreLog(opts.Name)

		return "", fmt.Errorf("restoring container from %s: %w", exportPath, err)
	}

	containerID := report.Id

	fields := logrus.Fields{
		"id":               containerID[:12],
		"duration":         time.Since(restoreStart).Round(time.Millisecond),
		"runtime_duration": time.Duration(report.RuntimeDuration) * time.Microsecond,
	}

	if s := report.CRIUStatistics; s != nil {
		fields["forking_time"] = time.Duration(s.ForkingTime) * time.Microsecond
		fields["restore_time"] = time.Duration(s.RestoreTime) * time.Microsecond
		fields["pages_compared"] = s.PagesCompared
		fields["pages_skipped_cow"] = s.PagesSkippedCow
		fields["pages_restored"] = s.PagesRestored
	}

	m.log.WithFields(fields).Info("Container restored successfully")

	return containerID, nil
}

// ReadFileFromImage extracts a file from an OCI image by running a throwaway
// container with "cat". The image must already be pulled.
func (m *manager) ReadFileFromImage(
	ctx context.Context,
	imageName, filePath string,
) ([]byte, error) {
	//nolint:gosec // imageName and filePath come from trusted internal callers.
	out, err := exec.CommandContext(ctx,
		"podman", "run", "--rm", "--entrypoint", "",
		imageName, "cat", filePath,
	).Output()
	if err != nil {
		return nil, fmt.Errorf("reading %s from image %s: %w", filePath, imageName, err)
	}

	return out, nil
}

// dropTCPConnections blocks new outgoing TCP connections and kills all
// existing non-LISTEN TCP sockets inside the container's network namespace.
// This two-step approach (block then kill) avoids a race where the process
// opens new connections between the kill and the CRIU freeze.
func (m *manager) dropTCPConnections(
	ctx context.Context,
	containerID string,
	waitAfterDrop time.Duration,
) error {
	inspect, err := containers.Inspect(m.conn, containerID, nil)
	if err != nil {
		return fmt.Errorf("inspecting container: %w", err)
	}

	pid := inspect.State.Pid
	if pid <= 0 {
		return fmt.Errorf("container %s has no running PID", containerID[:12])
	}

	pidStr := strconv.Itoa(pid)

	// Step 1: Reject new outgoing TCP connections via iptables. Using
	// REJECT (not DROP) so connect() fails immediately with ECONNREFUSED,
	// allowing the process to close the fd. DROP would leave sockets stuck
	// in SYN_SENT. The rule is ephemeral — restored containers get a new
	// network namespace.
	//nolint:gosec // pid comes from Podman inspect, not user input.
	if out, err := exec.CommandContext(ctx,
		"nsenter", "-t", pidStr, "-n",
		"iptables", "-A", "OUTPUT", "-p", "tcp",
		"--tcp-flags", "SYN", "SYN",
		"-m", "state", "--state", "NEW",
		"-j", "REJECT", "--reject-with", "tcp-reset",
	).CombinedOutput(); err != nil {
		return fmt.Errorf("iptables block failed: %w: %s", err, string(out))
	}

	// Step 2: Kill all non-listening TCP sockets.
	//nolint:gosec // pid comes from Podman inspect, not user input.
	if out, err := exec.CommandContext(ctx,
		"nsenter", "-t", pidStr, "-n",
		"ss", "-K", "state", "all", "exclude", "listening",
	).CombinedOutput(); err != nil {
		return fmt.Errorf("ss -K failed: %w: %s", err, string(out))
	}

	// Step 3: Wait for the process's async runtime to notice the
	// destroyed sockets (via epoll errors) and close the fds.
	m.log.WithField("container", containerID[:12]).
		Info("Blocked outgoing TCP and dropped existing connections, waiting for fd cleanup")

	select {
	case <-time.After(waitAfterDrop):
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

// logCRIUDumpLog reads and logs the CRIU dump.log from the container's
// work-path directory. This provides detailed error information when a
// checkpoint operation fails.
func (m *manager) logCRIUDumpLog(containerID string) {
	m.logCRIULog(containerID, "dump.log")
}

// logCRIURestoreLog looks up a container by name and logs its CRIU
// restore.log. On restore failure the container ID is unknown, so we
// resolve it via inspect-by-name.
func (m *manager) logCRIURestoreLog(containerName string) {
	inspect, err := containers.Inspect(m.conn, containerName, nil)
	if err != nil {
		m.log.WithError(err).Debug("Could not inspect container for restore log")

		return
	}

	m.logCRIULog(inspect.ID, "restore.log")
}

// logCRIULog reads and logs the specified CRIU log file from a container's
// work-path directory.
func (m *manager) logCRIULog(containerID, logFile string) {
	workPath := filepath.Join(
		"/var/lib/containers/storage/overlay-containers",
		containerID,
		"userdata",
		logFile,
	)

	data, err := os.ReadFile(workPath)
	if err != nil {
		m.log.WithError(err).WithField("path", workPath).
			Debug("Could not read CRIU log")

		return
	}

	m.log.WithField("criu_log", string(data)).
		WithField("file", logFile).
		Error("CRIU log contents")
}
