package executor

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/sirupsen/logrus"
)

const (
	archiveFormatZip   = "zip"
	archiveFormatTarGz = "targz"
)

// extractZipFile extracts a zip archive to the target directory.
func extractZipFile(zipPath, targetDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("opening zip: %w", err)
	}

	defer func() { _ = r.Close() }()

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("creating target directory: %w", err)
	}

	for _, f := range r.File {
		target := filepath.Join(targetDir, filepath.Clean(f.Name))
		if !strings.HasPrefix(target, filepath.Clean(targetDir)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid zip entry: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return fmt.Errorf("creating directory: %w", err)
			}

			continue
		}

		// Ensure parent directory exists.
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fmt.Errorf("creating parent directory: %w", err)
		}

		outFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			return fmt.Errorf("creating file: %w", err)
		}

		rc, err := f.Open()
		if err != nil {
			_ = outFile.Close()

			return fmt.Errorf("opening zip entry: %w", err)
		}

		if _, err := io.Copy(outFile, rc); err != nil {
			_ = rc.Close()
			_ = outFile.Close()

			return fmt.Errorf("extracting file: %w", err)
		}

		_ = rc.Close()
		_ = outFile.Close()
	}

	return nil
}

// extractTarGzFile extracts a .tar.gz file to the target directory.
func extractTarGzFile(tarballPath, targetDir string) error {
	f, err := os.Open(tarballPath)
	if err != nil {
		return fmt.Errorf("opening tarball: %w", err)
	}

	defer func() { _ = f.Close() }()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("creating gzip reader: %w", err)
	}

	defer func() { _ = gzr.Close() }()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			return fmt.Errorf("reading tar: %w", err)
		}

		target := filepath.Join(targetDir, filepath.Clean(header.Name))
		if !strings.HasPrefix(target, filepath.Clean(targetDir)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid tar entry: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return fmt.Errorf("creating directory: %w", err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("creating parent directory: %w", err)
			}

			outFile, err := os.OpenFile(
				target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode),
			)
			if err != nil {
				return fmt.Errorf("creating file: %w", err)
			}

			if _, err := io.Copy(outFile, tr); err != nil {
				_ = outFile.Close()

				return fmt.Errorf("extracting file: %w", err)
			}

			_ = outFile.Close()
		}
	}

	return nil
}

// extractInnerTarballs finds .tar.gz files in the directory, extracts them
// in-place, and removes the original tarball.
func extractInnerTarballs(dir string, log logrus.FieldLogger) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading directory %s: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tar.gz") {
			continue
		}

		tarballPath := filepath.Join(dir, entry.Name())

		log.WithField("file", tarballPath).Debug("Extracting inner tarball")

		if err := extractTarGzFile(tarballPath, dir); err != nil {
			return fmt.Errorf("extracting %s: %w", entry.Name(), err)
		}

		// Remove the tarball after successful extraction.
		if err := os.Remove(tarballPath); err != nil {
			log.WithError(err).WithField("file", tarballPath).
				Warn("Failed to remove extracted tarball")
		}
	}

	return nil
}

const (
	progressLogInterval = 10 * 1024 * 1024 // 10 MiB between progress logs
	defaultChunkSize    = 25 * 1024 * 1024 // 25 MiB per chunk
	defaultParallelism  = 8                // number of parallel download workers
	minParallelSize     = 10 * 1024 * 1024 // don't parallelize below 10 MiB
)

// downloadToFile downloads a URL to a local file. It probes for range request
// support and downloads chunks in parallel when the server supports it,
// falling back to a single sequential download otherwise.
func downloadToFile(
	ctx context.Context, url, destPath, bearerToken string, log logrus.FieldLogger,
) error {
	totalSize, supportsRange, err := probeDownload(ctx, url, bearerToken)
	if err != nil {
		return err
	}

	if supportsRange && totalSize >= minParallelSize {
		log.WithFields(logrus.Fields{
			"size":    formatBytes(totalSize),
			"workers": defaultParallelism,
		}).Info("Downloading with parallel range requests")

		return downloadParallel(ctx, url, destPath, bearerToken, totalSize, log)
	}

	if totalSize > 0 {
		log.WithField("size", formatBytes(totalSize)).
			Info("Downloading (server does not support range requests)")
	} else {
		log.Info("Downloading (unknown size)")
	}

	return downloadSequential(ctx, url, destPath, bearerToken, totalSize, log)
}

