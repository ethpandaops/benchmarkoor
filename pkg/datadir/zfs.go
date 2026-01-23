package datadir

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

// ZFSProvider implements Provider using ZFS snapshots and clones.
// It creates an instant, copy-on-write clone of the source dataset,
// providing near-instant data directory preparation without copying.
type ZFSProvider interface {
	Provider
}

// NewZFSProvider creates a new ZFS provider.
func NewZFSProvider(log logrus.FieldLogger) ZFSProvider {
	return &zfsProvider{
		log: log.WithField("component", "datadir-zfs"),
	}
}

type zfsProvider struct {
	log logrus.FieldLogger
}

// Ensure interface compliance.
var _ ZFSProvider = (*zfsProvider)(nil)

// Prepare creates a ZFS clone from a snapshot of the source dataset.
func (p *zfsProvider) Prepare(ctx context.Context, cfg *ProviderConfig) (*PreparedDir, error) {
	// Auto-detect the ZFS dataset from the source directory path.
	dsInfo, err := p.getDatasetFromPath(ctx, cfg.SourceDir)
	if err != nil {
		return nil, fmt.Errorf("detecting ZFS dataset for %q: %w", cfg.SourceDir, err)
	}

	p.log.WithFields(logrus.Fields{
		"source_dir":     cfg.SourceDir,
		"source_dataset": dsInfo.dataset,
		"mountpoint":     dsInfo.mountpoint,
		"relative_path":  dsInfo.relativePath,
		"instance_id":    cfg.InstanceID,
	}).Info("Detected ZFS dataset for source directory")

	// Log ZFS properties for the source dataset.
	p.logDatasetProperties(ctx, dsInfo.dataset)

	// Generate unique names for snapshot and clone.
	snapshotName := fmt.Sprintf("%s@benchmarkoor-%s", dsInfo.dataset, cfg.InstanceID)
	cloneDataset := fmt.Sprintf("%s/benchmarkoor-clone-%s", dsInfo.dataset, cfg.InstanceID)

	// Create snapshot of the source dataset.
	if err := p.createSnapshot(ctx, snapshotName); err != nil {
		return nil, err
	}

	// Create clone from the snapshot.
	if err := p.createClone(ctx, snapshotName, cloneDataset); err != nil {
		// Clean up snapshot on failure.
		if destroyErr := p.destroySnapshot(snapshotName); destroyErr != nil {
			p.log.WithError(destroyErr).Warn("Failed to cleanup snapshot after clone failure")
		}

		return nil, err
	}

	// Get the mount point of the clone.
	cloneMountpoint, err := p.getMountpoint(ctx, cloneDataset)
	if err != nil {
		// Clean up on failure.
		if destroyErr := p.destroyClone(cloneDataset); destroyErr != nil {
			p.log.WithError(destroyErr).Warn("Failed to cleanup clone after mountpoint error")
		}

		if destroyErr := p.destroySnapshot(snapshotName); destroyErr != nil {
			p.log.WithError(destroyErr).Warn("Failed to cleanup snapshot after mountpoint error")
		}

		return nil, err
	}

	// Calculate the final mount path by appending the relative path to the clone's mountpoint.
	// This ensures that if source_dir was a subdirectory within the dataset,
	// we return the corresponding subdirectory within the clone.
	mountPath := cloneMountpoint
	if dsInfo.relativePath != "" {
		mountPath = filepath.Join(cloneMountpoint, dsInfo.relativePath)
	}

	p.log.WithFields(logrus.Fields{
		"snapshot":         snapshotName,
		"clone_dataset":    cloneDataset,
		"clone_mountpoint": cloneMountpoint,
		"mount_path":       mountPath,
	}).Info("ZFS clone created successfully")

	// Return prepared directory with cleanup function.
	return &PreparedDir{
		MountPath: mountPath,
		Cleanup: func() error {
			return p.cleanup(cloneDataset, snapshotName)
		},
	}, nil
}

// datasetInfo contains information about a ZFS dataset and the path within it.
type datasetInfo struct {
	dataset      string // ZFS dataset name (e.g., "tank/data")
	mountpoint   string // Dataset mountpoint (e.g., "/tank/data")
	relativePath string // Path from mountpoint to source_dir (e.g., "subdir/files")
}

