package indexstore_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

const baselineName = "tests/benchmark/stateful/bloatnet/test_sload.py::" +
	"test_sload_bloated[fork_Amsterdam-blockchain_test_stateful_engine-" +
	"overhead_baseline_True-benchmark-gas-value_200M]"

func TestIsBaselineTest(t *testing.T) {
	assert.True(t, indexstore.IsBaselineTest(baselineName))
	assert.True(t, indexstore.IsBaselineTest("x-overhead_baseline_true-y"),
		"match must be case-insensitive")
	assert.False(t, indexstore.IsBaselineTest(
		"test_sload_bloated[overhead_baseline_False-benchmark-gas-value_200M]"),
		"the _False twin is the real workload measurement")
	assert.False(t, indexstore.IsBaselineTest("test_keccak[gas-value_200M]"))
}

func TestListRecentExcludesBaselineByDefault(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()
	suite := "suite-baseline"

	workload := stat(suite, "run-A", "t1", "geth", 300)
	baseline := stat(suite, "run-A", baselineName, "geth", 300)
	baseline.Baseline = true

	require.NoError(t, s.BulkUpsertTestStats(
		ctx, []*indexstore.TestStat{workload, baseline},
	))

	got, err := s.ListTestStatsBySuiteRecent(ctx, suite, 2, false)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "t1", got[0].TestName,
		"baseline rows must be excluded by default")

	got, err = s.ListTestStatsBySuiteRecent(ctx, suite, 2, true)
	require.NoError(t, err)
	assert.Len(t, got, 2, "includeBaseline must return the raw set")
}

// Rows indexed before the baseline column existed carry baseline=false even
// when their name matches; Start must backfill them.
func TestStartBackfillsBaselineFlag(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "index.db")

	cfg := &config.APIDatabaseConfig{
		Driver: "sqlite",
		SQLite: config.SQLiteDatabaseConfig{Path: dbPath},
	}

	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	s := indexstore.NewStore(log, cfg)
	require.NoError(t, s.Start(ctx))

	// Simulate a legacy row: baseline-named but unflagged.
	legacy := stat("suite-bf", "run-A", baselineName, "geth", 300)
	require.NoError(t, s.BulkUpsertTestStats(ctx, []*indexstore.TestStat{legacy}))
	require.NoError(t, s.Stop())

	// Restarting runs the migration backfill.
	s = indexstore.NewStore(log, cfg)
	require.NoError(t, s.Start(ctx))

	t.Cleanup(func() { _ = s.Stop() })

	got, err := s.ListTestStatsBySuiteRecent(ctx, "suite-bf", 2, false)
	require.NoError(t, err)
	assert.Empty(t, got, "backfilled baseline row must be excluded")

	got, err = s.ListTestStatsBySuiteRecent(ctx, "suite-bf", 2, true)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.True(t, got[0].Baseline, "flag must be backfilled on Start")
}
