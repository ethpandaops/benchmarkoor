package podman

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/containers/podman/v5/pkg/specgen"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/sirupsen/logrus"
)

// CheckpointManager extends ContainerManager with checkpoint/restore support.
type CheckpointManager interface {
	docker.ContainerManager

	// ValidateCheckpointSupport checks that CRIU is installed and
	// operational. Call this early to fail fast before starting containers.
	ValidateCheckpointSupport(ctx context.Context) error

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

// ValidateCheckpointSupport verifies that CRIU is installed and able to
// perform checkpoint/restore operations on this system.
func (m *manager) ValidateCheckpointSupport(ctx context.Context) error {
	criuPath, err := exec.LookPath("criu")
	if err != nil {
		return fmt.Errorf(
			"criu binary not found in PATH: %w\n"+
				"Install CRIU: https://criu.org/Installation",
			err,
		)
	}

	m.log.WithField("path", criuPath).Debug("Found CRIU binary")

	//nolint:gosec // criuPath comes from LookPath, not user input.
	out, err := exec.CommandContext(ctx, criuPath, "check").CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"criu check failed — checkpoint/restore may not work: %w\nOutput: %s",
			err, string(out),
		)
	}

	m.log.Debug("CRIU checkpoint support validated")

	return nil
}

// CheckpointContainer checkpoints a running container and exports the state
// to the given file path. The container stops as part of the checkpoint.
func (m *manager) CheckpointContainer(
	ctx context.Context,
	containerID string,
	exportPath string,
	waitAfterTCPDrop time.Duration,
) error {
	m.log.WithField("container", containerID[:12]).Info("Checkpointing container")

	// Drop all non-LISTEN TCP connections and all UDP sockets before
	// checkpointing. CRIU cannot restore sockets bound to the old
	// container IP, and restored containers get a new address. Killing
	// connections here avoids restore failures from stale socket state.
	if err := m.dropConnections(ctx, containerID, waitAfterTCPDrop); err != nil {
		m.log.WithError(err).Warn("Failed to drop connections before checkpoint")
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
		"export":           exportPath,
		"duration":         time.Since(restoreStart).Round(time.Millisecond),
		"runtime_duration": time.Duration(report.RuntimeDuration) * time.Microsecond,
	}

	if fi, err := os.Stat(exportPath); err == nil {
		fields["checkpoint_size"] = fi.Size()
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

// ReadFileFromImage extracts a file from an OCI image using the Podman API.
// It creates a throwaway container (without starting it) and copies the file
// out via the container archive endpoint. The image must already be pulled.
func (m *manager) ReadFileFromImage(
	ctx context.Context,
	imageName, filePath string,
) ([]byte, error) {
	imageName = qualifyImageName(imageName)

	// Create a throwaway container — no need to start it, the archive
	// API can read from the container's filesystem while it's stopped.
	s := &specgen.SpecGenerator{}
	s.Name = fmt.Sprintf("benchmarkoor-readfile-%d", time.Now().UnixNano())
	s.Image = imageName
	s.Command = []string{"true"}
	s.HealthLogDestination = "local"

	resp, err := containers.CreateWithSpec(m.conn, s, nil)
	if err != nil {
		return nil, fmt.Errorf(
			"creating throwaway container for %s: %w", imageName, err,
		)
	}

	defer func() {
		force := true
		timeout := uint(0)

		if _, rmErr := containers.Remove(
			m.conn, resp.ID, &containers.RemoveOptions{
				Force:   &force,
				Timeout: &timeout,
			},
		); rmErr != nil {
			m.log.WithError(rmErr).Debug(
				"Failed to remove throwaway container",
			)
		}
	}()

	// Copy the file out as a tar archive.
	var buf bytes.Buffer

	copyFn, err := containers.CopyToArchive(
		m.conn, resp.ID, filePath, &buf,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"copying %s from container: %w", filePath, err,
		)
	}

	if err := copyFn(); err != nil {
		return nil, fmt.Errorf(
			"reading %s from image %s: %w", filePath, imageName, err,
		)
	}

	// Extract the single file from the tar stream.
	tr := tar.NewReader(&buf)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf(
				"file %s not found in archive from %s", filePath, imageName,
			)
		}

		if err != nil {
			return nil, fmt.Errorf("reading tar entry: %w", err)
		}

		if hdr.Typeflag == tar.TypeReg {
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf(
					"reading %s content: %w", filePath, err,
				)
			}

			return data, nil
		}
	}
}

// dropConnections blocks new outgoing TCP and UDP traffic, then kills all
// non-LISTEN TCP sockets and all UDP sockets inside the container's network
// namespace. This block-then-kill approach avoids a race where the process
// opens new connections between the kill and the CRIU freeze. Both TCP and
// UDP sockets must be destroyed because CRIU cannot restore sockets bound
// to the old container IP (restored containers get a new address).
func (m *manager) dropConnections(
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
		return fmt.Errorf("iptables TCP block failed: %w: %s", err, string(out))
	}

	// Step 2: Drop all outgoing UDP traffic. This prevents the process
	// from sending new datagrams while we destroy its UDP sockets.
	//nolint:gosec // pid comes from Podman inspect, not user input.
	if out, err := exec.CommandContext(ctx,
		"nsenter", "-t", pidStr, "-n",
		"iptables", "-A", "OUTPUT", "-p", "udp",
		"-j", "DROP",
	).CombinedOutput(); err != nil {
		return fmt.Errorf("iptables UDP block failed: %w: %s", err, string(out))
	}

	// Step 3: Kill all non-listening TCP sockets.
	//nolint:gosec // pid comes from Podman inspect, not user input.
	if out, err := exec.CommandContext(ctx,
		"nsenter", "-t", pidStr, "-n",
		"ss", "-K", "state", "all", "exclude", "listening",
	).CombinedOutput(); err != nil {
		return fmt.Errorf("ss -K TCP failed: %w: %s", err, string(out))
	}

	// Step 4: Kill all UDP sockets. UDP sockets bound to the container's
	// IP cannot be rebound after restore when the container gets a new
	// address. Unlike TCP, UDP has no "listening" state so we destroy all.
	//nolint:gosec // pid comes from Podman inspect, not user input.
	if out, err := exec.CommandContext(ctx,
		"nsenter", "-t", pidStr, "-n",
		"ss", "-K", "-u", "state", "all",
	).CombinedOutput(); err != nil {
		return fmt.Errorf("ss -K UDP failed: %w: %s", err, string(out))
	}

	// Step 5: Wait for the process's async runtime to notice the
	// destroyed sockets (via epoll errors) and close the fds.
	m.log.WithField("container", containerID[:12]).
		Info("Blocked outgoing traffic and dropped existing connections, waiting for fd cleanup")

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