// getDatasetFromPath finds the ZFS dataset containing the given path.
// Returns the dataset name, its mountpoint, and the relative path from mountpoint to the source.
func (p *zfsProvider) getDatasetFromPath(ctx context.Context, path string) (*datasetInfo, error) {
	// Get absolute path for comparison.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("getting absolute path: %w", err)
	}

	// List all ZFS datasets with their mountpoints.
	//nolint:gosec // Command args are controlled by the application.
	cmd := exec.CommandContext(ctx, "zfs", "list", "-H", "-o", "name,mountpoint")

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("listing ZFS datasets: %w (stderr: %s)", err, string(exitErr.Stderr))
		}

		return nil, fmt.Errorf("listing ZFS datasets: %w", err)
	}

	// Find the dataset with the longest matching mountpoint.
	var bestDataset string

	var bestMountpoint string

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)

		if len(fields) < 2 {
			continue
		}

		dataset := fields[0]
		mountpoint := fields[1]

		// Skip datasets with no mountpoint or legacy/none mountpoint.
		if mountpoint == "-" || mountpoint == "none" || mountpoint == "legacy" {
			continue
		}

		// Check if the path is under this mountpoint.
		// Need to ensure we match on path boundaries (e.g., /data matches /data/foo but not /data-backup).
		if pathIsUnderMountpoint(absPath, mountpoint) {
			// Use the dataset with the longest matching mountpoint (most specific).
			if len(mountpoint) > len(bestMountpoint) {
				bestDataset = dataset
				bestMountpoint = mountpoint
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parsing ZFS list output: %w", err)
	}

	if bestDataset == "" {
		return nil, fmt.Errorf("path %q is not on a ZFS filesystem", absPath)
	}

	// Calculate relative path from mountpoint to source_dir.
	relativePath, err := filepath.Rel(bestMountpoint, absPath)
	if err != nil {
		return nil, fmt.Errorf("calculating relative path: %w", err)
	}

	// Clean up relative path (handle "." for same directory).
	if relativePath == "." {
		relativePath = ""
	}

	return &datasetInfo{
		dataset:      bestDataset,
		mountpoint:   bestMountpoint,
		relativePath: relativePath,
	}, nil
}

// pathIsUnderMountpoint checks if a path is under a mountpoint.
// Ensures proper path boundary matching (e.g., /data matches /data/foo but not /data-backup).
func pathIsUnderMountpoint(path, mountpoint string) bool {
	// Exact match.
	if path == mountpoint {
		return true
	}

	// Ensure mountpoint ends with separator for prefix check.
	mountpointWithSep := mountpoint
	if !strings.HasSuffix(mountpointWithSep, string(filepath.Separator)) {
		mountpointWithSep += string(filepath.Separator)
	}

	return strings.HasPrefix(path, mountpointWithSep)
}

// logDatasetProperties logs all ZFS properties for a dataset.
func (p *zfsProvider) logDatasetProperties(ctx context.Context, dataset string) {
	//nolint:gosec // Command args are controlled by the application.
	cmd := exec.CommandContext(ctx, "zfs", "get", "all", "-H", dataset)

	output, err := cmd.Output()
	if err != nil {
		p.log.WithError(err).WithField("dataset", dataset).Warn("Failed to get ZFS properties")

		return
	}

	p.log.WithField("dataset", dataset).Info("ZFS dataset properties:")

	// Parse and log each property on its own line.
	// Output format: NAME\tPROPERTY\tVALUE\tSOURCE
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) >= 4 {
			p.log.WithFields(logrus.Fields{
				"property": fields[1],
				"value":    fields[2],
				"source":   fields[3],
			}).Info("  zfs property")
		}
	}
}

// createSnapshot creates a ZFS snapshot.
func (p *zfsProvider) createSnapshot(ctx context.Context, snapshotName string) error {
	p.log.WithField("snapshot", snapshotName).Info("Creating ZFS snapshot")

	//nolint:gosec // Command args are controlled by the application.
	cmd := exec.CommandContext(ctx, "zfs", "snapshot", snapshotName)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("creating ZFS snapshot %q: %w (output: %s)", snapshotName, err, string(output))
	}

	return nil
}

// createClone creates a ZFS clone from a snapshot.
func (p *zfsProvider) createClone(ctx context.Context, snapshotName, cloneDataset string) error {
	p.log.WithFields(logrus.Fields{
		"snapshot":      snapshotName,
		"clone_dataset": cloneDataset,
	}).Info("Creating ZFS clone")

	//nolint:gosec // Command args are controlled by the application.
	cmd := exec.CommandContext(ctx, "zfs", "clone", snapshotName, cloneDataset)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("creating ZFS clone %q from %q: %w (output: %s)",
			cloneDataset, snapshotName, err, string(output))
	}

	return nil
}

// getMountpoint retrieves the mountpoint for a ZFS dataset.
func (p *zfsProvider) getMountpoint(ctx context.Context, dataset string) (string, error) {
	//nolint:gosec // Command args are controlled by the application.
	cmd := exec.CommandContext(ctx, "zfs", "get", "-H", "-o", "value", "mountpoint", dataset)

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("getting mountpoint for %q: %w (stderr: %s)", dataset, err, string(exitErr.Stderr))
		}

		return "", fmt.Errorf("getting mountpoint for %q: %w", dataset, err)
	}

	mountpoint := strings.TrimSpace(string(output))
	if mountpoint == "" || mountpoint == "-" || mountpoint == "none" || mountpoint == "legacy" {
		return "", fmt.Errorf("dataset %q has no valid mountpoint: %q", dataset, mountpoint)
	}

	return mountpoint, nil
}

