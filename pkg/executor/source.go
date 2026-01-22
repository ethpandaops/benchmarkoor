package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/sirupsen/logrus"
)

// StepType represents the type of step being executed.
type StepType string

const (
	StepTypeSetup   StepType = "setup"
	StepTypeTest    StepType = "test"
	StepTypeCleanup StepType = "cleanup"
	StepTypePreRun  StepType = "pre_run"
)

// StepFile represents a single step file.
type StepFile struct {
	Path string // Full absolute path
	Name string // Relative path from base
}

// TestWithSteps represents a test with its optional setup/test/cleanup steps.
type TestWithSteps struct {
	Name    string    // Common test name (e.g., "abc.txt")
	Setup   *StepFile // Optional setup step
	Test    *StepFile // Optional test step
	Cleanup *StepFile // Optional cleanup step
}

// PreparedSource contains the prepared test source with all discovered tests.
type PreparedSource struct {
	BasePath    string
	PreRunSteps []*StepFile
	Tests       []*TestWithSteps
}

// Source provides test files from local or git sources.
type Source interface {
	// Prepare ensures test files are available and returns the prepared source.
	Prepare(ctx context.Context) (*PreparedSource, error)
	// Cleanup removes any temporary resources.
	Cleanup() error
	// GetSourceInfo returns source information for the suite summary.
	GetSourceInfo() (*SuiteSource, error)
}

// NewSource creates a Source from the configuration.
func NewSource(log logrus.FieldLogger, cfg *config.SourceConfig, cacheDir string, filter string) Source {
	if cfg.Local != nil {
		return &LocalSource{
			log:    log.WithField("source", "local"),
			cfg:    cfg.Local,
			filter: filter,
		}
	}

	if cfg.Git != nil {
		return &GitSource{
			log:      log.WithField("source", "git"),
			cfg:      cfg.Git,
			cacheDir: cacheDir,
			filter:   filter,
		}
	}

	return nil
}

// LocalSource reads tests from a local directory.
type LocalSource struct {
	log      logrus.FieldLogger
	cfg      *config.LocalSourceV2
	filter   string
	basePath string
}

// Prepare validates that the local directory exists and discovers tests.
func (s *LocalSource) Prepare(_ context.Context) (*PreparedSource, error) {
	if _, err := os.Stat(s.cfg.BaseDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("base directory %q does not exist", s.cfg.BaseDir)
	}

	s.basePath = s.cfg.BaseDir
	s.log.WithField("path", s.basePath).Info("Using local test directory")

	return s.discoverTests()
}

// discoverTests discovers all tests from the local source.
func (s *LocalSource) discoverTests() (*PreparedSource, error) {
	return discoverTestsFromConfig(s.basePath, s.cfg.PreRunSteps, s.cfg.Steps, s.filter, s.log)
}

// Cleanup is a no-op for local sources.
func (s *LocalSource) Cleanup() error {
	return nil
}

// GetSourceInfo returns source information for the suite summary.
func (s *LocalSource) GetSourceInfo() (*SuiteSource, error) {
	local := &LocalSourceInfo{
		BaseDir:     s.basePath,
		PreRunSteps: s.cfg.PreRunSteps,
	}

	if s.cfg.Steps != nil {
		local.Steps = &SourceStepsGlobs{
			Setup:   s.cfg.Steps.Setup,
			Test:    s.cfg.Steps.Test,
			Cleanup: s.cfg.Steps.Cleanup,
		}
	}

	return &SuiteSource{Local: local}, nil
}

// GitSource clones/fetches from a git repository.
type GitSource struct {
	log      logrus.FieldLogger
	cfg      *config.GitSourceV2
	cacheDir string
	filter   string
	basePath string
}

// Prepare clones or updates the git repository and discovers tests.
func (s *GitSource) Prepare(ctx context.Context) (*PreparedSource, error) {
	basePath, err := s.prepareRepo(ctx)
	if err != nil {
		return nil, fmt.Errorf("preparing git repo: %w", err)
	}

	s.basePath = basePath

	return s.discoverTests()
}

// prepareRepo clones or updates the git repository.
func (s *GitSource) prepareRepo(ctx context.Context) (string, error) {
	repoHash := hashRepoURL(s.cfg.Repo)
	localPath := filepath.Join(s.cacheDir, repoHash)

	log := s.log.WithFields(logrus.Fields{
		"repo":    s.cfg.Repo,
		"version": s.cfg.Version,
		"path":    localPath,
	})

	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		log.Info("Cloning repository")

		if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
			return "", fmt.Errorf("creating cache directory: %w", err)
		}

		// Shallow clone with specific branch/tag.
		cmd := exec.CommandContext(ctx, "git", "clone",
			"--depth=1",
			"--branch", s.cfg.Version,
			"--single-branch",
			s.cfg.Repo, localPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("cloning repository: %w", err)
		}
	} else {
		log.Info("Updating cached repository")

		// Fetch the specific version.
		cmd := exec.CommandContext(ctx, "git", "-C", localPath, "fetch",
			"--depth=1", "origin", s.cfg.Version)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("fetching version: %w", err)
		}

		// Checkout FETCH_HEAD.
		cmd = exec.CommandContext(ctx, "git", "-C", localPath, "checkout", "FETCH_HEAD")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("checking out version: %w", err)
		}
	}

	return localPath, nil
}