// probeDownload sends a HEAD request to determine file size and range support.
func probeDownload(
	ctx context.Context, url, bearerToken string,
) (totalSize int64, supportsRange bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return 0, false, fmt.Errorf("creating HEAD request: %w", err)
	}

	applyAuth(req, bearerToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, false, fmt.Errorf("probing %s: %w", url, err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, false, fmt.Errorf("probing %s: HTTP %d", url, resp.StatusCode)
	}

	acceptRanges := resp.Header.Get("Accept-Ranges")
	supportsRange = acceptRanges == "bytes" && resp.ContentLength > 0

	return resp.ContentLength, supportsRange, nil
}

// downloadSequential downloads the file in a single GET request with progress
// logging.
func downloadSequential(
	ctx context.Context, url, destPath, bearerToken string,
	totalSize int64, log logrus.FieldLogger,
) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	applyAuth(req, bearerToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", url, err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading %s: HTTP %d", url, resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating file %s: %w", destPath, err)
	}

	pw := newProgressLogger(log, totalSize)

	if _, err := io.Copy(out, io.TeeReader(resp.Body, pw)); err != nil {
		_ = out.Close()
		_ = os.Remove(destPath)

		return fmt.Errorf("writing file %s: %w", destPath, err)
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("closing file %s: %w", destPath, err)
	}

	log.WithField("size", formatBytes(pw.Written())).Info("Download complete")

	return nil
}

// downloadParallel downloads the file using multiple concurrent range requests
// and assembles the chunks into the destination file.
func downloadParallel(
	ctx context.Context, url, destPath, bearerToken string,
	totalSize int64, log logrus.FieldLogger,
) error {
	// Pre-allocate the output file.
	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating file %s: %w", destPath, err)
	}

	if err := out.Truncate(totalSize); err != nil {
		_ = out.Close()

		return fmt.Errorf("pre-allocating file: %w", err)
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("closing pre-allocated file: %w", err)
	}

	// Build chunk list.
	type chunk struct {
		index      int
		start, end int64 // inclusive byte range
	}

	chunks := make([]chunk, 0, (totalSize/defaultChunkSize)+1)

	for start := int64(0); start < totalSize; start += defaultChunkSize {
		end := start + defaultChunkSize - 1
		if end >= totalSize {
			end = totalSize - 1
		}

		chunks = append(chunks, chunk{
			index: len(chunks),
			start: start,
			end:   end,
		})
	}

	pw := newProgressLogger(log, totalSize)

	// Download chunks in parallel.
	var (
		wg      sync.WaitGroup
		errOnce sync.Once
		dlErr   error
		sem     = make(chan struct{}, defaultParallelism)
	)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for _, c := range chunks {
		wg.Add(1)

		go func(c chunk) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				errOnce.Do(func() { dlErr = ctx.Err() })

				return
			}

			if err := downloadChunk(ctx, url, destPath, bearerToken, c.start, c.end, pw); err != nil {
				errOnce.Do(func() {
					dlErr = fmt.Errorf("chunk %d (%s-%s): %w",
						c.index, formatBytes(c.start), formatBytes(c.end), err,
					)
					cancel()
				})
			}
		}(c)
	}

	wg.Wait()

	if dlErr != nil {
		_ = os.Remove(destPath)

		return fmt.Errorf("parallel download failed: %w", dlErr)
	}

	log.WithField("size", formatBytes(totalSize)).Info("Download complete")

	return nil
}

