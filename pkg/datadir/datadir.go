package datadir

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

// CopyProvider implements Provider using parallel file copying.
// It collects all files first, then uses multiple workers to copy them concurrently.
type CopyProvider interface {
	Provider
}

// NewCopyProvider creates a new parallel copy provider.
func NewCopyProvider(log logrus.FieldLogger) CopyProvider {
	return &copyProvider{
		log:     log.WithField("component", "datadir-copy"),
		workers: runtime.NumCPU(),
	}
}

type copyProvider struct {
	log     logrus.FieldLogger
	workers int
}

// Ensure interface compliance.
var _ CopyProvider = (*copyProvider)(nil)

// fileEntry represents a file to copy.
type fileEntry struct {
	srcPath  string
	dstPath  string
	size     int64
	mode     os.FileMode
	isDir    bool
	dirMode  os.FileMode
	linkDest string // For symlinks.
}

// Prepare copies the source directory to a temp location using parallel workers.
func (p *copyProvider) Prepare(ctx context.Context, cfg *ProviderConfig) (*PreparedDir, error) {
	// Create temp directory for the copy.
	copyDir, err := os.MkdirTemp(cfg.TmpDir, "benchmarkoor-datadir-"+cfg.InstanceID+"-")
	if err != nil {
		return nil, fmt.Errorf("creating temp datadir directory: %w", err)
	}

	p.log.WithFields(logrus.Fields{
		"source":  cfg.SourceDir,
		"dest":    copyDir,
		"workers": p.workers,
	}).Info("Copying data directory")

	// Perform parallel copy.
	if err := p.parallelCopy(ctx, cfg.SourceDir, copyDir); err != nil {
		// Cleanup on failure.
		if rmErr := os.RemoveAll(copyDir); rmErr != nil {
			p.log.WithError(rmErr).Warn("Failed to cleanup copy directory")
		}

		return nil, fmt.Errorf("copying datadir: %w", err)
	}

	// Return prepared directory with cleanup function.
	return &PreparedDir{
		MountPath: copyDir,
		Cleanup: func() error {
			p.log.WithField("path", copyDir).Info("Removing copied data directory")

			if err := os.RemoveAll(copyDir); err != nil {
				p.log.WithError(err).Warn("Failed to remove copied datadir")

				return fmt.Errorf("removing copied datadir: %w", err)
			}

			return nil
		},
	}, nil
}

// parallelCopy performs the parallel copy operation.
func (p *copyProvider) parallelCopy(ctx context.Context, src, dst string) error {
	// Collect all files and directories.
	files, totalSize, err := p.collectFiles(src, dst)
	if err != nil {
		return fmt.Errorf("collecting files: %w", err)
	}

	p.log.WithFields(logrus.Fields{
		"total_files": len(files),
		"total_size":  formatBytes(totalSize),
	}).Info("Starting copy")

	// Create all directories first (must be sequential to ensure parent dirs exist).
	for _, f := range files {
		if f.isDir {
			if err := os.MkdirAll(f.dstPath, f.dirMode); err != nil {
				return fmt.Errorf("creating directory %s: %w", f.dstPath, err)
			}
		}
	}

	// Filter to only files and symlinks.
	var fileEntries []fileEntry
	for _, f := range files {
		if !f.isDir {
			fileEntries = append(fileEntries, f)
		}
	}

	if len(fileEntries) == 0 {
		p.log.Info("No files to copy")

		return nil
	}

	// Track progress.
	var copiedBytes atomic.Int64
	var copiedFiles atomic.Int64

	// Start progress reporter.
	progressCtx, cancelProgress := context.WithCancel(ctx)
	defer cancelProgress()

	progressDone := make(chan struct{})

	go func() {
		defer close(progressDone)
		p.reportProgress(progressCtx, totalSize, int64(len(fileEntries)), &copiedBytes, &copiedFiles)
	}()

	// Create worker pool using errgroup.
	g, gctx := errgroup.WithContext(ctx)

	// Create job channel.
	jobs := make(chan fileEntry, len(fileEntries))

	// Start workers.
	for i := 0; i < p.workers; i++ {
		g.Go(func() error {
			for {
				select {
				case <-gctx.Done():
					return gctx.Err()
				case f, ok := <-jobs:
					if !ok {
						return nil
					}

					if err := p.copyEntry(gctx, f, &copiedBytes); err != nil {
						return err
					}

					copiedFiles.Add(1)
				}
			}
		})
	}

	// Send jobs.
	for _, f := range fileEntries {
		select {
		case <-gctx.Done():
			close(jobs)

			return gctx.Err()
		case jobs <- f:
		}
	}

	close(jobs)

	// Wait for all workers to finish.
	if err := g.Wait(); err != nil {
		cancelProgress()
		<-progressDone

		return fmt.Errorf("copying files: %w", err)
	}

	// Stop progress reporter.
	cancelProgress()
	<-progressDone

	p.log.WithFields(logrus.Fields{
		"copied_files": copiedFiles.Load(),
		"copied_bytes": formatBytes(copiedBytes.Load()),
	}).Info("Copy completed")

	return nil
}

