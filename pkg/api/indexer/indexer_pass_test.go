package indexer

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
	"github.com/ethpandaops/benchmarkoor/pkg/api/storage"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

// passes returns the recorded pass history, newest first.
func (f *indexerFixture) passes() []indexstore.IndexerPass {
	f.t.Helper()

	passes, err := f.store.ListIndexerPasses(context.Background(), 100)
	require.NoError(f.t, err)

	return passes
}

// TestIndexer_RecordsPassCounters covers the row a pass leaves behind: the
// counters the admin page charts have to describe what the pass actually did.
func TestIndexer_RecordsPassCounters(t *testing.T) {
	f := newIndexerFixture(t, Options{
		Concurrency:  2,
		FailureGrace: time.Hour,
		// No mute window, so a broken run is attempted on every pass.
		FailureRetry: 0,
	})

	// The ages differ because addBrokenRun dates the run ID by them, and two
	// runs of the same age would be the same directory.
	good := f.addBrokenRun(8 * time.Hour)
	f.heal(good)
	f.addBrokenRun(9 * time.Hour)

	f.pass()

	passes := f.passes()
	require.Len(t, passes, 1)

	first := passes[0]
	assert.Equal(t, indexstore.IndexerPassTriggerSchedule, first.Trigger)
	assert.Equal(t, indexstore.IndexerPassStatusCompleted, first.Status)
	assert.Equal(t, 1, first.DiscoveryPaths)
	assert.Equal(t, 2, first.StorageRuns, "both run directories")
	assert.Equal(t, 0, first.IndexedRuns, "nothing was indexed before")
	assert.Equal(t, 1, first.RunsIndexed, "the healthy run")
	assert.Equal(t, 0, first.RunsReindexed)
	assert.Equal(t, 1, first.RunsFailed, "the run with no config.json")
	assert.Equal(t, 0, first.SkippedFailures)
	assert.Empty(t, first.Error)

	assert.Positive(t, first.DurationMs+1, "a duration is recorded")
	assert.False(t, first.StartedAt.IsZero())
	assert.False(t, first.FinishedAt.IsZero())
	assert.False(t, first.FinishedAt.Before(first.StartedAt))

	// A second pass sees the healthy run already indexed and leaves it alone.
	f.pass()

	passes = f.passes()
	require.Len(t, passes, 2)

	second := passes[0]
	assert.Equal(t, 2, second.StorageRuns)
	assert.Equal(t, 1, second.IndexedRuns, "the healthy run is in the index")
	assert.Equal(t, 0, second.RunsIndexed)
	assert.Equal(t, 1, second.RunsFailed)
}

// TestIndexer_RecordsSkippedFailures covers the counter that explains a fast
// pass on a neglected deployment: the muted runs were never read at all.
func TestIndexer_RecordsSkippedFailures(t *testing.T) {
	f := newIndexerFixture(t, Options{
		Concurrency:  2,
		FailureGrace: time.Hour,
		FailureRetry: 24 * time.Hour,
	})

	f.addBrokenRun(8 * time.Hour)

	f.pass()
	f.pass()

	passes := f.passes()
	require.Len(t, passes, 2)

	assert.Equal(t, 1, passes[1].RunsFailed, "the first pass tried it")
	assert.Equal(t, 0, passes[1].SkippedFailures)

	assert.Equal(t, 0, passes[0].RunsFailed, "the second pass skipped it")
	assert.Equal(t, 1, passes[0].SkippedFailures)
}

// TestIndexer_RecordsManualTrigger covers the trigger field, which is what
// tells a pass an admin asked for apart from a scheduled one.
func TestIndexer_RecordsManualTrigger(t *testing.T) {
	f := newIndexerFixture(t, Options{
		Concurrency:  1,
		Interval:     time.Minute,
		FailureGrace: time.Hour,
		FailureRetry: time.Hour,
	})
	f.idx.setLifecycle(context.Background())

	require.True(t, f.idx.RunNow())

	// RunNow returns before the pass finishes, so wait for its row.
	require.Eventually(t, func() bool {
		return len(f.passes()) == 1
	}, 5*time.Second, 10*time.Millisecond)

	assert.Equal(
		t, indexstore.IndexerPassTriggerManual, f.passes()[0].Trigger,
	)

	state := f.idx.State()
	assert.False(t, state.Running, "the pass is done")
	assert.Equal(t, time.Minute, state.Interval)
}

