package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/sirupsen/logrus"
)

// ghArtifactURLPattern matches GitHub Actions artifact browser URLs:
// https://github.com/{owner}/{repo}/actions/runs/{run_id}/artifacts/{artifact_id}
var ghArtifactURLPattern = regexp.MustCompile(
	`^https://github\.com/([^/]+/[^/]+)/actions/runs/\d+/artifacts/(\d+)$`,
)

// ArchiveSource downloads and extracts an archive file, then discovers tests
// from the extracted contents using glob patterns.
type ArchiveSource struct {
	log         logrus.FieldLogger
	cfg         *config.ArchiveSourceConfig
	cacheDir    string
	filter      string
	githubToken string
	basePath    string // temp directory where archive was extracted
}

// Prepare downloads (if URL) and extracts the archive, then discovers tests.
func (s *ArchiveSource) Prepare(ctx context.Context) (*PreparedSource, error) {
	// Create temp directory for extraction.
	parentDir := s.cacheDir
	if parentDir == "" {
		parentDir = os.TempDir()
	}

	tmpDir, err := os.MkdirTemp(parentDir, "archive-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp directory: %w", err)
	}

	s.basePath = tmpDir

	// Determine the archive file path.
	archivePath, err := s.resolveFile(ctx)
	if err != nil {
		_ = os.RemoveAll(s.basePath)
		s.basePath = ""

		return nil, fmt.Errorf("resolving archive file: %w", err)
	}

	// Detect format and extract.
	if err := s.extractArchive(archivePath); err != nil {
		_ = os.RemoveAll(s.basePath)
		s.basePath = ""

		return nil, fmt.Errorf("extracting archive: %w", err)
	}

	s.log.WithField("path", s.basePath).Info("Extracted archive")

	return discoverTestsFromConfig(
		s.basePath, s.cfg.PreRunSteps, s.cfg.Steps, s.filter, s.log,
	)
}

// Cleanup removes the temporary extraction directory.
func (s *ArchiveSource) Cleanup() error {
	if s.basePath != "" {
		return os.RemoveAll(s.basePath)
	}

	return nil
}

// GetSourceInfo returns source information for the suite summary.
func (s *ArchiveSource) GetSourceInfo() (*SuiteSource, error) {
	info := &ArchiveSourceInfo{
		File:        s.cfg.File,
		PreRunSteps: s.cfg.PreRunSteps,
	}

	if s.cfg.Steps != nil {
		info.Steps = &SourceStepsGlobs{
			Setup:   s.cfg.Steps.Setup,
			Test:    s.cfg.Steps.Test,
			Cleanup: s.cfg.Steps.Cleanup,
		}
	}

	return &SuiteSource{Archive: info}, nil
}

// resolveFile returns the local path to the archive file. For URLs, it checks
// the cache directory first and only downloads if the file is not already cached.
func (s *ArchiveSource) resolveFile(ctx context.Context) (string, error) {
	file := s.cfg.File

	if strings.HasPrefix(file, "http://") || strings.HasPrefix(file, "https://") {
		cachedPath := s.cachedArchivePath()

		if _, err := os.Stat(cachedPath); err == nil {
			s.log.WithFields(logrus.Fields{
				"url":  file,
				"path": cachedPath,
			}).Info("Using cached archive")

			return cachedPath, nil
		}

		s.log.WithField("url", file).Info("Downloading archive")

		downloadURL, token := s.resolveDownloadURL(file)

		// Download to a temp file first, then rename for atomic cache writes.
		tmpPath := cachedPath + ".tmp"

		if err := os.MkdirAll(filepath.Dir(cachedPath), 0755); err != nil {
			return "", fmt.Errorf("creating cache directory: %w", err)
		}

		if err := downloadToFile(ctx, downloadURL, tmpPath, token, s.log); err != nil {
			_ = os.Remove(tmpPath)

			return "", err
		}

		if err := os.Rename(tmpPath, cachedPath); err != nil {
			_ = os.Remove(tmpPath)

			return "", fmt.Errorf("caching archive: %w", err)
		}

		s.log.WithField("path", cachedPath).Info("Archive cached")

		return cachedPath, nil
	}

	// Local file path — resolve relative paths.
	if !filepath.IsAbs(file) {
		absPath, err := filepath.Abs(file)
		if err != nil {
			return "", fmt.Errorf("resolving path %q: %w", file, err)
		}

		file = absPath
	}

	if _, err := os.Stat(file); os.IsNotExist(err) {
		return "", fmt.Errorf("archive file %q does not exist", file)
	}

	return file, nil
}

// cachedArchivePath returns a stable file path in the cache directory derived
// from the configured URL, so repeated runs reuse the same downloaded file.
func (s *ArchiveSource) cachedArchivePath() string {
	hash := sha256.Sum256([]byte(s.cfg.File))
	name := "archive-" + hex.EncodeToString(hash[:8])

	cacheDir := s.cacheDir
	if cacheDir == "" {
		cacheDir = os.TempDir()
	}

	return filepath.Join(cacheDir, name)
}

// resolveDownloadURL converts browser URLs to API URLs where needed and returns
// the appropriate auth token. For GitHub Actions artifact URLs, it converts to
// the GitHub API download endpoint with bearer token auth.
func (s *ArchiveSource) resolveDownloadURL(rawURL string) (string, string) {
	matches := ghArtifactURLPattern.FindStringSubmatch(rawURL)
	if matches != nil {
		repo := matches[1]
		artifactID := matches[2]
		apiURL := fmt.Sprintf(
			"https://api.github.com/repos/%s/actions/artifacts/%s/zip",
			repo, artifactID,
		)

		s.log.WithFields(logrus.Fields{
			"repo":        repo,
			"artifact_id": artifactID,
		}).Info("Detected GitHub artifact URL, using API endpoint")

		if s.githubToken == "" {
			s.log.Warn(
				"GitHub token is required for artifact downloads. " +
					"Set runner.github_token in config or " +
					"BENCHMARKOOR_RUNNER_GITHUB_TOKEN env var",
			)
		}

		return apiURL, s.githubToken
	}

	return rawURL, ""
}

// extractArchive detects the archive format and extracts it to the base path.
// For ZIP archives, inner tarballs are also extracted automatically.
func (s *ArchiveSource) extractArchive(archivePath string) error {
	format, err := detectArchiveFormat(archivePath)
	if err != nil {
		return err
	}

	switch format {
	case archiveFormatZip:
		if err := extractZipFile(archivePath, s.basePath); err != nil {
			return fmt.Errorf("extracting zip: %w", err)
		}

		// Auto-extract inner tarballs (e.g. GitHub Actions artifacts).
		if err := extractInnerTarballs(s.basePath, s.log); err != nil {
			return fmt.Errorf("extracting inner tarballs: %w", err)
		}
	case archiveFormatTarGz:
		if err := extractTarGzFile(archivePath, s.basePath); err != nil {
			return fmt.Errorf("extracting tar.gz: %w", err)
		}
	default:
		return fmt.Errorf("unsupported archive format: %s", format)
	}

	return nil
}