// discoverTests discovers all tests from the git source.
func (s *GitSource) discoverTests() (*PreparedSource, error) {
	return discoverTestsFromConfig(s.basePath, s.cfg.PreRunSteps, s.cfg.Steps, s.filter, s.log)
}

// Cleanup is a no-op for git sources (we keep the cache).
func (s *GitSource) Cleanup() error {
	return nil
}

// GetSourceInfo returns source information for the suite summary.
func (s *GitSource) GetSourceInfo() (*SuiteSource, error) {
	sha, err := GetGitCommitSHA(s.basePath)
	if err != nil {
		return nil, fmt.Errorf("getting commit SHA: %w", err)
	}

	git := &GitSourceInfo{
		Repo:        s.cfg.Repo,
		Version:     s.cfg.Version,
		SHA:         sha,
		PreRunSteps: s.cfg.PreRunSteps,
	}

	if s.cfg.Steps != nil {
		git.Steps = &SourceStepsGlobs{
			Setup:   s.cfg.Steps.Setup,
			Test:    s.cfg.Steps.Test,
			Cleanup: s.cfg.Steps.Cleanup,
		}
	}

	return &SuiteSource{Git: git}, nil
}

// hashRepoURL creates a hash of the repository URL for caching.
func hashRepoURL(url string) string {
	hash := sha256.Sum256([]byte(url))

	return hex.EncodeToString(hash[:8])
}

// discoverTestsFromConfig discovers tests based on the configuration.
func discoverTestsFromConfig(
	basePath string,
	preRunStepPatterns []string,
	steps *config.StepsConfig,
	filter string,
	log logrus.FieldLogger,
) (*PreparedSource, error) {
	result := &PreparedSource{
		BasePath:    basePath,
		PreRunSteps: make([]*StepFile, 0),
		Tests:       make([]*TestWithSteps, 0),
	}

	// Discover pre-run steps in config order.
	// Patterns are processed in the order they appear in the config.
	// Within each pattern, filepath.Glob returns files in lexicographic order.
	for _, pattern := range preRunStepPatterns {
		files, _, err := expandGlobPattern(basePath, pattern, filter)
		if err != nil {
			return nil, fmt.Errorf("expanding pre_run_steps pattern %q: %w", pattern, err)
		}

		result.PreRunSteps = append(result.PreRunSteps, files...)
	}

	log.WithField("count", len(result.PreRunSteps)).Debug("Discovered pre-run steps")

	// If no steps config, return with just pre-run steps.
	if steps == nil {
		return result, nil
	}

	// Discover files for each step type.
	setupFiles, setupPrefixes, err := expandGlobPatterns(basePath, steps.Setup, filter)
	if err != nil {
		return nil, fmt.Errorf("expanding setup patterns: %w", err)
	}

	testFiles, testPrefixes, err := expandGlobPatterns(basePath, steps.Test, filter)
	if err != nil {
		return nil, fmt.Errorf("expanding test patterns: %w", err)
	}

	cleanupFiles, cleanupPrefixes, err := expandGlobPatterns(basePath, steps.Cleanup, filter)
	if err != nil {
		return nil, fmt.Errorf("expanding cleanup patterns: %w", err)
	}

	log.WithFields(logrus.Fields{
		"setup_files":   len(setupFiles),
		"test_files":    len(testFiles),
		"cleanup_files": len(cleanupFiles),
	}).Debug("Discovered step files")

	// Group files by matching key (relative path after stripping static prefix).
	result.Tests = groupTestsByFilename(
		setupFiles, setupPrefixes,
		testFiles, testPrefixes,
		cleanupFiles, cleanupPrefixes,
	)

	log.WithField("count", len(result.Tests)).Info("Discovered tests with steps")

	return result, nil
}

// expandGlobPatterns expands multiple glob patterns and returns unique files
// along with the collected static prefixes from all patterns.
func expandGlobPatterns(basePath string, patterns []string, filter string) ([]*StepFile, []string, error) {
	seen := make(map[string]struct{}, len(patterns)*10)
	result := make([]*StepFile, 0, len(patterns)*10)
	prefixes := make([]string, 0, len(patterns))

	for _, pattern := range patterns {
		files, staticPrefix, err := expandGlobPattern(basePath, pattern, filter)
		if err != nil {
			return nil, nil, err
		}

		if staticPrefix != "" {
			prefixes = append(prefixes, staticPrefix)
		}

		for _, f := range files {
			if _, ok := seen[f.Path]; !ok {
				seen[f.Path] = struct{}{}
				result = append(result, f)
			}
		}
	}

	return result, prefixes, nil
}

