package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

// bundleAt writes a pre-run bundle sidecar describing blocks 100..181.
func bundleAt(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, config.PreRunBundleSubdir), 0o755))

	meta := map[string]any{
		"payloads":           81,
		"start_block_number": 100,
		"start_block_hash":   "0xstart",
		"end_block_number":   181,
		"end_block_hash":     "0xend",
	}

	data, err := json.Marshal(meta)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, config.PreRunBundleSubdir, "pre-run.meta.json"), data, 0o644))

	return dir
}

func runnerWithBundle(dir string) *runner {
	return &runner{cfg: &Config{FullConfig: &config.Config{
		Runner: config.RunnerConfig{
			Benchmark: config.BenchmarkConfig{
				Tests: config.TestsConfig{
					Source: config.SourceConfig{
						EESTFixtures: &config.EESTFixturesSource{
							PreRuns: &config.EESTPreRunsSource{LocalFixturesDir: dir},
						},
					},
				},
			},
		},
	}}}
}

func TestVerifyPreRunBundleHead(t *testing.T) {
	log := logrus.New()
	log.SetOutput(os.NewFile(0, os.DevNull))

	dir := bundleAt(t)
	r := runnerWithBundle(dir)

	t.Run("head at the bundle end reports already applied", func(t *testing.T) {
		applied, err := r.verifyPreRunBundleHead(log, 181, "0xend")
		require.NoError(t, err)
		assert.True(t, applied, "the replay can be skipped entirely")
	})

	t.Run("head at the bundle start means about to replay", func(t *testing.T) {
		applied, err := r.verifyPreRunBundleHead(log, 100, "0xstart")
		require.NoError(t, err)
		assert.False(t, applied)
	})

	t.Run("head inside the range resumes", func(t *testing.T) {
		applied, err := r.verifyPreRunBundleHead(log, 140, "0xwhatever")
		require.NoError(t, err)
		assert.False(t, applied)
	})

	// The case the number-only skip could not catch: right height, wrong chain.
	t.Run("right height wrong hash is rejected", func(t *testing.T) {
		_, err := r.verifyPreRunBundleHead(log, 181, "0xdifferent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "different chain")
	})

	t.Run("wrong hash at the start block is rejected", func(t *testing.T) {
		_, err := r.verifyPreRunBundleHead(log, 100, "0xnope")
		require.Error(t, err)
	})

	t.Run("head outside the range is rejected", func(t *testing.T) {
		_, err := r.verifyPreRunBundleHead(log, 999, "0xfar")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "outside the pre-run bundle range")
	})

	t.Run("no pre_runs configured is a no-op", func(t *testing.T) {
		bare := &runner{cfg: &Config{FullConfig: &config.Config{Runner: config.RunnerConfig{}}}}
		applied, err := bare.verifyPreRunBundleHead(log, 1, "0xany")
		require.NoError(t, err)
		assert.False(t, applied)
	})
}
