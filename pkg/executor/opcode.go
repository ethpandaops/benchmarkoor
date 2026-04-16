package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

// loadOpcodes resolves the opcode source file (local path, URL, or a file
// inside an archive), reads the JSON map, and attaches opcode counts to
// prepared tests.
func (e *executor) loadOpcodes(ctx context.Context) error {
	file := e.cfg.OpcodeSource.File
	archive := e.cfg.OpcodeSource.Archive

	var (
		resolved string
		err      error
	)

	if archive != "" {
		if file == "" {
			return fmt.Errorf("opcode_source.file (filename inside the archive) is required when opcode_source.archive is set")
		}

		resolved, err = resolveOpcodeFromArchive(ctx, archive, file, e.cfg.CacheDir, e.cfg.GitHubToken, e.log)
	} else {
		resolved, err = resolveFile(ctx, file, e.cfg.CacheDir, e.cfg.GitHubToken, e.log)
	}

	if err != nil {
		return fmt.Errorf("resolving opcode file: %w", err)
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return fmt.Errorf("reading opcode file %q: %w", resolved, err)
	}

	var opcodeMap map[string]map[string]int
	if err := json.Unmarshal(data, &opcodeMap); err != nil {
		return fmt.Errorf("parsing opcode file: %w", err)
	}

	// Match opcode data to discovered tests by name.
	// Test names from glob discovery include the .txt extension, but opcode
	// JSON keys typically omit it, so we try both the full name and without .txt.
	matched := 0

	for _, test := range e.prepared.Tests {
		name := test.Name
		if counts, ok := opcodeMap[name]; ok {
			test.OpcodeCount = counts
			matched++
		} else if trimmed, hasSuffix := strings.CutSuffix(name, ".txt"); hasSuffix {
			if counts, ok := opcodeMap[trimmed]; ok {
				test.OpcodeCount = counts
				matched++
			}
		}
	}

	// Count opcode entries that are relevant (pass the filter) but didn't match a test.
	filtered := len(opcodeMap)
	if e.cfg.Filter != "" {
		filtered = 0

		for key := range opcodeMap {
			if strings.Contains(key, e.cfg.Filter) {
				filtered++
			}
		}
	}

	logFields := logrus.Fields{
		"file":          file,
		"total_entries": filtered,
		"matched_tests": matched,
		"total_tests":   len(e.prepared.Tests),
	}
	if archive != "" {
		logFields["archive"] = archive
	}

	e.log.WithFields(logFields).Info("Loaded external opcode data")

	if unmatched := filtered - matched; unmatched > 0 {
		e.log.WithField("unmatched", unmatched).Warn(
			"Some opcode entries did not match any discovered test",
		)
	}

	return nil
}

// resolveFile resolves a file reference that can be a local path or HTTP(S)
// URL. Remote files are cache-validated (via HEAD + ETag/Last-Modified)
// before being reused; if the origin has changed, a fresh copy is
// downloaded.
func resolveFile(ctx context.Context, file, cacheDir, githubToken string, log logrus.FieldLogger) (string, error) {
	if strings.HasPrefix(file, "http://") || strings.HasPrefix(file, "https://") {
		downloadURL := file

		var token string

		if ghArtifactURLPattern.MatchString(file) && githubToken != "" {
			m := ghArtifactURLPattern.FindStringSubmatch(file)
			downloadURL = fmt.Sprintf(
				"https://api.github.com/repos/%s/actions/artifacts/%s/zip",
				m[1], m[2],
			)
			token = githubToken
		}

		res, err := fetchCached(ctx, log, file, downloadURL, token, cacheDir, "opcode")
		if err != nil {
			return "", err
		}

		return res.Path, nil
	}

	// Local file path.
	if !filepath.IsAbs(file) {
		absPath, err := filepath.Abs(file)
		if err != nil {
			return "", fmt.Errorf("resolving path %q: %w", file, err)
		}

		file = absPath
	}

	if _, err := os.Stat(file); os.IsNotExist(err) {
		return "", fmt.Errorf("file %q does not exist", file)
	}

	return file, nil
}

