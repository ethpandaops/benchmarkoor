package datadir

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
)

// CheckpointZFSManager manages ZFS snapshots for the checkpoint-restore
// rollback strategy. After the client reports RPC readiness, SnapshotReady
// takes a ZFS snapshot of the container's data directory (the ZFS clone).
// Before each test, RollbackToReady resets the clone to the snapshot state.
// CRIU restores the container with the same mount path, so no per-test clones
// or mount overrides are needed.
type CheckpointZFSManager interface {
	// SnapshotReady creates a ZFS snapshot of the data directory in its
	// "RPC ready" state. The dataDir must be on a ZFS dataset (typically
	// a clone created earlier in the run). Must be called once per run.
	SnapshotReady(ctx context.Context, cfg *CheckpointConfig) (*CheckpointSnapshot, error)

	// RollbackToReady resets the dataset to the ready-state snapshot,
	// discarding any writes from the previous test. This is instant
	// (copy-on-write) and keeps the mount path unchanged.
	RollbackToReady(ctx context.Context, snapshot *CheckpointSnapshot) error

	// DestroySnapshot removes the ready-state snapshot.
	DestroySnapshot(snapshot *CheckpointSnapshot) error
}

// CheckpointConfig identifies the data directory to snapshot.
type CheckpointConfig struct {
	// DataDir is the host path to the data directory that the container
	// mounts. For checkpoint-restore this is the ZFS clone's mount path
	// (e.g., /pool/data/benchmarkoor-clone-geth/mainnet/24350000/geth).
	DataDir    string
	InstanceID string // Unique identifier for this run.
}

// CheckpointSnapshot holds the identifiers of a ready-state snapshot.
type CheckpointSnapshot struct {
	SnapshotName string // Full ZFS snapshot name (e.g., "pool/clone@benchmarkoor-ready-id").
	Dataset      string // Parent dataset name (the clone dataset).
}

// NewCheckpointZFSManager creates a new checkpoint ZFS manager.
func NewCheckpointZFSManager(log logrus.FieldLogger) CheckpointZFSManager {
	return &checkpointZFSManager{
		provider: &zfsProvider{
			log: log.WithField("component", "datadir-checkpoint-zfs"),
		},
	}
}

type checkpointZFSManager struct {
	provider *zfsProvider
}

// Ensure interface compliance.
var _ CheckpointZFSManager = (*checkpointZFSManager)(nil)

// SnapshotReady creates a snapshot of the ZFS dataset at dataDir.
func (m *checkpointZFSManager) SnapshotReady(
	ctx context.Context,
	cfg *CheckpointConfig,
) (*CheckpointSnapshot, error) {
	dsInfo, err := m.provider.getDatasetFromPath(ctx, cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("detecting ZFS dataset for %q: %w", cfg.DataDir, err)
	}

	m.provider.log.WithFields(logrus.Fields{
		"data_dir":    cfg.DataDir,
		"dataset":     dsInfo.dataset,
		"instance_id": cfg.InstanceID,
	}).Info("Creating ready-state ZFS snapshot")

	snapshotName := fmt.Sprintf(
		"%s@benchmarkoor-ready-%s", dsInfo.dataset, cfg.InstanceID,
	)

	if err := m.provider.createSnapshot(ctx, snapshotName); err != nil {
		return nil, err
	}

	return &CheckpointSnapshot{
		SnapshotName: snapshotName,
		Dataset:      dsInfo.dataset,
	}, nil
}

// RollbackToReady rolls the dataset back to the ready-state snapshot.
func (m *checkpointZFSManager) RollbackToReady(
	ctx context.Context,
	snapshot *CheckpointSnapshot,
) error {
	return m.provider.rollbackSnapshot(ctx, snapshot.SnapshotName)
}

// DestroySnapshot removes the ready-state ZFS snapshot.
func (m *checkpointZFSManager) DestroySnapshot(
	snapshot *CheckpointSnapshot,
) error {
	return m.provider.destroySnapshot(snapshot.SnapshotName)
}
