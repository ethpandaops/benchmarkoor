package executor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

// SuiteInfo contains information about a test suite.
type SuiteInfo struct {
	Hash   string       `json:"hash"`
	Source *SuiteSource `json:"source"`
	Filter string       `json:"filter,omitempty"`
	Warmup []SuiteFile  `json:"warmup,omitempty"`
	Tests  []SuiteFile  `json:"tests"`
}

// SuiteSource contains source information for tests and warmup.
type SuiteSource struct {
	Tests  *SourceInfo `json:"tests"`
	Warmup *SourceInfo `json:"warmup,omitempty"`
}

// SourceInfo describes where test files came from.
type SourceInfo struct {
	Git      *GitInfo `json:"git,omitempty"`
	LocalDir string   `json:"local_dir,omitempty"`
}

// GitInfo contains git repository information.
type GitInfo struct {
	Repo      string `json:"repo"`
	Version   string `json:"version"`
	Directory string `json:"directory,omitempty"`
	SHA       string `json:"sha"`
}

// SuiteFile represents a file in the suite output.
type SuiteFile struct {
	F string `json:"f"`           // filename
	D string `json:"d,omitempty"` // directory (omit if empty)
}

// ComputeSuiteHash computes a hash of all test file contents.
func ComputeSuiteHash(warmupFiles, testFiles []TestFile) (string, error) {
	h := sha256.New()

	// Process warmup files first, then test files.
	allFiles := make([]TestFile, 0, len(warmupFiles)+len(testFiles))
	allFiles = append(allFiles, warmupFiles...)
	allFiles = append(allFiles, testFiles...)

	for _, f := range allFiles {
		content, err := os.ReadFile(f.Path)
		if err != nil {
			return "", fmt.Errorf("reading file %s: %w", f.Path, err)
		}

		h.Write(content)
	}

	// Use first 16 characters of the hash.
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

// CreateSuiteOutput creates the suite directory structure with copied files and summary.
func CreateSuiteOutput(
	resultsDir, hash string,
	info *SuiteInfo,
	warmupFiles, testFiles []TestFile,
) error {
	suiteDir := filepath.Join(resultsDir, "suites", hash)

	// Check if suite already exists.
	if _, err := os.Stat(suiteDir); err == nil {
		// Suite already exists, skip creation.
		return nil
	}

	// Create suite directories.
	warmupDir := filepath.Join(suiteDir, "warmup")
	testsDir := filepath.Join(suiteDir, "tests")

	if len(warmupFiles) > 0 {
		if err := os.MkdirAll(warmupDir, 0755); err != nil {
			return fmt.Errorf("creating warmup dir: %w", err)
		}
	}

	if err := os.MkdirAll(testsDir, 0755); err != nil {
		return fmt.Errorf("creating tests dir: %w", err)
	}

	// Copy warmup files.
	for i := range warmupFiles {
		suiteFile, err := copyTestFile(warmupDir, &warmupFiles[i])
		if err != nil {
			return fmt.Errorf("copying warmup file: %w", err)
		}

		info.Warmup = append(info.Warmup, *suiteFile)
	}

	// Copy test files.
	for i := range testFiles {
		suiteFile, err := copyTestFile(testsDir, &testFiles[i])
		if err != nil {
			return fmt.Errorf("copying test file: %w", err)
		}

		info.Tests = append(info.Tests, *suiteFile)
	}

	// Write summary.json.
	summaryPath := filepath.Join(suiteDir, "summary.json")

	summaryData, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling summary: %w", err)
	}

	if err := os.WriteFile(summaryPath, summaryData, 0644); err != nil {
		return fmt.Errorf("writing summary: %w", err)
	}

	return nil
}

// copyTestFile copies a test file to the suite directory and returns its SuiteFile info.
func copyTestFile(baseDir string, file *TestFile) (*SuiteFile, error) {
	// Extract directory component from the relative name.
	dir := filepath.Dir(file.Name)
	filename := filepath.Base(file.Name)

	// Create subdirectory if needed.
	targetDir := baseDir
	if dir != "." && dir != "" {
		targetDir = filepath.Join(baseDir, dir)
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return nil, fmt.Errorf("creating subdir: %w", err)
		}
	}

	// Copy the file.
	srcFile, err := os.Open(file.Path)
	if err != nil {
		return nil, fmt.Errorf("opening source: %w", err)
	}

	defer func() { _ = srcFile.Close() }()

	dstPath := filepath.Join(targetDir, filename)

	dstFile, err := os.Create(dstPath)
	if err != nil {
		return nil, fmt.Errorf("creating destination: %w", err)
	}

	defer func() { _ = dstFile.Close() }()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return nil, fmt.Errorf("copying content: %w", err)
	}

	suiteFile := &SuiteFile{F: filename}
	if dir != "." && dir != "" {
		suiteFile.D = dir
	}

	return suiteFile, nil
}

// GetGitCommitSHA retrieves the current commit SHA from a git repository.
func GetGitCommitSHA(repoPath string) (string, error) {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD")
	output, err := cmd.Output()

	if err != nil {
		return "", fmt.Errorf("getting commit SHA: %w", err)
	}

	sha := string(output)
	// Remove trailing newline.
	if len(sha) > 0 && sha[len(sha)-1] == '\n' {
		sha = sha[:len(sha)-1]
	}

	return sha, nil
}

// GetLocalSourceInfo creates SourceInfo for a LocalSource.
func GetLocalSourceInfo(testsDir, warmupDir string) *SuiteSource {
	source := &SuiteSource{
		Tests: &SourceInfo{
			LocalDir: testsDir,
		},
	}

	if warmupDir != "" {
		source.Warmup = &SourceInfo{
			LocalDir: warmupDir,
		}
	}

	return source
}

// GetGitSourceInfo creates SourceInfo for a GitSource.
func GetGitSourceInfo(testsGit, warmupGit *config.GitSource, cacheDir string) (*SuiteSource, error) {
	testsRepoPath := filepath.Join(cacheDir, hashRepoURL(testsGit.Repo))

	testsSHA, err := GetGitCommitSHA(testsRepoPath)
	if err != nil {
		return nil, fmt.Errorf("getting tests commit SHA: %w", err)
	}

	source := &SuiteSource{
		Tests: &SourceInfo{
			Git: &GitInfo{
				Repo:      testsGit.Repo,
				Version:   testsGit.Version,
				Directory: testsGit.Directory,
				SHA:       testsSHA,
			},
		},
	}

	if warmupGit != nil {
		warmupRepoPath := filepath.Join(cacheDir, hashRepoURL(warmupGit.Repo))

		warmupSHA, err := GetGitCommitSHA(warmupRepoPath)
		if err != nil {
			return nil, fmt.Errorf("getting warmup commit SHA: %w", err)
		}

		source.Warmup = &SourceInfo{
			Git: &GitInfo{
				Repo:      warmupGit.Repo,
				Version:   warmupGit.Version,
				Directory: warmupGit.Directory,
				SHA:       warmupSHA,
			},
		}
	}

	return source, nil
}
