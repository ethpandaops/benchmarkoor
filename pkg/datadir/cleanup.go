package datadir

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

// OrphanedOverlayMount represents an orphaned overlay filesystem mount.
type OrphanedOverlayMount struct {
	MountPoint string // The merged directory mount point
	BaseDir    string // The parent temp directory
	Type       string // "overlayfs" or "fuse-overlayfs"
}

// ListOrphanedOverlayMounts finds overlay and fuse-overlayfs mounts created by benchmarkoor.
// These may be left behind if the process was killed before cleanup.
func ListOrphanedOverlayMounts(ctx context.Context) ([]OrphanedOverlayMount, error) {
	var mounts []OrphanedOverlayMount

	// Read /proc/mounts to find active mounts.
	file, err := os.Open("/proc/mounts")
	if err != nil {
		// /proc/mounts not available (non-Linux), return empty list.
		return nil, nil //nolint:nilerr // Not an error on non-Linux systems.
	}
	defer func() { _ = file.Close() }()

	// Build a map of mounted paths for quick lookup.
	mountedPaths := make(map[string]string, 64) // path -> filesystem type

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}

		mountPoint := fields[1]
		fsType := fields[2]

		// Track overlay and fuse mounts.
		if fsType == "overlay" || fsType == "fuse.fuse-overlayfs" {
			mountedPaths[mountPoint] = fsType
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Search common temp directories for benchmarkoor overlay directories.
	tmpDirs := []string{
		os.TempDir(),
		"/tmp",
		"/var/tmp",
	}

	seen := make(map[string]struct{}, 16)

	for _, tmpDir := range tmpDirs {
		entries, err := os.ReadDir(tmpDir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			name := entry.Name()
			baseDir := filepath.Join(tmpDir, name)

			// Skip if already seen (in case /tmp and os.TempDir() are the same).
			if _, ok := seen[baseDir]; ok {
				continue
			}

			seen[baseDir] = struct{}{}

			// Check for benchmarkoor overlay directories.
			var overlayType string

			if strings.HasPrefix(name, "benchmarkoor-overlay-") {
				overlayType = "overlayfs"
			} else if strings.HasPrefix(name, "benchmarkoor-fuse-overlay-") {
				overlayType = "fuse-overlayfs"
			} else {
				continue
			}

			mergedDir := filepath.Join(baseDir, "merged")

			// Check if the merged directory exists and is mounted.
			if _, err := os.Stat(mergedDir); err != nil {
				// Directory structure incomplete, but still orphaned - add for cleanup.
				mounts = append(mounts, OrphanedOverlayMount{
					MountPoint: mergedDir,
					BaseDir:    baseDir,
					Type:       overlayType,
				})

				continue
			}

			// Check if it's actually mounted.
			if _, mounted := mountedPaths[mergedDir]; mounted {
				mounts = append(mounts, OrphanedOverlayMount{
					MountPoint: mergedDir,
					BaseDir:    baseDir,
					Type:       overlayType,
				})
			} else {
				// Not mounted but directory exists - still orphaned, needs cleanup.
				mounts = append(mounts, OrphanedOverlayMount{
					MountPoint: mergedDir,
					BaseDir:    baseDir,
					Type:       overlayType,
				})
			}
		}
	}

	return mounts, nil
}

// CleanupOrphanedOverlayMounts unmounts and removes orphaned overlay directories.
func CleanupOrphanedOverlayMounts(ctx context.Context, log logrus.FieldLogger, mounts []OrphanedOverlayMount) error {
	for _, mount := range mounts {
		log.WithFields(logrus.Fields{
			"mount_point": mount.MountPoint,
			"base_dir":    mount.BaseDir,
			"type":        mount.Type,
		}).Info("Cleaning up orphaned overlay mount")

		// Try to unmount first.
		var unmountCmd *exec.Cmd

		if mount.Type == "fuse-overlayfs" {
			//nolint:gosec // Command args are controlled by the application.
			unmountCmd = exec.CommandContext(ctx, "fusermount", "-u", mount.MountPoint)
		} else {
			//nolint:gosec // Command args are controlled by the application.
			unmountCmd = exec.CommandContext(ctx, "umount", mount.MountPoint)
		}

		if output, err := unmountCmd.CombinedOutput(); err != nil {
			// Unmount failed - might not be mounted, try lazy unmount.
			log.WithError(err).WithField("output", string(output)).Debug("Regular unmount failed, trying lazy unmount")

			//nolint:gosec // Command args are controlled by the application.
			lazyCmd := exec.CommandContext(ctx, "umount", "-l", mount.MountPoint)

			if output, err := lazyCmd.CombinedOutput(); err != nil {
				log.WithError(err).WithFields(logrus.Fields{
					"mount_point": mount.MountPoint,
					"output":      string(output),
				}).Warn("Failed to unmount orphaned overlay (may not be mounted)")
			}
		}

		// Remove the base directory.
		if err := os.RemoveAll(mount.BaseDir); err != nil {
			log.WithError(err).WithField("base_dir", mount.BaseDir).Warn("Failed to remove orphaned overlay directory")
		} else {
			log.WithField("base_dir", mount.BaseDir).Info("Removed orphaned overlay directory")
		}
	}

	return nil
}
