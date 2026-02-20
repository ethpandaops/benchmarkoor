package podman

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
)

// CheckpointManager extends ContainerManager with checkpoint/restore support.
type CheckpointManager interface {
	docker.ContainerManager

	// CheckpointContainer checkpoints a running container and exports
	// the checkpoint data to exportPath. The container stops after
	// checkpointing.
	CheckpointContainer(ctx context.Context, containerID string, exportPath string) error

	// RestoreContainer creates and starts a new container from a checkpoint
	// export file. Returns the new container's ID.
	RestoreContainer(ctx context.Context, exportPath string, opts *RestoreOptions) (string, error)
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
) error {
	m.log.WithField("container", containerID[:12]).Info("Checkpointing container")

	fileLocks := true
	keep := true

	_, err := containers.Checkpoint(m.conn, containerID, &containers.CheckpointOptions{
		Export:    &exportPath,
		FileLocks: &fileLocks,
		Keep:      &keep,
	})
	if err != nil {
		m.logCRIUDumpLog(containerID)

		return fmt.Errorf("checkpointing container %s: %w", containerID[:12], err)
	}

	m.log.WithField("export", exportPath).Info("Container checkpointed successfully")

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

	ignoreRootFS := true
	fileLocks := true
	keep := true

	restoreOpts := &containers.RestoreOptions{
		ImportArchive: &exportPath,
		Name:          &opts.Name,
		IgnoreRootfs:  &ignoreRootFS,
		FileLocks:     &fileLocks,
		Keep:          &keep,
	}

	report, err := containers.Restore(m.conn, "", restoreOpts)
	if err != nil {
		m.logCRIURestoreLog(opts.Name)

		return "", fmt.Errorf("restoring container from %s: %w", exportPath, err)
	}

	containerID := report.Id

	m.log.WithField("id", containerID[:12]).Info("Container restored successfully")

	return containerID, nil
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
