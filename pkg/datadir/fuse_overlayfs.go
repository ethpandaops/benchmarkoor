package datadir

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

// FuseOverlayFSProvider implements Provider using fuse-overlayfs.
// It provides unprivileged overlay filesystem support via FUSE,
// the same approach used by rootless Podman/Docker.
type FuseOverlayFSProvider interface {
	Provider
}

// NewFuseOverlayFSProvider creates a new fuse-overlayfs provider.
func NewFuseOverlayFSProvider(log logrus.FieldLogger) FuseOverlayFSProvider {
	return &fuseOverlayFSProvider{
		log: log.WithField("component", "datadir-fuse-overlayfs"),
	}
}

type fuseOverlayFSProvider struct {
	log logrus.FieldLogger
}

// Ensure interface compliance.
var _ FuseOverlayFSProvider = (*fuseOverlayFSProvider)(nil)

// Prepare creates a fuse-overlayfs mount with the source as the lower layer.
func (p *fuseOverlayFSProvider) Prepare(ctx context.Context, cfg *ProviderConfig) (*PreparedDir, error) {
	// Create temp directory for overlay structure.
	baseDir, err := os.MkdirTemp(cfg.TmpDir, "benchmarkoor-fuse-overlay-"+cfg.InstanceID+"-")
	if err != nil {
		return nil, fmt.Errorf("creating fuse-overlay base directory: %w", err)
	}

	// Create subdirectories for fuse-overlayfs.
	upperDir := filepath.Join(baseDir, "upper")
	workDir := filepath.Join(baseDir, "work")
	mergedDir := filepath.Join(baseDir, "merged")

	for _, dir := range []string{upperDir, workDir, mergedDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			if rmErr := os.RemoveAll(baseDir); rmErr != nil {
				p.log.WithError(rmErr).Warn("Failed to cleanup fuse-overlay base directory")
			}

			return nil, fmt.Errorf("creating fuse-overlay subdirectory %s: %w", dir, err)
		}
	}

	p.log.WithFields(logrus.Fields{
		"source":  cfg.SourceDir,
		"upper":   upperDir,
		"work":    workDir,
		"merged":  mergedDir,
		"basedir": baseDir,
	}).Info("Mounting fuse-overlayfs")

	// Mount fuse-overlayfs.
	// allow_root: required so Docker daemon can access the mount (needs user_allow_other in /etc/fuse.conf)
	// squash_to_uid/gid=0: make all files appear owned by root so container can write
	mountOpts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s,allow_root,squash_to_uid=0,squash_to_gid=0",
		cfg.SourceDir, upperDir, workDir)

	//nolint:gosec // Command args are controlled by the application.
	cmd := exec.CommandContext(ctx, "fuse-overlayfs", "-o", mountOpts, mergedDir)

	if output, err := cmd.CombinedOutput(); err != nil {
		// Cleanup on failure.
		if rmErr := os.RemoveAll(baseDir); rmErr != nil {
			p.log.WithError(rmErr).Warn("Failed to cleanup fuse-overlay base directory")
		}

		return nil, fmt.Errorf("mounting fuse-overlayfs: %w (output: %s)", err, string(output))
	}

	p.log.WithField("mount_path", mergedDir).Info("fuse-overlayfs mounted successfully")

	// Return prepared directory with cleanup function.
	return &PreparedDir{
		MountPath: mergedDir,
		Cleanup: func() error {
			return p.cleanup(mergedDir, baseDir)
		},
	}, nil
}

// cleanup unmounts the fuse-overlayfs and removes the temp directory.
func (p *fuseOverlayFSProvider) cleanup(mergedDir, baseDir string) error {
	p.log.WithField("mount_path", mergedDir).Info("Unmounting fuse-overlayfs")

	// Unmount the fuse-overlayfs using fusermount.
	//nolint:gosec // Command args are controlled by the application.
	cmd := exec.Command("fusermount", "-u", mergedDir)

	if output, err := cmd.CombinedOutput(); err != nil {
		p.log.WithError(err).WithField("output", string(output)).
			Warn("Failed to unmount fuse-overlayfs")

		return fmt.Errorf("unmounting fuse-overlayfs: %w", err)
	}

	// Remove the temp directory.
	if err := os.RemoveAll(baseDir); err != nil {
		p.log.WithError(err).Warn("Failed to remove fuse-overlay base directory")

		return fmt.Errorf("removing fuse-overlay base directory: %w", err)
	}

	p.log.Info("fuse-overlayfs cleanup completed")

	return nil
}