// collectFiles walks the source directory and collects all files and directories.
func (p *copyProvider) collectFiles(src, dst string) ([]fileEntry, int64, error) {
	var files []fileEntry

	var totalSize int64

	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Calculate relative path.
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("getting relative path: %w", err)
		}

		dstPath := filepath.Join(dst, relPath)

		entry := fileEntry{
			srcPath: path,
			dstPath: dstPath,
			mode:    info.Mode(),
		}

		if info.IsDir() {
			entry.isDir = true
			entry.dirMode = info.Mode()
		} else if info.Mode()&os.ModeSymlink != 0 {
			// Handle symlinks.
			linkDest, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("reading symlink: %w", err)
			}

			entry.linkDest = linkDest
		} else {
			entry.size = info.Size()
			totalSize += info.Size()
		}

		files = append(files, entry)

		return nil
	})

	return files, totalSize, err
}

// copyEntry copies a single file entry (file or symlink).
func (p *copyProvider) copyEntry(ctx context.Context, f fileEntry, copiedBytes *atomic.Int64) error {
	if f.linkDest != "" {
		// Create symlink.
		return os.Symlink(f.linkDest, f.dstPath)
	}

	// Copy regular file.
	return p.copyFile(ctx, f.srcPath, f.dstPath, f.mode, copiedBytes)
}

// copyFile copies a single file.
func (p *copyProvider) copyFile(
	ctx context.Context,
	src, dst string,
	mode os.FileMode,
	copiedBytes *atomic.Int64,
) (err error) {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening source file: %w", err)
	}

	defer func() {
		if closeErr := srcFile.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("closing source file: %w", closeErr)
		}
	}()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("creating destination file: %w", err)
	}

	defer func() {
		if closeErr := dstFile.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("closing destination file: %w", closeErr)
		}
	}()

	// Copy with progress tracking.
	writer := &progressWriter{
		w:           dstFile,
		copiedBytes: copiedBytes,
	}

	// Copy in chunks to allow cancellation checks.
	buf := make([]byte, 64*1024) // 64KB buffer for better throughput.

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, readErr := srcFile.Read(buf)
		if n > 0 {
			if _, writeErr := writer.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("writing to destination: %w", writeErr)
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				break
			}

			return fmt.Errorf("reading source file: %w", readErr)
		}
	}

	return nil
}

// reportProgress logs copy progress every second.
func (p *copyProvider) reportProgress(
	ctx context.Context,
	totalSize int64,
	totalFiles int64,
	copiedBytes *atomic.Int64,
	copiedFiles *atomic.Int64,
) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			bytes := copiedBytes.Load()
			files := copiedFiles.Load()

			var percent float64
			if totalSize > 0 {
				percent = float64(bytes) / float64(totalSize) * 100
			}

			p.log.WithFields(logrus.Fields{
				"progress":     fmt.Sprintf("%.1f%%", percent),
				"copied_bytes": formatBytes(bytes),
				"total_bytes":  formatBytes(totalSize),
				"copied_files": files,
				"total_files":  totalFiles,
			}).Info("Copy progress")
		}
	}
}

// progressWriter wraps an io.Writer to track bytes written.
type progressWriter struct {
	w           io.Writer
	copiedBytes *atomic.Int64
}

func (pw *progressWriter) Write(p []byte) (n int, err error) {
	n, err = pw.w.Write(p)
	pw.copiedBytes.Add(int64(n))

	return n, err
}

// formatBytes formats bytes in a human-readable format.
func formatBytes(bytes int64) string {
	const unit = 1024

	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
