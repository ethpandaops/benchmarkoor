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
	"github.com/sirupsen/logrus"
)

// SuiteInfo contains information about a test suite.
type SuiteInfo struct {
	Hash        string                 `json:"hash"`
	Source      *SuiteSource           `json:"source"`
	Filter      string                 `json:"filter,omitempty"`
	Metadata    *config.MetadataConfig `json:"metadata,omitempty"`
	PreRunSteps []SuiteFile            `json:"pre_run_steps,omitempty"`
	Tests       []SuiteTest            `json:"tests"`
	// EESTMetadata is true when the suite output contains an .eest-meta
	// directory copied from the EEST fixtures' .meta (fill provenance). Lets the
	// UI surface an "EEST Metadata" view without statting the suite directory.
	EESTMetadata bool `json:"eest_metadata,omitempty"`
}

// SuiteSource contains source information for the suite.
type SuiteSource struct {
	Git     *GitSourceInfo     `json:"git,omitempty"`
	Local   *LocalSourceInfo   `json:"local,omitempty"`
	Archive *ArchiveSourceInfo `json:"archive,omitempty"`
	EEST    *EESTSourceInfo    `json:"eest,omitempty"`
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

// ArchiveSourceInfo contains archive file source information.
type ArchiveSourceInfo struct {
	File        string            `json:"file,omitempty"`
	Parts       []string          `json:"parts,omitempty"`
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
	// SizeBytes is the size of the source file, recorded even when the file
	// itself was left out of the suite.
	SizeBytes int64 `json:"size_bytes,omitempty"`
	// Omitted marks a step whose payload was too large to keep in the suite
	// directory, so it was never uploaded. The UI must not offer it for
	// viewing; the bytes live in the fixtures artifact the step came from.
	Omitted bool `json:"omitted,omitempty"`
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
	OpcodeCount map[string]int `json:"opcode_count,omitempty"`

	// Engine payload sizes per step — computed once per suite, same across
	// clients. Each populated step exposes per-newPayload byte counts as
	// {raw, bal, snappy} arrays (one element per engine_newPayload* line in
	// step order). Steps with no newPayload activity are omitted.
	PayloadSizes *PayloadSizes `json:"payload_sizes,omitempty"`

	// Engine newPayload transaction counts per step — one element per
	// engine_newPayload* line in step order, equal to len(payload.transactions).
	// Steps with no newPayload activity are omitted.
	TxCounts *TxCounts `json:"tx_counts,omitempty"`
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
// maxPreRunStepSize caps the pre-run payloads kept in the suite; see
// config.TestsConfig.MaxPreRunStepSize. Zero or less keeps every one.
func CreateSuiteOutput(
	log logrus.FieldLogger,
	resultsDir, hash string,
	info *SuiteInfo,
	prepared *PreparedSource,
	owner *fsutil.OwnerConfig,
	maxPreRunStepSize int64,
) error {
	suiteDir := filepath.Join(resultsDir, "suites", hash)

	suiteExists := false

	// Treat the suite as already built only when a complete summary.json with
	// tests is present. A bare or partial directory — e.g. left behind by a run
	// that aborted mid-creation — must be rebuilt; otherwise info.Tests stays
	// nil and summary.json gets (re)written with "tests": null.
	if data, err := os.ReadFile(filepath.Join(suiteDir, "summary.json")); err == nil {
		var existing SuiteInfo
		if json.Unmarshal(data, &existing) == nil && len(existing.Tests) > 0 {
			suiteExists = true
		}
	}

	if !suiteExists {
		// Create suite directory.
		if err := fsutil.MkdirAll(suiteDir, 0755, owner); err != nil {
			return fmt.Errorf("creating suite dir: %w", err)
		}

		// Attach the source's metadata directory (EEST .meta) as .eest-meta.
		// Best-effort: the metadata is auxiliary provenance, so a copy failure
		// must not fail the whole suite.
		if prepared.MetaDir != "" {
			metaDst := filepath.Join(suiteDir, ".eest-meta")
			if err := fsutil.CopyDir(prepared.MetaDir, metaDst, owner); err != nil {
				log.WithError(err).WithField("src", prepared.MetaDir).
					Warn("Failed to copy EEST .meta into suite output")
			} else {
				log.WithField("dst", metaDst).Debug("Copied EEST .meta into suite output")
			}
		}

		// Copy pre-run steps.
		// Structure: <suite_dir>/<step_name>/pre_run.request (same pattern as tests).
		for _, f := range prepared.PreRunSteps {
			suiteFile, err := copyPreRunStepFile(log, suiteDir, f, owner, maxPreRunStepSize)
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
				suiteTest.OpcodeCount = test.EESTInfo.AggregatedOpcodeCount()
			}

			// External opcode data takes precedence over EEST-derived opcodes.
			if test.OpcodeCount != nil {
				suiteTest.OpcodeCount = test.OpcodeCount
			}

			// Create test directory.
			testDir := filepath.Join(suiteDir, sanitizeResultPath(test.Name))
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

			// Compute per-newPayload payload sizes for each step that has any.
			// stepLinesForTest reads from the SOURCE step (Provider in-memory
			// for EEST, or the source file path for file-backed sources) —
			// not from the just-copied <suiteDir>/<test>/<step>.request. Both
			// contain the same bytes; reading the source avoids depending on
			// the copy order.
			psLog := log.WithField("component", "suite-payload-sizes")
			steps := []struct {
				kind StepKind
				file *StepFile
			}{
				{StepKindSetup, test.Setup},
				{StepKindTest, test.Test},
				{StepKindCleanup, test.Cleanup},
			}
			var ps PayloadSizes
			var tc TxCounts
			anyData := false
			anyTxData := false
			for _, s := range steps {
				if s.file == nil {
					continue
				}
				lines := stepLinesForTest(psLog, s.file)
				buckets := ComputePayloadSizeBuckets(psLog, test.Name, lines)
				if buckets.HasData() {
					b := buckets
					switch s.kind {
					case StepKindSetup:
						ps.Setup = &b
					case StepKindTest:
						ps.Test = &b
					case StepKindCleanup:
						ps.Cleanup = &b
					}
					anyData = true
				}
				if counts := ComputeTxCountsForStep(lines); len(counts) > 0 {
					switch s.kind {
					case StepKindSetup:
						tc.Setup = counts
					case StepKindTest:
						tc.Test = counts
					case StepKindCleanup:
						tc.Cleanup = counts
					}
					anyTxData = true
				}
			}
			if anyData {
				p := ps
				suiteTest.PayloadSizes = &p
			}
			if anyTxData {
				t := tc
				suiteTest.TxCounts = &t
			}

			info.Tests = append(info.Tests, suiteTest)
		}
	}

	// Reflect whether the suite carries an .eest-meta directory. Derived from
	// the on-disk dir (not just this run's copy) so it stays correct on re-runs
	// where the suite already existed and the copy step above was skipped.
	if fi, err := os.Stat(filepath.Join(suiteDir, ".eest-meta")); err == nil && fi.IsDir() {
		info.EESTMetadata = true
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

				// Merge opcode data from prepared tests into existing entries.
				mergeOpcodeData(existing.Tests, prepared)

				lineProvider := func(testName string, step StepKind) []string {
					reqPath := filepath.Join(suiteDir, sanitizeResultPath(testName), string(step)+".request")
					data, err := os.ReadFile(reqPath)
					if err != nil {
						// Missing files are normal — most tests don't have setup/cleanup.
						// Only warn for the test step where absence is genuinely unexpected.
						if step == StepKindTest && !os.IsNotExist(err) {
							log.WithError(err).WithField("path", reqPath).Warn("Failed to read test.request for payload-size merge")
						}
						return nil
					}
					return splitNonEmptyLines(string(data))
				}

				MergePayloadSizes(log, existing.Tests, lineProvider)
				MergeTxCounts(log, existing.Tests, lineProvider)

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
// A file over maxSize is described in the summary but not copied, so it never
// reaches the bucket either; maxSize <= 0 disables the limit.
func copyPreRunStepFile(
	log logrus.FieldLogger,
	suiteDir string,
	file *StepFile,
	owner *fsutil.OwnerConfig,
	maxSize int64,
) (*SuiteFile, error) {
	size, err := stepSize(file)
	if err != nil {
		return nil, err
	}

	// Checked before anything is created, so an oversized bundle costs neither
	// the local write nor the upload — and leaves no empty directory behind to
	// suggest a file that was never stored.
	if maxSize > 0 && size > maxSize {
		log.WithFields(logrus.Fields{
			"step":  file.Name,
			"bytes": size,
			"max":   maxSize,
		}).Info("Pre-run bundle over the size limit; describing it in the suite without storing it")

		return &SuiteFile{OgPath: file.Name, SizeBytes: size, Omitted: true}, nil
	}

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

		return &SuiteFile{OgPath: file.Name, SizeBytes: size}, nil
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

	return &SuiteFile{OgPath: file.Name, SizeBytes: size}, nil
}

// stepSize reports a step's payload size without reading a file-backed one.
func stepSize(file *StepFile) (int64, error) {
	if file.Provider != nil {
		return int64(len(file.Provider.Content())), nil
	}

	stat, err := os.Stat(file.Path)
	if err != nil {
		return 0, fmt.Errorf("stating source: %w", err)
	}

	return stat.Size(), nil
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

// mergeOpcodeData updates existing suite tests with opcode data from
// the current prepared source. This is needed when the suite directory
// already exists on disk and we re-read its summary.json — the old
// entries won't have opcode_count if the feature was added after the
// suite was first created.
func mergeOpcodeData(existing []SuiteTest, prepared *PreparedSource) {
	if prepared == nil {
		return
	}

	opcodeByName := make(map[string]map[string]int, len(prepared.Tests))

	for _, t := range prepared.Tests {
		if t.OpcodeCount != nil {
			opcodeByName[t.Name] = t.OpcodeCount
		} else if counts := t.EESTInfo.AggregatedOpcodeCount(); counts != nil {
			opcodeByName[t.Name] = counts
		}
	}

	for i := range existing {
		if counts, ok := opcodeByName[existing[i].Name]; ok {
			existing[i].OpcodeCount = counts
		}
	}
}
