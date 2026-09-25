package indexer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
	"github.com/ethpandaops/benchmarkoor/pkg/api/storage"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

const testDiscoveryPath = "results"

// indexerFixture is a local-filesystem discovery path wired to an in-memory
// index store, so a pass can be driven end to end without S3.
type indexerFixture struct {
	t     *testing.T
	dir   string
	store indexstore.Store
	idx   *indexer
}

func newIndexerFixture(t *testing.T, opts Options) *indexerFixture {
	t.Helper()

	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	store := indexstore.NewStore(log, &config.APIDatabaseConfig{
		Driver: "sqlite",
		SQLite: config.SQLiteDatabaseConfig{Path: ":memory:"},
	})
	require.NoError(t, store.Start(context.Background()))
	t.Cleanup(func() { _ = store.Stop() })

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "runs"), 0o755))

	reader := storage.NewLocalReader(&config.APILocalStorageConfig{
		Enabled:        true,
		DiscoveryPaths: map[string]string{testDiscoveryPath: dir},
	})

	idx, ok := NewIndexer(log, store, reader, opts, nil).(*indexer)
	require.True(t, ok)

	return &indexerFixture{t: t, dir: dir, store: store, idx: idx}
}

// addBrokenRun creates a run directory with no config.json, dated by the
// timestamp its run ID carries. This is the shape the indexer keeps tripping
// over in production.
func (f *indexerFixture) addBrokenRun(age time.Duration) string {
	f.t.Helper()

	runID := fmt.Sprintf(
		"%d_abc12345_geth-bal-full", time.Now().Add(-age).Unix(),
	)
	require.NoError(f.t, os.MkdirAll(f.runDir(runID), 0o755))

	return runID
}

// heal writes a valid config.json into an existing run directory.
func (f *indexerFixture) heal(runID string) {
	f.t.Helper()

	const cfg = `{"timestamp":100,"status":"completed",` +
		`"instance":{"id":"geth-bal-full","client":"geth"}}`

	require.NoError(f.t, os.WriteFile(
		filepath.Join(f.runDir(runID), "config.json"), []byte(cfg), 0o644,
	))
}

func (f *indexerFixture) runDir(runID string) string {
	return filepath.Join(f.dir, "runs", runID)
}

func (f *indexerFixture) pass() {
	f.t.Helper()

	f.idx.runPass(context.Background(), indexstore.IndexerPassTriggerSchedule)
}

func (f *indexerFixture) failures() []indexstore.IndexFailure {
	f.t.Helper()

	failures, err := f.store.ListIndexFailures(context.Background(), 100, 0)
	require.NoError(f.t, err)

	return failures
}

// TestIndexer_RecordsFailureOnlyAfterGracePeriod covers the rule that keeps a
// run still uploading out of the failure table: a missing config.json only
// counts against a run old enough to have finished uploading.
func TestIndexer_RecordsFailureOnlyAfterGracePeriod(t *testing.T) {
	f := newIndexerFixture(t, Options{
		Concurrency:  2,
		FailureGrace: 6 * time.Hour,
		FailureRetry: 24 * time.Hour,
	})

	fresh := f.addBrokenRun(10 * time.Minute)
	old := f.addBrokenRun(8 * time.Hour)

	f.pass()

	failures := f.failures()
	require.Len(t, failures, 1, "only the old run earns a record")
	assert.Equal(t, old, failures[0].RunID)
	assert.Equal(t, testDiscoveryPath, failures[0].DiscoveryPath)
	assert.Equal(t, 1, failures[0].Attempts)
	assert.Contains(t, failures[0].LastError, "config.json not found")

	for _, failure := range failures {
		assert.NotEqual(t, fresh, failure.RunID)
	}
}

// TestIndexer_SkipsRecordedFailures covers the retry interval: a recorded run
// is not read from storage again until the interval lapses. Re-reading
// thousands of broken runs every pass is what made a pass take half an hour.
func TestIndexer_SkipsRecordedFailures(t *testing.T) {
	f := newIndexerFixture(t, Options{
		Concurrency:  2,
		FailureGrace: time.Hour,
		FailureRetry: 24 * time.Hour,
	})

	runID := f.addBrokenRun(8 * time.Hour)

	f.pass()
	require.Len(t, f.failures(), 1)

	// The run is muted, so a second pass does not even attempt it.
	f.pass()

	failures := f.failures()
	require.Len(t, failures, 1)
	assert.Equal(t, 1, failures[0].Attempts, "a muted run is not retried")

	// Healing it changes nothing while the record is still muted.
	f.heal(runID)
	f.pass()

	assert.Len(t, f.failures(), 1)

	runs, err := f.store.ListRuns(context.Background(), testDiscoveryPath)
	require.NoError(t, err)
	assert.Empty(t, runs)
}

// TestIndexer_RetriesAfterInterval covers the other half of the retry
// interval: once it lapses, a run that healed is indexed and loses its record.
func TestIndexer_RetriesAfterInterval(t *testing.T) {
	f := newIndexerFixture(t, Options{
		Concurrency: 2,
		// No mute window, so every pass retries.
		FailureGrace: time.Hour,
		FailureRetry: 0,
	})

	runID := f.addBrokenRun(8 * time.Hour)

	f.pass()
	require.Len(t, f.failures(), 1)

	// Still broken: the attempt count climbs, the record stays one row.
	f.pass()

	failures := f.failures()
	require.Len(t, failures, 1)
	assert.Equal(t, 2, failures[0].Attempts)

	// The upload finally lands.
	f.heal(runID)
	f.pass()

	assert.Empty(t, f.failures(), "an indexed run loses its record")

	runs, err := f.store.ListRuns(context.Background(), testDiscoveryPath)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, runID, runs[0].RunID)
}

// TestIndexer_PrunesFailuresForRunsLeavingStorage covers the cleanup that
// makes an admin deleting a failed run's data remove its record too.
func TestIndexer_PrunesFailuresForRunsLeavingStorage(t *testing.T) {
	f := newIndexerFixture(t, Options{
		Concurrency:  2,
		FailureGrace: time.Hour,
		FailureRetry: 24 * time.Hour,
	})

	kept := f.addBrokenRun(8 * time.Hour)
	removed := f.addBrokenRun(9 * time.Hour)

	f.pass()
	require.Len(t, f.failures(), 2)

	require.NoError(t, os.RemoveAll(f.runDir(removed)))

	f.pass()

	failures := f.failures()
	require.Len(t, failures, 1)
	assert.Equal(t, kept, failures[0].RunID)
}

// TestIndexer_HealthyRunNeverRecordsAFailure is the guard that this
// housekeeping stays invisible on a working deployment.
func TestIndexer_HealthyRunNeverRecordsAFailure(t *testing.T) {
	f := newIndexerFixture(t, Options{
		Concurrency:  2,
		FailureGrace: time.Hour,
		FailureRetry: 24 * time.Hour,
	})

	runID := f.addBrokenRun(8 * time.Hour)
	f.heal(runID)

	f.pass()

	assert.Empty(t, f.failures())

	runs, err := f.store.ListRuns(context.Background(), testDiscoveryPath)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, runID, runs[0].RunID)
}
