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
