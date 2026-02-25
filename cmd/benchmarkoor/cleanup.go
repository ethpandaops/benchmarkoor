package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethpandaops/benchmarkoor/pkg/cpufreq"
	"github.com/ethpandaops/benchmarkoor/pkg/datadir"
	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/ethpandaops/benchmarkoor/pkg/podman"
	"github.com/spf13/cobra"
)

var forceCleanup bool

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove dangling benchmarkoor containers, volumes, and filesystem resources",
	Long: `Remove all containers, volumes, and filesystem resources created by benchmarkoor.
This is useful for cleaning up after failed runs or interrupted benchmarks.

Filesystem resources that may be left behind if the process was killed:
  - ZFS clones and snapshots
  - OverlayFS mounts and temp directories
  - fuse-overlayfs mounts and temp directories
  - CPU frequency state files (restores original CPU settings)`,
	RunE: runCleanup,
}

func init() {
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().BoolVarP(&forceCleanup, "force", "f", false, "Skip confirmation prompt")
}

// managedContainer associates a container with the manager that owns it.
type managedContainer struct {
	info docker.ContainerInfo
	mgr  docker.ContainerManager
}

// managedVolume associates a volume with the manager that owns it.
type managedVolume struct {
	info docker.VolumeInfo
	mgr  docker.ContainerManager
}

func runCleanup(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	managers := buildCleanupManagers(ctx)
	if len(managers) == 0 {
		return fmt.Errorf("no container runtimes available (tried Docker and Podman)")
	}

	defer func() {
		for _, mgr := range managers {
			if err := mgr.Stop(); err != nil {
				log.WithError(err).Warn("Failed to stop container manager")
			}
		}
	}()

	return performCleanup(ctx, managers, forceCleanup)
}