// blockingReader holds the pass inside its first storage call until it is
// released, so a test can look at the indexer while a pass is in flight.
type blockingReader struct {
	release chan struct{}
	entered chan struct{}
	once    sync.Once
}

var _ storage.Reader = (*blockingReader)(nil)

func newBlockingReader() *blockingReader {
	return &blockingReader{
		release: make(chan struct{}),
		entered: make(chan struct{}),
	}
}

func (r *blockingReader) DiscoveryPaths() []string { return []string{"dp"} }

func (r *blockingReader) ListRunIDs(
	_ context.Context, _ string,
) ([]string, error) {
	r.once.Do(func() { close(r.entered) })
	<-r.release

	return nil, nil
}

func (r *blockingReader) GetRunFile(
	_ context.Context, _, _, _ string,
) ([]byte, error) {
	return nil, nil
}

func (r *blockingReader) GetSuiteFile(
	_ context.Context, _, _, _ string,
) ([]byte, error) {
	return nil, nil
}

// TestIndexer_ReportsRunningPass covers what the admin page needs to keep the
// "Run Indexer" button honest: while a pass is in flight the indexer says so,
// says since when, and refuses to start a second one.
func TestIndexer_ReportsRunningPass(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	store := indexstore.NewStore(log, &config.APIDatabaseConfig{
		Driver: "sqlite",
		SQLite: config.SQLiteDatabaseConfig{Path: ":memory:"},
	})
	require.NoError(t, store.Start(context.Background()))
	t.Cleanup(func() { _ = store.Stop() })

	reader := newBlockingReader()

	idx, ok := NewIndexer(
		log, store, reader, Options{Interval: 2 * time.Minute}, nil,
	).(*indexer)
	require.True(t, ok)

	idx.setLifecycle(context.Background())

	assert.False(t, idx.State().Running, "idle before anything starts")

	require.True(t, idx.RunNow())

	select {
	case <-reader.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the pass never reached storage")
	}

	state := idx.State()
	assert.True(t, state.Running)
	assert.Equal(t, indexstore.IndexerPassTriggerManual, state.Trigger)
	assert.False(t, state.StartedAt.IsZero(), "the start time is published")
	assert.False(t, state.StartedAt.After(time.Now().UTC()))

	assert.False(t, idx.RunNow(), "a second pass is refused while one runs")

	close(reader.release)

	require.Eventually(t, func() bool {
		return !idx.State().Running
	}, 5*time.Second, 10*time.Millisecond)

	assert.True(
		t, idx.State().StartedAt.IsZero(), "the finished pass is cleared",
	)
}

// TestIndexer_RunNowBeforeStart covers the window between the HTTP server
// listening and Start setting the lifecycle context: a pass triggered there
// must run, not take the process down with a nil context.
func TestIndexer_RunNowBeforeStart(t *testing.T) {
	f := newIndexerFixture(t, Options{
		Concurrency:  1,
		Interval:     time.Minute,
		FailureGrace: time.Hour,
		FailureRetry: time.Hour,
	})

	require.Nil(t, f.idx.lifecycle.Load(), "Start has not run")
	require.True(t, f.idx.RunNow())

	require.Eventually(t, func() bool {
		return len(f.passes()) == 1
	}, 5*time.Second, 10*time.Millisecond)
}

// TestIndexer_RunNowDuringStart covers the same window from the other side.
// Start publishes the lifecycle context while HTTP handlers can already call
// RunNow, so the two must not touch it through a plain field. Run this one
// with -race: that is the failure it is here to catch.
func TestIndexer_RunNowDuringStart(t *testing.T) {
	f := newIndexerFixture(t, Options{
		Concurrency:  1,
		Interval:     time.Hour,
		FailureGrace: time.Hour,
		FailureRetry: time.Hour,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		wg       sync.WaitGroup
		startErr error
	)

	wg.Add(1)

	go func() {
		defer wg.Done()

		startErr = f.idx.Start(ctx)
	}()

	for range 200 {
		f.idx.RunNow()
	}

	wg.Wait()
	require.NoError(t, startErr)
	require.NoError(t, f.idx.Stop())
}