// destroyClone destroys a ZFS clone dataset.
func (p *zfsProvider) destroyClone(cloneDataset string) error {
	p.log.WithField("clone_dataset", cloneDataset).Info("Destroying ZFS clone")

	//nolint:gosec // Command args are controlled by the application.
	cmd := exec.Command("zfs", "destroy", cloneDataset)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("destroying ZFS clone %q: %w (output: %s)", cloneDataset, err, string(output))
	}

	return nil
}

// destroySnapshot destroys a ZFS snapshot.
func (p *zfsProvider) destroySnapshot(snapshotName string) error {
	p.log.WithField("snapshot", snapshotName).Info("Destroying ZFS snapshot")

	//nolint:gosec // Command args are controlled by the application.
	cmd := exec.Command("zfs", "destroy", snapshotName)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("destroying ZFS snapshot %q: %w (output: %s)", snapshotName, err, string(output))
	}

	return nil
}

// cleanup destroys the ZFS clone and its source snapshot.
func (p *zfsProvider) cleanup(cloneDataset, snapshotName string) error {
	p.log.WithFields(logrus.Fields{
		"clone_dataset": cloneDataset,
		"snapshot":      snapshotName,
	}).Info("Cleaning up ZFS clone and snapshot")

	// Destroy clone first (required before snapshot can be destroyed).
	if err := p.destroyClone(cloneDataset); err != nil {
		p.log.WithError(err).Warn("Failed to destroy ZFS clone")

		return err
	}

	// Destroy the snapshot.
	if err := p.destroySnapshot(snapshotName); err != nil {
		p.log.WithError(err).Warn("Failed to destroy ZFS snapshot")

		return err
	}

	p.log.Info("ZFS cleanup completed")

	return nil
}

// ZFSOrphanedResource represents an orphaned ZFS resource created by benchmarkoor.
type ZFSOrphanedResource struct {
	Name string // Full resource name (dataset or snapshot)
	Type string // "clone" or "snapshot"
}

// ListOrphanedZFSResources finds ZFS clones and snapshots created by benchmarkoor.
// These may be left behind if the process was killed before cleanup.
func ListOrphanedZFSResources(ctx context.Context) ([]ZFSOrphanedResource, error) {
	// List all ZFS filesystems and snapshots.
	//nolint:gosec // Command args are controlled by the application.
	cmd := exec.CommandContext(ctx, "zfs", "list", "-t", "all", "-H", "-o", "name,type")

	output, err := cmd.Output()
	if err != nil {
		// If zfs command doesn't exist or fails, return empty list (ZFS not available).
		return nil, nil //nolint:nilerr // ZFS not available is not an error.
	}

	var resources []ZFSOrphanedResource

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}

		name := fields[0]
		resourceType := fields[1]

		// Check for benchmarkoor clones (filesystem type with benchmarkoor-clone in name).
		if resourceType == "filesystem" && strings.Contains(name, "/benchmarkoor-clone-") {
			resources = append(resources, ZFSOrphanedResource{
				Name: name,
				Type: "clone",
			})
		}

		// Check for benchmarkoor snapshots.
		if resourceType == "snapshot" && strings.Contains(name, "@benchmarkoor-") {
			resources = append(resources, ZFSOrphanedResource{
				Name: name,
				Type: "snapshot",
			})
		}
	}

	return resources, scanner.Err()
}

// CleanupOrphanedZFSResources removes orphaned ZFS clones and snapshots.
// Clones must be destroyed before their parent snapshots.
func CleanupOrphanedZFSResources(ctx context.Context, log logrus.FieldLogger, resources []ZFSOrphanedResource) error {
	// Separate clones and snapshots - clones must be destroyed first.
	var clones, snapshots []ZFSOrphanedResource

	for _, r := range resources {
		if r.Type == "clone" {
			clones = append(clones, r)
		} else {
			snapshots = append(snapshots, r)
		}
	}

	// Destroy clones first.
	for _, clone := range clones {
		log.WithField("clone", clone.Name).Info("Destroying orphaned ZFS clone")

		//nolint:gosec // Command args are controlled by the application.
		cmd := exec.CommandContext(ctx, "zfs", "destroy", clone.Name)

		if output, err := cmd.CombinedOutput(); err != nil {
			log.WithError(err).WithFields(logrus.Fields{
				"clone":  clone.Name,
				"output": string(output),
			}).Warn("Failed to destroy orphaned ZFS clone")
		}
	}

	// Then destroy snapshots.
	for _, snapshot := range snapshots {
		log.WithField("snapshot", snapshot.Name).Info("Destroying orphaned ZFS snapshot")

		//nolint:gosec // Command args are controlled by the application.
		cmd := exec.CommandContext(ctx, "zfs", "destroy", snapshot.Name)

		if output, err := cmd.CombinedOutput(); err != nil {
			log.WithError(err).WithFields(logrus.Fields{
				"snapshot": snapshot.Name,
				"output":   string(output),
			}).Warn("Failed to destroy orphaned ZFS snapshot")
		}
	}

	return nil
}
