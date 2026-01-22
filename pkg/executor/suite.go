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
)

// SuiteInfo contains information about a test suite.
type SuiteInfo struct {
	Hash        string       `json:"hash"`
	Source      *SuiteSource `json:"source"`
	Filter      string       `json:"filter,omitempty"`
	PreRunSteps []SuiteFile  `json:"pre_run_steps,omitempty"`
	Tests       []SuiteTest  `json:"tests"`
}

// SuiteSource contains source information for the suite.
type SuiteSource struct {
	Git   *GitSourceInfo   `json:"git,omitempty"`
	Local *LocalSourceInfo `json:"local,omitempty"`
}

// GitSourceInfo contains git repository source information.
type GitSourceInfo struct {
	Repo        string            `json:"repo"`
	Version     string            `json:"version"`
	SHA         string            `json:"sha"`
	PreRunSteps []string          `json:"pre_run_steps,omitempty"`
	Steps       *SourceStepsGlobs `json:"steps,omitempty"`
}

// LocalSourceInfo contains local directory source information.
type LocalSourceInfo struct {
	BaseDir     string            `json:"base_dir"`
	PreRunSteps []string          `json:"pre_run_steps,omitempty"`
	Steps       *SourceStepsGlobs `json:"steps,omitempty"`
}

// SourceStepsGlobs contains the glob patterns used to discover test steps.
type SourceStepsGlobs struct {
	Setup   []string `json:"setup,omitempty"`
	Test    []string `json:"test,omitempty"`
	Cleanup []string `json:"cleanup,omitempty"`
}

// SuiteFile represents a file in the suite output.
type SuiteFile struct {
	OgPath string `json:"og_path"` // original relative path
}

// SuiteTest represents a test with its optional steps in the suite output.
type SuiteTest struct {
	Name    string     `json:"name"`
	Setup   *SuiteFile `json:"setup,omitempty"`
	Test    *SuiteFile `json:"test,omitempty"`
	Cleanup *SuiteFile `json:"cleanup,omitempty"`
}

// ComputeSuiteHash computes a hash of all test file contents.
func ComputeSuiteHash(prepared *PreparedSource) (string, error) {
	h := sha256.New()

	// Hash pre-run steps first.
	for _, f := range prepared.PreRunSteps {
		content, err := os.ReadFile(f.Path)
		if err != nil {
			return "", fmt.Errorf("reading pre-run step %s: %w", f.Path, err)
		}

		h.Write(content)
	}

	// Hash all test step files.
	for _, test := range prepared.Tests {
		if test.Setup != nil {
			content, err := os.ReadFile(test.Setup.Path)
			if err != nil {
				return "", fmt.Errorf("reading setup file %s: %w", test.Setup.Path, err)
			}

			h.Write(content)
		}

		if test.Test != nil {
			content, err := os.ReadFile(test.Test.Path)
			if err != nil {
				return "", fmt.Errorf("reading test file %s: %w", test.Test.Path, err)
			}

			h.Write(content)
		}

		if test.Cleanup != nil {
			content, err := os.ReadFile(test.Cleanup.Path)
			if err != nil {
				return "", fmt.Errorf("reading cleanup file %s: %w", test.Cleanup.Path, err)
			}

			h.Write(content)
		}
	}

	// Use first 16 characters of the hash.
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

// CreateSuiteOutput creates the suite directory structure with copied files and summary.
func CreateSuiteOutput(
	resultsDir, hash string,
	info *SuiteInfo,
	prepared *PreparedSource,
) error {
	suiteDir := filepath.Join(resultsDir, "suites", hash)

	// Check if suite already exists.
	if _, err := os.Stat(suiteDir); err == nil {
		// Suite already exists, skip creation.
		return nil
	}

	// Create suite directory.
	if err := os.MkdirAll(suiteDir, 0755); err != nil {
		return fmt.Errorf("creating suite dir: %w", err)
	}

	// Copy pre-run steps.
	if len(prepared.PreRunSteps) > 0 {
		preRunDir := filepath.Join(suiteDir, "pre_run_steps")
		if err := os.MkdirAll(preRunDir, 0755); err != nil {
			return fmt.Errorf("creating pre_run_steps dir: %w", err)
		}

		for _, f := range prepared.PreRunSteps {
			suiteFile, err := copyPreRunStepFile(preRunDir, f)
			if err != nil {
				return fmt.Errorf("copying pre-run step: %w", err)
			}

			info.PreRunSteps = append(info.PreRunSteps, *suiteFile)
		}
	}

	// Copy test files and build SuiteTest entries.
	// New structure: <suite_dir>/<test_name>/{setup,test,cleanup}.request
	for _, test := range prepared.Tests {
		suiteTest := SuiteTest{
			Name: test.Name,
		}

		// Create test directory.
		testDir := filepath.Join(suiteDir, test.Name)
		if err := os.MkdirAll(testDir, 0755); err != nil {
			return fmt.Errorf("creating test dir for %s: %w", test.Name, err)
		}

		if test.Setup != nil {
			suiteFile, err := copyTestStepFile(testDir, "setup", test.Setup)
			if err != nil {
				return fmt.Errorf("copying setup file: %w", err)
			}

			suiteTest.Setup = suiteFile
		}

		if test.Test != nil {
			suiteFile, err := copyTestStepFile(testDir, "test", test.Test)
			if err != nil {
				return fmt.Errorf("copying test file: %w", err)
			}

			suiteTest.Test = suiteFile
		}

		if test.Cleanup != nil {
			suiteFile, err := copyTestStepFile(testDir, "cleanup", test.Cleanup)
			if err != nil {
				return fmt.Errorf("copying cleanup file: %w", err)
			}

			suiteTest.Cleanup = suiteFile
		}

		info.Tests = append(info.Tests, suiteTest)
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

// copyTestStepFile copies a test step file to the test directory with a standardized name.
// Files are stored as <test_dir>/<step_type>.request (e.g., setup.request, test.request, cleanup.request).
func copyTestStepFile(testDir, stepType string, file *StepFile) (*SuiteFile, error) {
	srcFile, err := os.Open(file.Path)
	if err != nil {
		return nil, fmt.Errorf("opening source: %w", err)
	}

	defer func() { _ = srcFile.Close() }()

	dstPath := filepath.Join(testDir, stepType+".request")

	dstFile, err := os.Create(dstPath)
	if err != nil {
		return nil, fmt.Errorf("creating destination: %w", err)
	}

	defer func() { _ = dstFile.Close() }()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return nil, fmt.Errorf("copying content: %w", err)
	}

	return &SuiteFile{OgPath: file.Name}, nil
}

// copyPreRunStepFile copies a pre-run step file preserving its original directory structure.
// Files are stored as pre_run_steps/<original_path>.
func copyPreRunStepFile(preRunDir string, file *StepFile) (*SuiteFile, error) {
	// Extract directory component from the relative name.
	dir := filepath.Dir(file.Name)
	filename := filepath.Base(file.Name)

	// Create subdirectory if needed.
	targetDir := preRunDir
	if dir != "." && dir != "" {
		targetDir = filepath.Join(preRunDir, dir)
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return nil, fmt.Errorf("creating subdir: %w", err)
		}
	}

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

	return &SuiteFile{OgPath: file.Name}, nil
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
