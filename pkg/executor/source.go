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

// Source provides test files from local or git sources.
type Source interface {
	// Prepare ensures test files are available and returns the paths.
	Prepare(ctx context.Context) (testsPath, warmupPath string, err error)
	// Cleanup removes any temporary resources.
	Cleanup() error
}

// TestFile represents a single test file.
type TestFile struct {
	Path     string
	Name     string
	IsWarmup bool
}

// NewSource creates a Source from the configuration.
func NewSource(log logrus.FieldLogger, cfg *config.SourceConfig, cacheDir string) Source {
	if cfg.TestsLocalDir != "" {
		return &LocalSource{
			log:       log.WithField("source", "local"),
			testsDir:  cfg.TestsLocalDir,
			warmupDir: cfg.WarmupTestsLocalDir,
		}
	}

	if cfg.TestsGit != nil {
		return &GitSource{
			log:       log.WithField("source", "git"),
			testsGit:  cfg.TestsGit,
			warmupGit: cfg.WarmupGit,
			cacheDir:  cacheDir,
		}
	}

	return nil
}

// LocalSource reads tests from a local directory.
type LocalSource struct {
	log       logrus.FieldLogger
	testsDir  string
	warmupDir string
}

// Prepare validates that the local directories exist and returns the paths.
func (s *LocalSource) Prepare(_ context.Context) (string, string, error) {
	if _, err := os.Stat(s.testsDir); os.IsNotExist(err) {
		return "", "", fmt.Errorf("tests directory %q does not exist", s.testsDir)
	}

	s.log.WithField("path", s.testsDir).Info("Using local test directory")

	warmupPath := ""
	if s.warmupDir != "" {
		if _, err := os.Stat(s.warmupDir); os.IsNotExist(err) {
			return "", "", fmt.Errorf("warmup directory %q does not exist", s.warmupDir)
		}

		warmupPath = s.warmupDir

		s.log.WithField("path", s.warmupDir).Info("Using local warmup directory")
	}

	return s.testsDir, warmupPath, nil
}

// Cleanup is a no-op for local sources.
func (s *LocalSource) Cleanup() error {
	return nil
}

// GitSource clones/fetches from a git repository.
type GitSource struct {
	log       logrus.FieldLogger
	testsGit  *config.GitSource
	warmupGit *config.GitSource
	cacheDir  string
}

// Prepare clones or updates the git repository and returns the test paths.
func (s *GitSource) Prepare(ctx context.Context) (string, string, error) {
	testsPath, err := s.prepareRepo(ctx, s.testsGit)
	if err != nil {
		return "", "", fmt.Errorf("preparing tests repo: %w", err)
	}

	warmupPath := ""
	if s.warmupGit != nil {
		warmupPath, err = s.prepareRepo(ctx, s.warmupGit)
		if err != nil {
			return "", "", fmt.Errorf("preparing warmup repo: %w", err)
		}
	}

	return testsPath, warmupPath, nil
}

// prepareRepo clones or updates a single git repository.
func (s *GitSource) prepareRepo(ctx context.Context, git *config.GitSource) (string, error) {
	repoHash := hashRepoURL(git.Repo)
	localPath := filepath.Join(s.cacheDir, repoHash)

	log := s.log.WithFields(logrus.Fields{
		"repo":    git.Repo,
		"version": git.Version,
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
			"--branch", git.Version,
			"--single-branch",
			git.Repo, localPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("cloning repository: %w", err)
		}
	} else {
		log.Info("Updating cached repository")

		// Fetch the specific version.
		cmd := exec.CommandContext(ctx, "git", "-C", localPath, "fetch",
			"--depth=1", "origin", git.Version)
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

	// Return the path to the specific directory within the repo.
	if git.Directory != "" {
		return filepath.Join(localPath, git.Directory), nil
	}

	return localPath, nil
}

// Cleanup is a no-op for git sources (we keep the cache).
func (s *GitSource) Cleanup() error {
	return nil
}

// hashRepoURL creates a hash of the repository URL for caching.
func hashRepoURL(url string) string {
	hash := sha256.Sum256([]byte(url))

	return hex.EncodeToString(hash[:8])
}

// DiscoverTests finds all test files in the given path, sorted by path.
func DiscoverTests(basePath, filter string, isWarmup bool) ([]TestFile, error) {
	if basePath == "" {
		return nil, nil
	}

	var tests []TestFile

	err := filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Only include .txt files.
		if !strings.HasSuffix(path, ".txt") {
			return nil
		}

		// Apply filter if provided.
		if filter != "" && !strings.Contains(path, filter) {
			return nil
		}

		relPath, err := filepath.Rel(basePath, path)
		if err != nil {
			relPath = path
		}

		tests = append(tests, TestFile{
			Path:     path,
			Name:     relPath,
			IsWarmup: isWarmup,
		})

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking directory: %w", err)
	}

	// Sort by path to ensure consistent ordering.
	sort.Slice(tests, func(i, j int) bool {
		return tests[i].Name < tests[j].Name
	})

	return tests, nil
}
