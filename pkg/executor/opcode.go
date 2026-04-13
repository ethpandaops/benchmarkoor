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

// loadOpcodes resolves the opcode source file (local path or URL),
// reads the JSON map, and attaches opcode counts to prepared tests.
func (e *executor) loadOpcodes(ctx context.Context) error {
	file := e.cfg.OpcodeSource.File

	resolved, err := resolveFile(ctx, file, e.cfg.CacheDir, e.cfg.GitHubToken, e.log)
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

	e.log.WithFields(logrus.Fields{
		"file":          file,
		"total_entries": filtered,
		"matched_tests": matched,
		"total_tests":   len(e.prepared.Tests),
	}).Info("Loaded external opcode data")

	if unmatched := filtered - matched; unmatched > 0 {
		e.log.WithField("unmatched", unmatched).Warn(
			"Some opcode entries did not match any discovered test",
		)
	}

	return nil
}

// resolveFile resolves a file reference that can be a local path or HTTP(S)
// URL. Remote files are downloaded and cached in cacheDir.
func resolveFile(ctx context.Context, file, cacheDir, githubToken string, log logrus.FieldLogger) (string, error) {
	if strings.HasPrefix(file, "http://") || strings.HasPrefix(file, "https://") {
		hash := sha256.Sum256([]byte(file))
		name := "opcode-" + hex.EncodeToString(hash[:8])

		if cacheDir == "" {
			cacheDir = os.TempDir()
		}

		cachedPath := filepath.Join(cacheDir, name)

		if _, err := os.Stat(cachedPath); err == nil {
			log.WithFields(logrus.Fields{
				"url":  file,
				"path": cachedPath,
			}).Info("Using cached opcode file")

			return cachedPath, nil
		}

		log.WithField("url", file).Info("Downloading opcode file")

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

		tmpPath := cachedPath + ".tmp"

		if err := os.MkdirAll(filepath.Dir(cachedPath), 0755); err != nil {
			return "", fmt.Errorf("creating cache directory: %w", err)
		}

		if err := downloadToFile(ctx, downloadURL, tmpPath, token, log); err != nil {
			_ = os.Remove(tmpPath)

			return "", err
		}

		if err := os.Rename(tmpPath, cachedPath); err != nil {
			_ = os.Remove(tmpPath)

			return "", fmt.Errorf("caching file: %w", err)
		}

		return cachedPath, nil
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