// expandGlobPattern expands a single glob pattern and returns matching files
// along with the static prefix extracted from the pattern.
func expandGlobPattern(basePath, pattern, filter string) ([]*StepFile, string, error) {
	fullPattern := filepath.Join(basePath, pattern)
	staticPrefix := extractStaticPrefix(pattern)

	matches, err := filepath.Glob(fullPattern)
	if err != nil {
		return nil, "", fmt.Errorf("invalid glob pattern %q: %w", pattern, err)
	}

	result := make([]*StepFile, 0, len(matches))

	for _, match := range matches {
		// Skip directories.
		info, err := os.Stat(match)
		if err != nil {
			continue
		}

		if info.IsDir() {
			continue
		}

		// Only include .txt files.
		if !strings.HasSuffix(match, ".txt") {
			continue
		}

		// Apply filter if provided.
		if filter != "" && !strings.Contains(match, filter) {
			continue
		}

		relPath, err := filepath.Rel(basePath, match)
		if err != nil {
			relPath = match
		}

		result = append(result, &StepFile{
			Path: match,
			Name: relPath,
		})
	}

	return result, staticPrefix, nil
}

// groupTestsByFilename groups step files by their matching key.
// The matching key is derived by stripping the static prefix from the file path,
// allowing files in different directories with the same relative path to be matched.
// For example: "stateful_tests/setup/001/abc.txt" with prefix "stateful_tests/setup/"
// produces key "001/abc.txt".
func groupTestsByFilename(
	setupFiles []*StepFile, setupPrefixes []string,
	testFiles []*StepFile, testPrefixes []string,
	cleanupFiles []*StepFile, cleanupPrefixes []string,
) []*TestWithSteps {
	// Build maps of matching key -> StepFile for each step type.
	setupByKey := make(map[string]*StepFile, len(setupFiles))
	for _, f := range setupFiles {
		key := findMatchingKey(f.Name, setupPrefixes)
		setupByKey[key] = f
	}

	testByKey := make(map[string]*StepFile, len(testFiles))
	for _, f := range testFiles {
		key := findMatchingKey(f.Name, testPrefixes)
		testByKey[key] = f
	}

	cleanupByKey := make(map[string]*StepFile, len(cleanupFiles))
	for _, f := range cleanupFiles {
		key := findMatchingKey(f.Name, cleanupPrefixes)
		cleanupByKey[key] = f
	}

	// Collect all unique matching keys.
	allKeys := make(map[string]struct{}, len(setupFiles)+len(testFiles)+len(cleanupFiles))
	for key := range setupByKey {
		allKeys[key] = struct{}{}
	}

	for key := range testByKey {
		allKeys[key] = struct{}{}
	}

	for key := range cleanupByKey {
		allKeys[key] = struct{}{}
	}

	// Create sorted list of keys.
	keys := make([]string, 0, len(allKeys))
	for key := range allKeys {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	// Build TestWithSteps for each unique matching key.
	tests := make([]*TestWithSteps, 0, len(keys))

	for _, key := range keys {
		test := &TestWithSteps{
			Name:    key,
			Setup:   setupByKey[key],
			Test:    testByKey[key],
			Cleanup: cleanupByKey[key],
		}
		tests = append(tests, test)
	}

	return tests
}

// extractStaticPrefix extracts the static prefix from a glob pattern.
// The static prefix is the path before the first wildcard character (*, ?, [).
// For example: "stateful_tests/setup/*/*" -> "stateful_tests/setup/"
func extractStaticPrefix(pattern string) string {
	for i, c := range pattern {
		if c == '*' || c == '?' || c == '[' {
			prefix := pattern[:i]
			lastSep := strings.LastIndex(prefix, string(filepath.Separator))

			if lastSep == -1 {
				return ""
			}

			return prefix[:lastSep+1]
		}
	}

	// No wildcard found, return directory portion with trailing separator.
	return filepath.Dir(pattern) + string(filepath.Separator)
}

// findMatchingKey extracts the matching key from a file path given static prefixes.
// It strips the first matching prefix to produce a key for matching files across step types.
// Falls back to filepath.Base() if no prefix matches.
func findMatchingKey(filePath string, prefixes []string) string {
	for _, prefix := range prefixes {
		if prefix != "" && strings.HasPrefix(filePath, prefix) {
			return filePath[len(prefix):]
		}
	}

	return filepath.Base(filePath)
}
