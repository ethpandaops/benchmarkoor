package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

// FixtureWriter writes captured Engine API payloads as fixture files
// organized by phase (setup/testing/cleanup).
type FixtureWriter struct {
	log       logrus.FieldLogger
	outputDir string
	mu        sync.Mutex
}

// NewFixtureWriter creates a new fixture writer targeting the given output directory.
func NewFixtureWriter(log logrus.FieldLogger, outputDir string) *FixtureWriter {
	return &FixtureWriter{
		log:       log,
		outputDir: outputDir,
	}
}

// Init creates the output directory structure.
func (w *FixtureWriter) Init() error {
	dirs := []string{
		filepath.Join(w.outputDir, "setup"),
		filepath.Join(w.outputDir, "testing"),
		filepath.Join(w.outputDir, "cleanup"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}

	return nil
}

// WriteTestBlock writes a captured block to the appropriate phase directory.
// For the "testing" phase, it implements the MITM convention:
//   - First block → written to testing/<scenario>.txt
//   - Subsequent blocks → previous testing content migrated to setup/<scenario>.txt,
//     new block overwrites testing/<scenario>.txt
func (w *FixtureWriter) WriteTestBlock(
	testID string,
	phase string,
	block *CapturedBlock,
) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	scenario := scenarioName(testID)
	payload := block.NewPayloadRequest + "\n" + block.FCURequest + "\n"

	switch phase {
	case "testing":
		return w.writeTestingBlock(scenario, payload)
	case "setup":
		return w.appendToFile(
			filepath.Join(w.outputDir, "setup", scenario+".txt"),
			payload,
		)
	case "cleanup":
		return w.appendToFile(
			filepath.Join(w.outputDir, "cleanup", scenario+".txt"),
			payload,
		)
	default:
		return fmt.Errorf("unknown phase %q", phase)
	}
}

// WriteGasBumpBlock writes a gas bump block to gas-bump.txt.
func (w *FixtureWriter) WriteGasBumpBlock(block *CapturedBlock) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	payload := block.NewPayloadRequest + "\n" + block.FCURequest + "\n"

	return w.appendToFile(
		filepath.Join(w.outputDir, "gas-bump.txt"),
		payload,
	)
}

// WriteFundingBlock writes a funding block to funding.txt.
func (w *FixtureWriter) WriteFundingBlock(block *CapturedBlock) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	payload := block.NewPayloadRequest + "\n" + block.FCURequest + "\n"

	return w.appendToFile(
		filepath.Join(w.outputDir, "funding.txt"),
		payload,
	)
}

// writeTestingBlock handles the testing phase migration logic.
// If testing/<scenario>.txt already exists, migrate its content to setup/
// before writing the new block to testing/.
func (w *FixtureWriter) writeTestingBlock(scenario, payload string) error {
	testingFile := filepath.Join(w.outputDir, "testing", scenario+".txt")
	setupFile := filepath.Join(w.outputDir, "setup", scenario+".txt")

	// Check if testing file already exists.
	if existingContent, err := os.ReadFile(testingFile); err == nil {
		// Migrate existing testing content to setup.
		if err := w.appendToFile(setupFile, string(existingContent)); err != nil {
			return fmt.Errorf("migrating testing to setup: %w", err)
		}

		w.log.WithField("scenario", scenario).Debug(
			"Migrated previous testing block to setup",
		)
	}

	// Write new block to testing (overwrite).
	if err := os.WriteFile(testingFile, []byte(payload), 0644); err != nil {
		return fmt.Errorf("writing testing file: %w", err)
	}

	return nil
}

// appendToFile appends content to a file, creating it if necessary.
func (w *FixtureWriter) appendToFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.WriteString(content); err != nil {
		return fmt.Errorf("writing to file: %w", err)
	}

	return nil
}

// scenarioName extracts the scenario name from a test ID.
// Input: "tests/benchmark/compute/precompile/test_identity.py::test_identity[fork_Prague-...]"
// Output: "test_identity.py__test_identity[fork_Prague-...]"
func scenarioName(testID string) string {
	// Split on "::" to get file path and test name.
	parts := strings.SplitN(testID, "::", 2)

	var fileBase, testName string

	if len(parts) == 2 {
		fileBase = filepath.Base(parts[0])
		testName = parts[1]
	} else {
		// Fallback: use the whole ID, sanitized.
		return sanitizeFilename(testID)
	}

	return sanitizeFilename(fileBase + "__" + testName)
}

// sanitizeFilename replaces characters that are invalid in filenames.
func sanitizeFilename(s string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
	)

	return replacer.Replace(s)
}