// performCleanup lists and removes all benchmarkoor resources across all
// provided container managers (runtimes).
func performCleanup(ctx context.Context, managers []docker.ContainerManager, force bool) error {
	// Collect containers and volumes from all runtimes.
	var containers []managedContainer

	var volumes []managedVolume

	for _, mgr := range managers {
		cl, err := mgr.ListContainers(ctx)
		if err != nil {
			log.WithError(err).Warn("Failed to list containers from a runtime")
		}

		for _, c := range cl {
			containers = append(containers, managedContainer{info: c, mgr: mgr})
		}

		vl, err := mgr.ListVolumes(ctx)
		if err != nil {
			log.WithError(err).Warn("Failed to list volumes from a runtime")
		}

		for _, v := range vl {
			volumes = append(volumes, managedVolume{info: v, mgr: mgr})
		}
	}

	// List orphaned ZFS resources.
	zfsResources, err := datadir.ListOrphanedZFSResources(ctx)
	if err != nil {
		log.WithError(err).Warn("Failed to list ZFS resources")
	}

	// List orphaned overlay mounts.
	overlayMounts, err := datadir.ListOrphanedOverlayMounts(ctx)
	if err != nil {
		log.WithError(err).Warn("Failed to list overlay mounts")
	}

	// List orphaned CPU frequency state files.
	cpufreqStateFiles, err := cpufreq.ListOrphanedStateFiles(getCPUFreqCacheDir())
	if err != nil {
		log.WithError(err).Warn("Failed to list CPU frequency state files")
	}

	if len(containers) == 0 && len(volumes) == 0 && len(zfsResources) == 0 &&
		len(overlayMounts) == 0 && len(cpufreqStateFiles) == 0 {
		log.Info("No benchmarkoor resources found")

		return nil
	}

	// Display resources to be deleted.
	if len(containers) > 0 {
		fmt.Printf("\nContainers to be removed (%d):\n", len(containers))

		for _, c := range containers {
			fmt.Printf("  - %s (%s)\n", c.info.Name, c.info.ID[:12])
		}
	}

	if len(volumes) > 0 {
		fmt.Printf("\nVolumes to be removed (%d):\n", len(volumes))

		for _, v := range volumes {
			fmt.Printf("  - %s\n", v.info.Name)
		}
	}

	if len(zfsResources) > 0 {
		fmt.Printf("\nZFS resources to be removed (%d):\n", len(zfsResources))

		for _, r := range zfsResources {
			fmt.Printf("  - %s (%s)\n", r.Name, r.Type)
		}
	}

	if len(overlayMounts) > 0 {
		fmt.Printf("\nOverlay mounts to be removed (%d):\n", len(overlayMounts))

		for _, m := range overlayMounts {
			fmt.Printf("  - %s (%s)\n", m.BaseDir, m.Type)
		}
	}

	if len(cpufreqStateFiles) > 0 {
		fmt.Printf("\nCPU frequency state files to be restored and removed (%d):\n", len(cpufreqStateFiles))

		for _, sf := range cpufreqStateFiles {
			fmt.Printf("  - %s (created: %s)\n", sf.Path, sf.Timestamp.Format("2006-01-02 15:04:05"))
		}
	}

	fmt.Println()

	// Prompt for confirmation if not forced.
	if !force {
		fmt.Print("Are you sure you want to remove these resources? [y/N] ")

		reader := bufio.NewReader(os.Stdin)

		response, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("reading response: %w", err)
		}

		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			log.Info("Cleanup cancelled")

			return nil
		}
	}

	// Remove containers first.
	for _, c := range containers {
		log.WithField("container", c.info.Name).Info("Removing container")

		if err := c.mgr.RemoveContainer(ctx, c.info.ID); err != nil {
			log.WithError(err).WithField("container", c.info.Name).Warn("Failed to remove container")
		}
	}

	// Remove volumes.
	for _, v := range volumes {
		log.WithField("volume", v.info.Name).Info("Removing volume")

		if err := v.mgr.RemoveVolume(ctx, v.info.Name); err != nil {
			log.WithError(err).WithField("volume", v.info.Name).Warn("Failed to remove volume")
		}
	}

	// Remove ZFS resources (clones first, then snapshots).
	if len(zfsResources) > 0 {
		if err := datadir.CleanupOrphanedZFSResources(ctx, log, zfsResources); err != nil {
			log.WithError(err).Warn("Failed to cleanup ZFS resources")
		}
	}

	// Remove overlay mounts (unmount first, then remove directories).
	if len(overlayMounts) > 0 {
		if err := datadir.CleanupOrphanedOverlayMounts(ctx, log, overlayMounts); err != nil {
			log.WithError(err).Warn("Failed to cleanup overlay mounts")
		}
	}

	// Restore CPU frequency settings from orphaned state files and remove them.
	if len(cpufreqStateFiles) > 0 {
		if err := cpufreq.CleanupOrphanedCPUFreqState(
			ctx, log, cpufreqStateFiles, cpufreq.DefaultSysfsCPUPath,
		); err != nil {
			log.WithError(err).Warn("Failed to cleanup CPU frequency state files")
		}
	}

	log.Info("Cleanup completed")

	return nil
}

// buildCleanupManagers tries to create and start container managers for both
// Docker and Podman. Runtimes that are unavailable (e.g. socket missing) are
// silently skipped. The caller is responsible for stopping all returned managers.
func buildCleanupManagers(ctx context.Context) []docker.ContainerManager {
	managers := make([]docker.ContainerManager, 0, 2)

	// Try Docker.
	dockerMgr, err := docker.NewManager(log)
	if err != nil {
		log.WithError(err).Debug("Docker runtime not available for cleanup")
	} else if err := dockerMgr.Start(ctx); err != nil {
		log.WithError(err).Debug("Failed to start Docker manager for cleanup")
	} else {
		managers = append(managers, dockerMgr)
	}

	// Try Podman.
	podmanMgr, err := podman.NewManager(log)
	if err != nil {
		log.WithError(err).Debug("Podman runtime not available for cleanup")
	} else if err := podmanMgr.Start(ctx); err != nil {
		log.WithError(err).Debug("Failed to start Podman manager for cleanup")
	} else {
		managers = append(managers, podmanMgr)
	}

	return managers
}

// getCPUFreqCacheDir returns the cache directory for CPU frequency state files.
func getCPUFreqCacheDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}

	return filepath.Join(homeDir, ".cache", "benchmarkoor")
}
