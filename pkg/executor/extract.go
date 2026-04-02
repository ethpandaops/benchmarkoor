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

// downloadToFile downloads a URL to a local file path using plain HTTP with
// redirect following. If bearerToken is non-empty, it is sent as an
// Authorization header.
func downloadToFile(ctx context.Context, url, destPath, bearerToken string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
		req.Header.Set("Accept", "application/vnd.github+json")
	}

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

	if _, err := io.Copy(out, resp.Body); err != nil {
		_ = out.Close()
		_ = os.Remove(destPath)

		return fmt.Errorf("writing file %s: %w", destPath, err)
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("closing file %s: %w", destPath, err)
	}

	return nil
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
