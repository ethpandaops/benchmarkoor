package podman

import (
	"context"
	"fmt"

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
	Name        string         // New container name.
	Mounts      []docker.Mount // Override mounts (e.g., for ZFS clone).
	NetworkName string         // Network to attach the restored container to.
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

	_, err := containers.Checkpoint(m.conn, containerID, &containers.CheckpointOptions{
		Export: &exportPath,
	})
	if err != nil {
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

	restoreOpts := &containers.RestoreOptions{
		ImportArchive: &exportPath,
		Name:          &opts.Name,
		IgnoreRootfs:  &ignoreRootFS,
	}

	report, err := containers.Restore(m.conn, "", restoreOpts)
	if err != nil {
		return "", fmt.Errorf("restoring container from %s: %w", exportPath, err)
	}

	containerID := report.Id

	m.log.WithField("id", containerID[:12]).Info("Container restored successfully")

	return containerID, nil
}
