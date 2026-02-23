package datadir

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/sirupsen/logrus"
)

// CheckpointCopyManager manages copy-based snapshots for the checkpoint-restore
// rollback strategy when no ZFS datadir is configured. After the client reports
// RPC readiness, SnapshotReady copies the data directory to a snapshot path.
// Before each test, RollbackToReady rsyncs the snapshot back over the data
// directory, discarding any writes from the previous test.
type CheckpointCopyManager struct {
	log logrus.FieldLogger
}

// CopySnapshot holds the paths for a copy-based snapshot.
type CopySnapshot struct {
	DataDir     string // Original data directory path.
	SnapshotDir string // Path to the snapshot copy.
}

// NewCheckpointCopyManager creates a new copy-based checkpoint manager.
func NewCheckpointCopyManager(log logrus.FieldLogger) *CheckpointCopyManager {
	return &CheckpointCopyManager{
		log: log.WithField("component", "datadir-checkpoint-copy"),
	}
}

// SnapshotReady creates a copy of the data directory using cp -a.
func (m *CheckpointCopyManager) SnapshotReady(
	ctx context.Context,
	cfg *CheckpointConfig,
) (*CopySnapshot, error) {
	snapshotDir := cfg.DataDir + "-snapshot"

	m.log.WithFields(logrus.Fields{
		"data_dir":     cfg.DataDir,
		"snapshot_dir": snapshotDir,
		"instance_id":  cfg.InstanceID,
	}).Info("Creating ready-state copy snapshot")

	// Remove any stale snapshot from a previous run.
	if err := os.RemoveAll(snapshotDir); err != nil {
		return nil, fmt.Errorf("removing stale snapshot %q: %w", snapshotDir, err)
	}

	//nolint:gosec // Arguments are computed paths, not user-supplied.
	cmd := exec.CommandContext(ctx, "cp", "-a", cfg.DataDir, snapshotDir)

	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf(
			"copying data directory to snapshot: %w (output: %s)",
			err, string(out),
		)
	}

	return &CopySnapshot{
		DataDir:     cfg.DataDir,
		SnapshotDir: snapshotDir,
	}, nil
}

// RollbackToReady restores the data directory from the snapshot using rsync.
func (m *CheckpointCopyManager) RollbackToReady(
	ctx context.Context,
	snapshot *CopySnapshot,
) error {
	// Trailing slashes are significant for rsync: source/ copies contents,
	// not the directory itself.
	//nolint:gosec // Arguments are computed paths, not user-supplied.
	cmd := exec.CommandContext(
		ctx, "rsync", "-a", "--delete",
		snapshot.SnapshotDir+"/", snapshot.DataDir+"/",
	)

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf(
			"rsync rollback to ready state: %w (output: %s)",
			err, string(out),
		)
	}

	return nil
}

// DestroySnapshot removes the snapshot directory.
func (m *CheckpointCopyManager) DestroySnapshot(snapshot *CopySnapshot) error {
	return os.RemoveAll(snapshot.SnapshotDir)
}