// downloadChunk downloads a single byte range and writes it at the correct
// offset in the destination file.
func downloadChunk(
	ctx context.Context, url, destPath, bearerToken string,
	start, end int64, pw *progressLogger,
) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	applyAuth(req, bearerToken)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("expected HTTP 206, got %d", resp.StatusCode)
	}

	f, err := os.OpenFile(destPath, os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening file for writing: %w", err)
	}

	defer func() { _ = f.Close() }()

	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return fmt.Errorf("seeking to offset %d: %w", start, err)
	}

	if _, err := io.Copy(f, io.TeeReader(resp.Body, pw)); err != nil {
		return fmt.Errorf("writing chunk: %w", err)
	}

	return nil
}

// applyAuth sets bearer token authentication headers on a request.
func applyAuth(req *http.Request, bearerToken string) {
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
		req.Header.Set("Accept", "application/vnd.github+json")
	}
}

// progressLogger tracks total bytes downloaded across concurrent workers and
// logs progress periodically. Safe for concurrent use.
type progressLogger struct {
	log       logrus.FieldLogger
	total     int64
	written   atomic.Int64
	nextLogAt atomic.Int64
}

// newProgressLogger creates a progress logger for the given total size.
func newProgressLogger(log logrus.FieldLogger, total int64) *progressLogger {
	pl := &progressLogger{
		log:   log,
		total: total,
	}
	pl.nextLogAt.Store(progressLogInterval)

	return pl
}

// Written returns the total bytes written so far.
func (pl *progressLogger) Written() int64 {
	return pl.written.Load()
}

func (pl *progressLogger) Write(p []byte) (int, error) {
	n := len(p)
	current := pl.written.Add(int64(n))

	threshold := pl.nextLogAt.Load()
	if current >= threshold {
		// CAS to avoid duplicate log lines from concurrent writers.
		if pl.nextLogAt.CompareAndSwap(threshold, current+progressLogInterval) {
			fields := logrus.Fields{
				"downloaded": formatBytes(current),
			}

			if pl.total > 0 {
				pct := float64(current) / float64(pl.total) * 100
				fields["total"] = formatBytes(pl.total)
				fields["progress"] = fmt.Sprintf("%.0f%%", pct)
			}

			pl.log.WithFields(fields).Info("Downloading")
		}
	}

	return n, nil
}

// formatBytes returns a human-readable byte size string.
func formatBytes(b int64) string {
	const (
		mib = 1024 * 1024
		gib = 1024 * mib
	)

	switch {
	case b >= gib:
		return fmt.Sprintf("%.1f GiB", float64(b)/float64(gib))
	case b >= mib:
		return fmt.Sprintf("%.1f MiB", float64(b)/float64(mib))
	case b >= 1024:
		return fmt.Sprintf("%.1f KiB", float64(b)/1024)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// detectArchiveFormat determines the archive format from file extension,
// falling back to magic bytes detection.
func detectArchiveFormat(filePath string) (string, error) {
	lower := strings.ToLower(filePath)

	switch {
	case strings.HasSuffix(lower, ".zip"):
		return archiveFormatZip, nil
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return archiveFormatTarGz, nil
	}

	// Fall back to magic bytes.
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("opening file for format detection: %w", err)
	}

	defer func() { _ = f.Close() }()

	header := make([]byte, 4)

	n, err := f.Read(header)
	if err != nil {
		return "", fmt.Errorf("reading file header: %w", err)
	}

	if n < 2 {
		return "", fmt.Errorf("file too small to detect format")
	}

	// ZIP magic: PK\x03\x04
	if n >= 4 && header[0] == 0x50 && header[1] == 0x4B &&
		header[2] == 0x03 && header[3] == 0x04 {
		return archiveFormatZip, nil
	}

	// Gzip magic: \x1f\x8b
	if header[0] == 0x1F && header[1] == 0x8B {
		return archiveFormatTarGz, nil
	}

	return "", fmt.Errorf("unrecognized archive format for %s", filePath)
}
