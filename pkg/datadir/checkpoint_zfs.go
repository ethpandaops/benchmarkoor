package datadir

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

// CheckpointZFSManager manages ZFS snapshots and clones for the
// checkpoint-restore rollback strategy. After the client reports RPC readiness,
// SnapshotReady takes a ZFS snapshot of the data directory. For each test,
// CloneForTest creates an instant copy-on-write clone from that snapshot.
type CheckpointZFSManager interface {
	// SnapshotReady creates a ZFS snapshot of the data directory in its
	// "RPC ready" state. Must be called exactly once per run, after the
	// client confirms readiness.
	SnapshotReady(ctx context.Context, cfg *CheckpointConfig) (*CheckpointSnapshot, error)

	// CloneForTest creates a ZFS clone of the ready-state snapshot for a
	// single test iteration. Returns a PreparedDir whose MountPath can be
	// bind-mounted into the restored container.
	CloneForTest(ctx context.Context, snapshot *CheckpointSnapshot, testID string) (*PreparedDir, error)

	// DestroySnapshot removes the ready-state snapshot. All clones derived
	// from it must be destroyed first.
	DestroySnapshot(snapshot *CheckpointSnapshot) error
}

// CheckpointConfig identifies the data directory to snapshot.
type CheckpointConfig struct {
	SourceDir  string // Host path to the data directory (must be on ZFS).
	InstanceID string // Unique identifier for this run.
}

// CheckpointSnapshot holds the identifiers of a ready-state snapshot.
type CheckpointSnapshot struct {
	SnapshotName string // Full ZFS snapshot name (e.g., "tank/data@benchmarkoor-ready-myid").
	Dataset      string // Parent dataset name.
	RelativePath string // Path from mountpoint to the actual data directory.
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

// SnapshotReady creates a snapshot of the ZFS dataset containing sourceDir.
func (m *checkpointZFSManager) SnapshotReady(
	ctx context.Context,
	cfg *CheckpointConfig,
) (*CheckpointSnapshot, error) {
	dsInfo, err := m.provider.getDatasetFromPath(ctx, cfg.SourceDir)
	if err != nil {
		return nil, fmt.Errorf("detecting ZFS dataset for %q: %w", cfg.SourceDir, err)
	}

	m.provider.log.WithFields(logrus.Fields{
		"source_dir":     cfg.SourceDir,
		"source_dataset": dsInfo.dataset,
		"instance_id":    cfg.InstanceID,
	}).Info("Creating ready-state ZFS snapshot")

	snapshotName := fmt.Sprintf("%s@benchmarkoor-ready-%s", dsInfo.dataset, cfg.InstanceID)

	if err := m.provider.createSnapshot(ctx, snapshotName); err != nil {
		return nil, err
	}

	return &CheckpointSnapshot{
		SnapshotName: snapshotName,
		Dataset:      dsInfo.dataset,
		RelativePath: dsInfo.relativePath,
	}, nil
}

// CloneForTest creates a ZFS clone from the ready-state snapshot for a single
// test. The clone is named with the testID for easy identification.
func (m *checkpointZFSManager) CloneForTest(
	ctx context.Context,
	snapshot *CheckpointSnapshot,
	testID string,
) (*PreparedDir, error) {
	cloneDataset := fmt.Sprintf("%s/benchmarkoor-cp-%s", snapshot.Dataset, testID)

	if err := m.provider.createClone(ctx, snapshot.SnapshotName, cloneDataset); err != nil {
		return nil, err
	}

	cloneMountpoint, err := m.provider.getMountpoint(ctx, cloneDataset)
	if err != nil {
		if destroyErr := m.provider.destroyClone(cloneDataset); destroyErr != nil {
			m.provider.log.WithError(destroyErr).Warn("Failed to cleanup clone after mountpoint error")
		}

		return nil, err
	}

	mountPath := cloneMountpoint
	if snapshot.RelativePath != "" {
		mountPath = filepath.Join(cloneMountpoint, snapshot.RelativePath)
	}

	return &PreparedDir{
		MountPath: mountPath,
		Cleanup: func() error {
			return m.provider.destroyClone(cloneDataset)
		},
	}, nil
}

// DestroySnapshot removes the ready-state ZFS snapshot.
func (m *checkpointZFSManager) DestroySnapshot(snapshot *CheckpointSnapshot) error {
	return m.provider.destroySnapshot(snapshot.SnapshotName)
}