// resolveOpcodeFromArchive downloads (with cache validation) an archive
// containing the opcode JSON file and returns the path to the extracted
// file within it. Supports local paths, HTTP(S) URLs, and GitHub Actions
// artifact browser URLs.
func resolveOpcodeFromArchive(
	ctx context.Context,
	archive, file, cacheDir, githubToken string,
	log logrus.FieldLogger,
) (string, error) {
	// Step 1 — get a local path to the archive.
	archivePath, changed, err := resolveArchiveRef(ctx, archive, cacheDir, githubToken, log)
	if err != nil {
		return "", fmt.Errorf("resolving opcode archive: %w", err)
	}

	// Step 2 — extract into a stable, per-archive cache dir. Re-extract
	// whenever the underlying archive was (re-)downloaded.
	extractDir, err := extractOpcodeArchive(archivePath, cacheDir, changed, log)
	if err != nil {
		return "", fmt.Errorf("extracting opcode archive: %w", err)
	}

	// Step 3 — find the requested file by basename under the extraction
	// root, then fall back to an exact relative path if that didn't match.
	found, err := findFileInDir(extractDir, file)
	if err != nil {
		return "", fmt.Errorf("opcode file %q not found in archive: %w", file, err)
	}

	return found, nil
}

// resolveArchiveRef resolves an archive reference (local path or URL) to a
// local file path. The `changed` return value is true when the call caused
// a fresh download.
func resolveArchiveRef(
	ctx context.Context,
	ref, cacheDir, githubToken string,
	log logrus.FieldLogger,
) (string, bool, error) {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		downloadURL := ref

		var token string

		if ghArtifactURLPattern.MatchString(ref) && githubToken != "" {
			m := ghArtifactURLPattern.FindStringSubmatch(ref)
			downloadURL = fmt.Sprintf(
				"https://api.github.com/repos/%s/actions/artifacts/%s/zip",
				m[1], m[2],
			)
			token = githubToken
		}

		res, err := fetchCached(ctx, log, ref, downloadURL, token, cacheDir, "opcode-archive")
		if err != nil {
			return "", false, err
		}

		return res.Path, res.Changed, nil
	}

	// Local file path.
	if !filepath.IsAbs(ref) {
		absPath, err := filepath.Abs(ref)
		if err != nil {
			return "", false, fmt.Errorf("resolving path %q: %w", ref, err)
		}

		ref = absPath
	}

	if _, err := os.Stat(ref); os.IsNotExist(err) {
		return "", false, fmt.Errorf("archive %q does not exist", ref)
	}

	return ref, false, nil
}

// extractOpcodeArchive extracts archivePath into a stable cache directory
// keyed by sha256(archivePath). The extraction is reused on subsequent
// runs unless `forceRefresh` is true (set when fetchCached reported the
// archive was re-downloaded). GitHub Actions artifact zips that contain
// an inner tarball are auto-extracted as well.
func extractOpcodeArchive(archivePath, cacheDir string, forceRefresh bool, log logrus.FieldLogger) (string, error) {
	if cacheDir == "" {
		cacheDir = os.TempDir()
	}

	hash := sha256.Sum256([]byte(archivePath))
	extractDir := filepath.Join(cacheDir, "opcode-archive-extract-"+hex.EncodeToString(hash[:8]))

	if forceRefresh {
		_ = os.RemoveAll(extractDir)
	}

	if _, err := os.Stat(extractDir); err == nil {
		log.WithField("path", extractDir).Info("Using cached opcode archive extraction")
		return extractDir, nil
	}

	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return "", fmt.Errorf("creating extraction directory: %w", err)
	}

	format, err := detectArchiveFormat(archivePath)
	if err != nil {
		return "", err
	}

	switch format {
	case archiveFormatZip:
		if err := extractZipFile(archivePath, extractDir); err != nil {
			return "", fmt.Errorf("extracting zip: %w", err)
		}
		// GitHub Actions artifacts are zips containing an inner tar.gz;
		// unpack those too so findFileInDir can see the actual JSON.
		if err := extractInnerTarballs(extractDir, log); err != nil {
			return "", fmt.Errorf("extracting inner tarballs: %w", err)
		}
	case archiveFormatTarGz:
		if err := extractTarGzFile(archivePath, extractDir); err != nil {
			return "", fmt.Errorf("extracting tar.gz: %w", err)
		}
	default:
		return "", fmt.Errorf("unsupported archive format: %s", format)
	}

	log.WithField("path", extractDir).Info("Extracted opcode archive")

	return extractDir, nil
}

// findFileInDir walks dir recursively and returns the absolute path of the
// first file whose basename matches name. If name contains a path
// separator, it is treated as a relative path from dir and matched exactly.
func findFileInDir(dir, name string) (string, error) {
	// Exact relative match first.
	if strings.ContainsAny(name, "/\\") {
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	var found string

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if info.IsDir() {
			return nil
		}

		if filepath.Base(path) == filepath.Base(name) {
			found = path

			return filepath.SkipAll
		}

		return nil
	})
	if err != nil {
		return "", err
	}

	if found == "" {
		return "", fmt.Errorf("%q not found under %q", name, dir)
	}

	return found, nil
}
