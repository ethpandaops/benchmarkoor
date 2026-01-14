package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ethpandaops/benchmarkoor/pkg/docker"
	"github.com/spf13/cobra"
)

var forceCleanup bool

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove dangling benchmarkoor containers and volumes",
	Long: `Remove all Docker containers and volumes created by benchmarkoor.
This is useful for cleaning up after failed runs or interrupted benchmarks.`,
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

	if len(containers) == 0 && len(volumes) == 0 {
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

	log.Info("Cleanup completed")

	return nil
}
