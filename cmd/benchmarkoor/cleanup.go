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
	"github.com/spf13/cobra"
)

var forceCleanup bool

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove dangling benchmarkoor containers, volumes, and filesystem resources",
	Long: `Remove all Docker containers, volumes, and filesystem resources created by benchmarkoor.
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

func runCleanup(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Create Docker manager.
	dockerMgr, err := docker.NewManager(log)
	if err != nil {
		return fmt.Errorf("creating docker manager: %w", err)
	}

	if err := dockerMgr.Start(ctx); err != nil {
		return fmt.Errorf("starting docker manager: %w", err)
	}

	defer func() {
		if err := dockerMgr.Stop(); err != nil {
			log.WithError(err).Warn("Failed to stop docker manager")
		}
	}()

	return performCleanup(ctx, dockerMgr, forceCleanup)
}

// performCleanup lists and removes all benchmarkoor resources.
func performCleanup(ctx context.Context, dockerMgr docker.Manager, force bool) error {
	// List containers.
	containers, err := dockerMgr.ListContainers(ctx)
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}

	// List volumes.
	volumes, err := dockerMgr.ListVolumes(ctx)
	if err != nil {
		return fmt.Errorf("listing volumes: %w", err)
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
			fmt.Printf("  - %s (%s)\n", c.Name, c.ID[:12])
		}
	}

	if len(volumes) > 0 {
		fmt.Printf("\nVolumes to be removed (%d):\n", len(volumes))

		for _, v := range volumes {
			fmt.Printf("  - %s\n", v.Name)
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
		log.WithField("container", c.Name).Info("Removing container")

		if err := dockerMgr.RemoveContainer(ctx, c.ID); err != nil {
			log.WithError(err).WithField("container", c.Name).Warn("Failed to remove container")
		}
	}

	// Remove volumes.
	for _, v := range volumes {
		log.WithField("volume", v.Name).Info("Removing volume")

		if err := dockerMgr.RemoveVolume(ctx, v.Name); err != nil {
			log.WithError(err).WithField("volume", v.Name).Warn("Failed to remove volume")
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

// getCPUFreqCacheDir returns the cache directory for CPU frequency state files.
func getCPUFreqCacheDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}

	return filepath.Join(homeDir, ".cache", "benchmarkoor")
}
