package executor

import (
	"context"
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

// resolveFile returns the local path to the archive file, downloading it first
// if the configured file is a URL.
func (s *ArchiveSource) resolveFile(ctx context.Context) (string, error) {
	file := s.cfg.File

	if strings.HasPrefix(file, "http://") || strings.HasPrefix(file, "https://") {
		s.log.WithField("url", file).Info("Downloading archive")

		destPath := filepath.Join(s.basePath, "archive-download")

		downloadURL, token := s.resolveDownloadURL(file)

		if err := downloadToFile(ctx, downloadURL, destPath, token); err != nil {
			return "", err
		}

		return destPath, nil
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
