package executor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverTestsFromConfig_PreRunStepsNotFiltered(t *testing.T) {
	// Create temp directory structure mimicking a real test source.
	base := t.TempDir()

	// Pre-run step files (no "bn128" in path).
	preRunDir := filepath.Join(base, "bloatnet")
	require.NoError(t, os.MkdirAll(preRunDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(preRunDir, "funding.txt"), []byte("line1"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(preRunDir, "gas-bump.txt"), []byte("line2"), 0644))

	// Test step files — some match "bn128", some don't.
	for _, sub := range []string{"bn128", "ecadd"} {
		dir := filepath.Join(base, "testing", sub)
		require.NoError(t, os.MkdirAll(dir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "test.txt"), []byte("payload"), 0644))
	}

	log := logrus.New()
	log.SetLevel(logrus.DebugLevel)

	result, err := discoverTestsFromConfig(
		base,
		[]string{"bloatnet/funding.txt", "bloatnet/gas-bump.txt"},
		&config.StepsConfig{
			Test: []string{"testing/*/*"},
		},
		"bn128", // filter that does NOT match pre_run_step paths
		log,
	)
	require.NoError(t, err)

	// Pre-run steps must always be included regardless of filter.
	assert.Len(t, result.PreRunSteps, 2, "pre_run_steps should not be filtered")

	preRunNames := make([]string, 0, len(result.PreRunSteps))
	for _, s := range result.PreRunSteps {
		preRunNames = append(preRunNames, s.Name)
	}
	assert.Contains(t, preRunNames, "bloatnet/funding.txt")
	assert.Contains(t, preRunNames, "bloatnet/gas-bump.txt")

	// Test files should be filtered — only "bn128" matches.
	assert.Len(t, result.Tests, 1, "only bn128 test should match filter")
	assert.Contains(t, result.Tests[0].Name, "bn128")
}

func TestLooksLikeCommitHash(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{name: "full sha1", input: "e5011aa5f75d7a1722481f25408347fadfb7fd3c", expected: true},
		{name: "short hash 7 chars", input: "e5011aa", expected: true},
		{name: "short hash 8 chars", input: "e5011aa5", expected: true},
		{name: "uppercase hex", input: "E5011AA5F75D7A17", expected: true},
		{name: "mixed case hex", input: "e5011AA5f75d", expected: true},
		{name: "branch name", input: "main", expected: false},
		{name: "branch with slash", input: "feature/foo", expected: false},
		{name: "tag semver", input: "v1.0.0", expected: false},
		{name: "too short 6 chars", input: "e5011a", expected: false},
		{name: "too long 41 chars", input: "e5011aa5f75d7a1722481f25408347fadfb7fd3c0", expected: false},
		{name: "empty string", input: "", expected: false},
		{name: "hex with non-hex char", input: "e5011gg", expected: false},
		{name: "7 char all digits", input: "1234567", expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, looksLikeCommitHash(tt.input))
		})
	}
}
