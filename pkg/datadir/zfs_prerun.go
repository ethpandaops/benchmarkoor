package datadir

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

// zfsPreRunOutputProp is a ZFS user property naming the output_dir a pre-run
// clone was made for. It marks the dataset as ours, so a re-run may replace it,
// and nothing else is ever destroyed.
const zfsPreRunOutputProp = "benchmarkoor:prerun-output"

// ZFSPreRunNames returns the snapshot and clone that back a pre-run output_dir
// cloned from sourceDataset. The names are derived from the output path, so a
// re-run of the same target finds its previous clone. They deliberately avoid the
// "benchmarkoor-" prefixes ListOrphanedZFSResources reaps: a pre-run's output is
// meant to outlive the process that made it.
func ZFSPreRunNames(sourceDataset, outputDir string) (snapshot, clone string) {
	sum := sha256.Sum256([]byte(filepath.Clean(outputDir)))
	id := hex.EncodeToString(sum[:])[:12]

	return fmt.Sprintf("%s@prerun-%s", sourceDataset, id), fmt.Sprintf("%s/prerun-%s", sourceDataset, id)
}

// CloneZFSPreRunOutput makes outputDir a persistent ZFS clone of sourceDir
// instead of a copy: O(1) in time and in space until the pre-run writes. The
// source is never modified, and the clone is not a disposable benchmark clone:
// it holds the advanced datadir for downstream stages until a forced re-run
// replaces it. sourceDir must be a dataset's mountpoint, so that the clone's
// root, mounted at outputDir, is the datadir itself.
func CloneZFSPreRunOutput(ctx context.Context, log logrus.FieldLogger, sourceDir, outputDir string) error {
	p := &zfsProvider{log: log}

	info, err := p.getDatasetFromPath(ctx, sourceDir)
	if err != nil {
		return fmt.Errorf("detecting ZFS dataset for source_dir %q: %w", sourceDir, err)
	}

	if info.relativePath != "" {
		return fmt.Errorf(
			"datadir_method zfs needs source_dir to be a dataset's mountpoint; %q is %q inside dataset %s (mounted at %s)",
			sourceDir, info.relativePath, info.dataset, info.mountpoint,
		)
	}

	out, err := filepath.Abs(outputDir)
	if err != nil {
		return fmt.Errorf("resolving output_dir %q: %w", outputDir, err)
	}

	snapshot, clone := ZFSPreRunNames(info.dataset, out)

	if err := replacePreviousPreRunClone(ctx, log, snapshot, clone, out); err != nil {
		return err
	}

	// The clone mounts onto output_dir, which must be an empty directory.
	if err := os.MkdirAll(out, 0o755); err != nil {
		return fmt.Errorf("creating output_dir %q: %w", out, err)
	}

	entries, err := os.ReadDir(out)
	if err != nil {
		return fmt.Errorf("reading output_dir %q: %w", out, err)
	}

	if len(entries) > 0 {
		return fmt.Errorf("output_dir %q is not empty; a ZFS clone can only be mounted on an empty directory", out)
	}

	log.WithFields(logrus.Fields{
		"source_dataset": info.dataset,
		"snapshot":       snapshot,
		"clone":          clone,
		"output_dir":     out,
	}).Info("Cloning the snapshot datadir into output_dir (ZFS, no copy)")

	if err := runZFS(ctx, "snapshot", snapshot); err != nil {
		return err
	}

	if err := runZFS(ctx, "clone", "-o", "mountpoint="+out, "-o", zfsPreRunOutputProp+"="+out, snapshot, clone); err != nil {
		if derr := runZFS(context.Background(), "destroy", snapshot); derr != nil {
			log.WithError(derr).Warn("Failed to remove snapshot after clone failure")
		}

		return err
	}

	mounted, err := IsMountedAt(out)
	if err != nil {
		return fmt.Errorf("checking that %q is mounted: %w", out, err)
	}

	if !mounted {
		return fmt.Errorf("ZFS clone %s was created but %q is not mounted", clone, out)
	}

	return nil
}

// replacePreviousPreRunClone destroys the clone and snapshot a previous run of
// this target left, refusing if the dataset under that name is not ours.
func replacePreviousPreRunClone(ctx context.Context, log logrus.FieldLogger, snapshot, clone, out string) error {
	owner, err := runZFSOutput(ctx, "get", "-H", "-o", "value", zfsPreRunOutputProp, clone)
	if err != nil {
		// No clone. A snapshot of this name without one is a run that died between
		// the two commands; the name encodes this output_dir, so it is ours to drop.
		clones, serr := runZFSOutput(ctx, "get", "-H", "-o", "value", "clones", snapshot)
		if serr != nil {
			return nil //nolint:nilerr // neither exists: the expected first-run case.
		}

		if c := strings.TrimSpace(clones); c != "" && c != "-" {
			return fmt.Errorf("snapshot %s has clones (%s) but %s is missing; refusing to touch it", snapshot, c, clone)
		}

		log.WithField("snapshot", snapshot).Warn("Removing a pre-run snapshot left without its clone")

		return runZFS(ctx, "destroy", snapshot)
	}

	if strings.TrimSpace(owner) != out {
		return fmt.Errorf("dataset %s exists but was not created for output_dir %q (%s=%q); refusing to destroy it",
			clone, out, zfsPreRunOutputProp, strings.TrimSpace(owner))
	}

	log.WithField("clone", clone).Info("Replacing the previous pre-run clone")

	if err := runZFS(ctx, "destroy", clone); err != nil {
		return err
	}

	return runZFS(ctx, "destroy", snapshot)
}

func runZFS(ctx context.Context, args ...string) error {
	_, err := runZFSOutput(ctx, args...)

	return err
}

func runZFSOutput(ctx context.Context, args ...string) (string, error) {
	//nolint:gosec // Command args are controlled by the application.
	cmd := exec.CommandContext(ctx, "zfs", args...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("zfs %s: %w (output: %s)", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}

	return string(output), nil
}
