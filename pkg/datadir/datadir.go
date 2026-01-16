package datadir

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// Copier copies data directories with progress reporting.
type Copier interface {
	// Copy copies the source directory to the destination with progress reporting.
	Copy(ctx context.Context, src, dst string) error
}

// NewCopier creates a new directory copier.
func NewCopier(log logrus.FieldLogger) Copier {
	return &copier{
		log: log.WithField("component", "datadir-copier"),
	}
}

type copier struct {
	log logrus.FieldLogger
}

// Ensure interface compliance.
var _ Copier = (*copier)(nil)

// Copy copies the source directory to the destination with progress reporting.
func (c *copier) Copy(ctx context.Context, src, dst string) error {
	// Calculate total size for progress reporting.
	totalSize, err := c.calculateSize(src)
	if err != nil {
		return fmt.Errorf("calculating source size: %w", err)
	}

	c.log.WithFields(logrus.Fields{
		"src":        src,
		"dst":        dst,
		"total_size": formatBytes(totalSize),
	}).Info("Starting directory copy")

	// Track progress.
	var copiedBytes atomic.Int64
	var currentFile atomic.Value

	currentFile.Store("")

	// Start progress reporter.
	progressCtx, cancelProgress := context.WithCancel(ctx)
	defer cancelProgress()

	progressDone := make(chan struct{})

	go func() {
		defer close(progressDone)
		c.reportProgress(progressCtx, totalSize, &copiedBytes, &currentFile)
	}()

	// Perform the copy.
	if err := c.copyDir(ctx, src, dst, &copiedBytes, &currentFile); err != nil {
		cancelProgress()
		<-progressDone

		return fmt.Errorf("copying directory: %w", err)
	}

	// Stop progress reporter and wait for it to finish.
	cancelProgress()
	<-progressDone

	c.log.WithFields(logrus.Fields{
		"copied": formatBytes(copiedBytes.Load()),
	}).Info("Directory copy completed")

	return nil
}

// calculateSize recursively calculates the total size of a directory.
func (c *copier) calculateSize(path string) (int64, error) {
	var size int64

	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			size += info.Size()
		}

		return nil
	})

	return size, err
}

// reportProgress logs copy progress every second.
func (c *copier) reportProgress(
	ctx context.Context,
	totalSize int64,
	copiedBytes *atomic.Int64,
	currentFile *atomic.Value,
) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			copied := copiedBytes.Load()
			file := currentFile.Load().(string)

			var percent float64
			if totalSize > 0 {
				percent = float64(copied) / float64(totalSize) * 100
			}

			c.log.WithFields(logrus.Fields{
				"progress":     fmt.Sprintf("%.1f%%", percent),
				"copied":       formatBytes(copied),
				"total":        formatBytes(totalSize),
				"current_file": filepath.Base(file),
			}).Info("Copy progress")
		}
	}
}

// copyDir recursively copies a directory.
func (c *copier) copyDir(
	ctx context.Context,
	src, dst string,
	copiedBytes *atomic.Int64,
	currentFile *atomic.Value,
) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	// Create destination directory with same permissions.
	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return fmt.Errorf("creating destination directory: %w", err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("reading source directory: %w", err)
	}

	for _, entry := range entries {
		// Check for cancellation.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := c.copyDir(ctx, srcPath, dstPath, copiedBytes, currentFile); err != nil {
				return err
			}
		} else {
			if err := c.copyFile(ctx, srcPath, dstPath, copiedBytes, currentFile); err != nil {
				return err
			}
		}
	}

	return nil
}

// copyFile copies a single file preserving permissions.
func (c *copier) copyFile(
	ctx context.Context,
	src, dst string,
	copiedBytes *atomic.Int64,
	currentFile *atomic.Value,
) (err error) {
	currentFile.Store(src)

	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening source file: %w", err)
	}

	defer func() {
		if closeErr := srcFile.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("closing source file: %w", closeErr)
		}
	}()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("stat source file: %w", err)
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
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
	buf := make([]byte, 32*1024) // 32KB buffer

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
