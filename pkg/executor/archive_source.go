package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
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
	// Create temp directory for extraction. MkdirTemp requires the parent to
	// exist, so ensure the cache dir is created (e.g. ~/.cache/benchmarkoor on
	// a fresh host).
	parentDir := s.cacheDir
	if parentDir == "" {
		parentDir = os.TempDir()
	}

	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return nil, fmt.Errorf("creating cache directory: %w", err)
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

	prepared, err := discoverTestsFromConfig(
		s.basePath, s.cfg.PreRunSteps, s.cfg.Steps, s.filter, s.log,
	)
	if err != nil {
		_ = os.RemoveAll(s.basePath)
		s.basePath = ""

		return nil, err
	}

	return prepared, nil
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
		Parts:       s.cfg.Parts,
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

// resolveFile returns the local path to the archive file. For URLs, it
// checks the cache directory first and validates any existing cached copy
// against the origin (via HEAD + ETag/Last-Modified) before reuse. If the
// remote has changed or has no cache entry yet, a fresh copy is downloaded.
// When the config uses `parts`, the parts are resolved (each with its own
// validation) and concatenated into a single cached file.
func (s *ArchiveSource) resolveFile(ctx context.Context) (string, error) {
	if len(s.cfg.Parts) > 0 {
		return s.resolvePartsFile(ctx)
	}

	file := s.cfg.File

	if strings.HasPrefix(file, "http://") || strings.HasPrefix(file, "https://") {
		downloadURL, token := s.resolveDownloadURL(file)

		res, err := fetchCached(ctx, s.log, file, downloadURL, token, s.cacheDir, "archive")
		if err != nil {
			return "", err
		}

		return res.Path, nil
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

// resolvePartsFile resolves each configured part (validating HTTP caches
// against the origin) and concatenates them into a single combined cache
// file. The combined file is rebuilt whenever any part was (re-)downloaded
// during resolution, so updating a part on the origin causes the combined
// archive to be regenerated on the next run.
func (s *ArchiveSource) resolvePartsFile(ctx context.Context) (string, error) {
	cacheDir := s.cacheDir
	if cacheDir == "" {
		cacheDir = os.TempDir()
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("creating cache directory: %w", err)
	}

	// Combined cache key is derived from the full ordered parts list so any
	// change to the list produces a fresh combined file.
	combined := sha256.Sum256([]byte(strings.Join(s.cfg.Parts, "\n")))
	combinedPath := filepath.Join(cacheDir, "archive-parts-"+hex.EncodeToString(combined[:8]))

	// Resolve each part to a local file path (validating URL caches).
	partPaths := make([]string, 0, len(s.cfg.Parts))

	anyPartChanged := false

	for i, part := range s.cfg.Parts {
		s.log.WithFields(logrus.Fields{
			"part":  i + 1,
			"total": len(s.cfg.Parts),
			"ref":   part,
		}).Info("Resolving archive part")

		partPath, changed, err := s.resolvePart(ctx, part, cacheDir)
		if err != nil {
			return "", fmt.Errorf("resolving part %d (%s): %w", i+1, part, err)
		}

		if changed {
			anyPartChanged = true
		}

		partPaths = append(partPaths, partPath)
	}

	// Reuse the combined file only when it exists AND no underlying part
	// was refreshed during this call.
	if !anyPartChanged {
		if _, err := os.Stat(combinedPath); err == nil {
			s.log.WithField("path", combinedPath).Info("Using cached combined archive")

			return combinedPath, nil
		}
	}

	// Concatenate all parts into a temporary file, then atomically rename.
	tmpPath := combinedPath + ".tmp"

	if err := concatFiles(tmpPath, partPaths); err != nil {
		_ = os.Remove(tmpPath)

		return "", fmt.Errorf("concatenating parts: %w", err)
	}

	if err := os.Rename(tmpPath, combinedPath); err != nil {
		_ = os.Remove(tmpPath)

		return "", fmt.Errorf("caching combined archive: %w", err)
	}

	s.log.WithField("path", combinedPath).Info("Combined archive parts")

	return combinedPath, nil
}

// resolvePart resolves a single part reference (URL or local path) to a
// local file path. Remote parts are cache-validated via fetchCached.
// Returns (path, changed, error), where `changed` is true when the call
// produced a fresh download; used by resolvePartsFile to know when the
// concatenated combined file needs to be rebuilt.
func (s *ArchiveSource) resolvePart(ctx context.Context, part, cacheDir string) (string, bool, error) {
	if strings.HasPrefix(part, "http://") || strings.HasPrefix(part, "https://") {
		downloadURL, token := s.resolveDownloadURL(part)

		res, err := fetchCached(ctx, s.log, part, downloadURL, token, cacheDir, "archive-part")
		if err != nil {
			return "", false, err
		}

		return res.Path, res.Changed, nil
	}

	// Local file path — resolve relative paths.
	if !filepath.IsAbs(part) {
		absPath, err := filepath.Abs(part)
		if err != nil {
			return "", false, fmt.Errorf("resolving path %q: %w", part, err)
		}

		part = absPath
	}

	if _, err := os.Stat(part); os.IsNotExist(err) {
		return "", false, fmt.Errorf("archive part %q does not exist", part)
	}

	return part, false, nil
}

// concatFiles concatenates src files (in order) into dst. dst is created (or
// truncated) and written through a streamed copy so memory usage stays flat.
func concatFiles(dst string, srcs []string) error {
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("creating %s: %w", dst, err)
	}
	defer out.Close() //nolint:errcheck

	for _, src := range srcs {
		in, err := os.Open(src) //nolint:gosec // trusted paths under our cache
		if err != nil {
			return fmt.Errorf("opening %s: %w", src, err)
		}

		if _, err := io.Copy(out, in); err != nil {
			_ = in.Close()

			return fmt.Errorf("copying %s: %w", src, err)
		}

		if err := in.Close(); err != nil {
			return fmt.Errorf("closing %s: %w", src, err)
		}
	}

	return out.Close()
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
