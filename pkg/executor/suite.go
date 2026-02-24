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
	"github.com/ethpandaops/benchmarkoor/pkg/eest"
	"github.com/ethpandaops/benchmarkoor/pkg/fsutil"
)

// SuiteInfo contains information about a test suite.
type SuiteInfo struct {
	Hash        string                 `json:"hash"`
	Source      *SuiteSource           `json:"source"`
	Filter      string                 `json:"filter,omitempty"`
	Metadata    *config.MetadataConfig `json:"metadata,omitempty"`
	PreRunSteps []SuiteFile            `json:"pre_run_steps,omitempty"`
	Tests       []SuiteTest            `json:"tests"`
}

// SuiteSource contains source information for the suite.
type SuiteSource struct {
	Git   *GitSourceInfo   `json:"git,omitempty"`
	Local *LocalSourceInfo `json:"local,omitempty"`
	EEST  *EESTSourceInfo  `json:"eest,omitempty"`
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

// SuiteTestEEST contains EEST-specific metadata for a test.
type SuiteTestEEST struct {
	Info *eest.FixtureInfo `json:"info"`
}

// SuiteTest represents a test with its optional steps in the suite output.
type SuiteTest struct {
	Name        string         `json:"name"`
	GenesisHash string         `json:"genesis,omitempty"`
	Setup       *SuiteFile     `json:"setup,omitempty"`
	Test        *SuiteFile     `json:"test,omitempty"`
	Cleanup     *SuiteFile     `json:"cleanup,omitempty"`
	EEST        *SuiteTestEEST `json:"eest,omitempty"`
}

// ComputeSuiteHash computes a hash of all test file contents.
func ComputeSuiteHash(prepared *PreparedSource) (string, error) {
	h := sha256.New()

	// Hash pre-run steps first.
	for _, f := range prepared.PreRunSteps {
		content, err := getStepContent(f)
		if err != nil {
			return "", fmt.Errorf("reading pre-run step %s: %w", f.Name, err)
		}

		h.Write(content)
	}

	// Hash all test step files.
	for _, test := range prepared.Tests {
		if test.Setup != nil {
			content, err := getStepContent(test.Setup)
			if err != nil {
				return "", fmt.Errorf("reading setup file %s: %w", test.Setup.Name, err)
			}

			h.Write(content)
		}

		if test.Test != nil {
			content, err := getStepContent(test.Test)
			if err != nil {
				return "", fmt.Errorf("reading test file %s: %w", test.Test.Name, err)
			}

			h.Write(content)
		}

		if test.Cleanup != nil {
			content, err := getStepContent(test.Cleanup)
			if err != nil {
				return "", fmt.Errorf("reading cleanup file %s: %w", test.Cleanup.Name, err)
			}

			h.Write(content)
		}
	}

	// Use first 16 characters of the hash.
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

// getStepContent returns the content of a step, either from provider or file.
func getStepContent(step *StepFile) ([]byte, error) {
	if step.Provider != nil {
		return step.Provider.Content(), nil
	}

	return os.ReadFile(step.Path)
}

// CreateSuiteOutput creates the suite directory structure with copied files and summary.
func CreateSuiteOutput(
	resultsDir, hash string,
	info *SuiteInfo,
	prepared *PreparedSource,
	owner *fsutil.OwnerConfig,
) error {
	suiteDir := filepath.Join(resultsDir, "suites", hash)

	suiteExists := false

	// Check if suite already exists.
	if _, err := os.Stat(suiteDir); err == nil {
		suiteExists = true
	}

	if !suiteExists {
		// Create suite directory.
		if err := fsutil.MkdirAll(suiteDir, 0755, owner); err != nil {
			return fmt.Errorf("creating suite dir: %w", err)
		}

		// Copy pre-run steps.
		// Structure: <suite_dir>/<step_name>/pre_run.request (same pattern as tests).
		for _, f := range prepared.PreRunSteps {
			suiteFile, err := copyPreRunStepFile(suiteDir, f, owner)
			if err != nil {
				return fmt.Errorf("copying pre-run step: %w", err)
			}

			info.PreRunSteps = append(info.PreRunSteps, *suiteFile)
		}

		// Copy test files and build SuiteTest entries.
		// New structure: <suite_dir>/<test_name>/{setup,test,cleanup}.request
		for _, test := range prepared.Tests {
			suiteTest := SuiteTest{
				Name:        test.Name,
				GenesisHash: test.GenesisHash,
			}

			if test.EESTInfo != nil {
				suiteTest.EEST = &SuiteTestEEST{Info: test.EESTInfo}
			}

			// Create test directory.
			testDir := filepath.Join(suiteDir, test.Name)
			if err := fsutil.MkdirAll(testDir, 0755, owner); err != nil {
				return fmt.Errorf("creating test dir for %s: %w", test.Name, err)
			}

			if test.Setup != nil {
				suiteFile, err := copyTestStepFile(testDir, "setup", test.Setup, owner)
				if err != nil {
					return fmt.Errorf("copying setup file: %w", err)
				}

				suiteTest.Setup = suiteFile
			}

			if test.Test != nil {
				suiteFile, err := copyTestStepFile(testDir, "test", test.Test, owner)
				if err != nil {
					return fmt.Errorf("copying test file: %w", err)
				}

				suiteTest.Test = suiteFile
			}

			if test.Cleanup != nil {
				suiteFile, err := copyTestStepFile(testDir, "cleanup", test.Cleanup, owner)
				if err != nil {
					return fmt.Errorf("copying cleanup file: %w", err)
				}

				suiteTest.Cleanup = suiteFile
			}

			info.Tests = append(info.Tests, suiteTest)
		}
	}

	// Always write summary.json — metadata (e.g. labels) can change between
	// runs without affecting the suite hash, so we update it every time.
	summaryPath := filepath.Join(suiteDir, "summary.json")

	// If the suite already existed, read the existing summary to preserve
	// test/step file references, then overlay the new info fields.
	if suiteExists {
		existingData, readErr := os.ReadFile(summaryPath)
		if readErr == nil {
			var existing SuiteInfo
			if jsonErr := json.Unmarshal(existingData, &existing); jsonErr == nil {
				info.PreRunSteps = existing.PreRunSteps
				info.Tests = existing.Tests
			}
		}
	}

	summaryData, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling summary: %w", err)
	}

	if err := fsutil.WriteFile(summaryPath, summaryData, 0644, owner); err != nil {
		return fmt.Errorf("writing summary: %w", err)
	}

	return nil
}

// copyTestStepFile copies a test step file to the test directory with a standardized name.
// Files are stored as <test_dir>/<step_type>.request (e.g., setup.request, test.request, cleanup.request).
func copyTestStepFile(testDir, stepType string, file *StepFile, owner *fsutil.OwnerConfig) (*SuiteFile, error) {
	dstPath := filepath.Join(testDir, stepType+".request")

	// Handle provider-based steps.
	if file.Provider != nil {
		if err := fsutil.WriteFile(dstPath, file.Provider.Content(), 0644, owner); err != nil {
			return nil, fmt.Errorf("writing content: %w", err)
		}

		return &SuiteFile{OgPath: file.Name}, nil
	}

	// Handle file-based steps.
	srcFile, err := os.Open(file.Path)
	if err != nil {
		return nil, fmt.Errorf("opening source: %w", err)
	}

	defer func() { _ = srcFile.Close() }()

	dstFile, err := fsutil.Create(dstPath, owner)
	if err != nil {
		return nil, fmt.Errorf("creating destination: %w", err)
	}

	defer func() { _ = dstFile.Close() }()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return nil, fmt.Errorf("copying content: %w", err)
	}

	return &SuiteFile{OgPath: file.Name}, nil
}

// copyPreRunStepFile copies a pre-run step file to the suite directory.
// Files are stored as <suite_dir>/<step_name>/pre_run.request (same pattern as tests).
func copyPreRunStepFile(suiteDir string, file *StepFile, owner *fsutil.OwnerConfig) (*SuiteFile, error) {
	// Create step directory using the step name (relative path).
	stepDir := filepath.Join(suiteDir, file.Name)
	if err := fsutil.MkdirAll(stepDir, 0755, owner); err != nil {
		return nil, fmt.Errorf("creating step dir: %w", err)
	}

	dstPath := filepath.Join(stepDir, "pre_run.request")

	// Handle provider-based steps.
	if file.Provider != nil {
		if err := fsutil.WriteFile(dstPath, file.Provider.Content(), 0644, owner); err != nil {
			return nil, fmt.Errorf("writing content: %w", err)
		}

		return &SuiteFile{OgPath: file.Name}, nil
	}

	// Handle file-based steps.
	srcFile, err := os.Open(file.Path)
	if err != nil {
		return nil, fmt.Errorf("opening source: %w", err)
	}

	defer func() { _ = srcFile.Close() }()

	dstFile, err := fsutil.Create(dstPath, owner)
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
