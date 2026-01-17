package datadir

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

// OverlayFSProvider implements Provider using Linux overlayfs.
// It creates a union filesystem with the source as a read-only lower layer
// and a writable upper layer, avoiding the need to copy data.
type OverlayFSProvider interface {
	Provider
}

// NewOverlayFSProvider creates a new OverlayFS provider.
func NewOverlayFSProvider(log logrus.FieldLogger) OverlayFSProvider {
	return &overlayFSProvider{
		log: log.WithField("component", "datadir-overlayfs"),
	}
}

type overlayFSProvider struct {
	log logrus.FieldLogger
}

// Ensure interface compliance.
var _ OverlayFSProvider = (*overlayFSProvider)(nil)

// Prepare creates an overlayfs mount with the source as the lower layer.
func (p *overlayFSProvider) Prepare(ctx context.Context, cfg *ProviderConfig) (*PreparedDir, error) {
	// Create temp directory for overlay structure.
	baseDir, err := os.MkdirTemp(cfg.TmpDir, "benchmarkoor-overlay-"+cfg.InstanceID+"-")
	if err != nil {
		return nil, fmt.Errorf("creating overlay base directory: %w", err)
	}

	// Create subdirectories for overlayfs.
	upperDir := filepath.Join(baseDir, "upper")
	workDir := filepath.Join(baseDir, "work")
	mergedDir := filepath.Join(baseDir, "merged")

	for _, dir := range []string{upperDir, workDir, mergedDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			if rmErr := os.RemoveAll(baseDir); rmErr != nil {
				p.log.WithError(rmErr).Warn("Failed to cleanup overlay base directory")
			}

			return nil, fmt.Errorf("creating overlay subdirectory %s: %w", dir, err)
		}
	}

	p.log.WithFields(logrus.Fields{
		"source":  cfg.SourceDir,
		"upper":   upperDir,
		"work":    workDir,
		"merged":  mergedDir,
		"basedir": baseDir,
	}).Info("Mounting overlayfs")

	// Mount overlayfs.
	// mount -t overlay overlay -o lowerdir=<src>,upperdir=<upper>,workdir=<work> <merged>
	mountOpts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s",
		cfg.SourceDir, upperDir, workDir)

	//nolint:gosec // Command args are controlled by the application.
	cmd := exec.CommandContext(ctx, "mount", "-t", "overlay", "overlay",
		"-o", mountOpts, mergedDir)

	if output, err := cmd.CombinedOutput(); err != nil {
		// Cleanup on failure.
		if rmErr := os.RemoveAll(baseDir); rmErr != nil {
			p.log.WithError(rmErr).Warn("Failed to cleanup overlay base directory")
		}

		return nil, fmt.Errorf("mounting overlayfs: %w (output: %s)", err, string(output))
	}

	p.log.WithField("mount_path", mergedDir).Info("OverlayFS mounted successfully")

	// Return prepared directory with cleanup function.
	return &PreparedDir{
		MountPath: mergedDir,
		Cleanup: func() error {
			return p.cleanup(mergedDir, baseDir)
		},
	}, nil
}

// cleanup unmounts the overlayfs and removes the temp directory.
func (p *overlayFSProvider) cleanup(mergedDir, baseDir string) error {
	p.log.WithField("mount_path", mergedDir).Info("Unmounting overlayfs")

	// Unmount the overlayfs.
	//nolint:gosec // Command args are controlled by the application.
	cmd := exec.Command("umount", mergedDir)

	if output, err := cmd.CombinedOutput(); err != nil {
		p.log.WithError(err).WithField("output", string(output)).
			Warn("Failed to unmount overlayfs")

		return fmt.Errorf("unmounting overlayfs: %w", err)
	}

	// Remove the temp directory.
	if err := os.RemoveAll(baseDir); err != nil {
		p.log.WithError(err).Warn("Failed to remove overlay base directory")

		return fmt.Errorf("removing overlay base directory: %w", err)
	}

	p.log.Info("OverlayFS cleanup completed")

	return nil
}
