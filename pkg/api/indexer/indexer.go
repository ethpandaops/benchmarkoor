package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
	"github.com/ethpandaops/benchmarkoor/pkg/api/storage"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/executor"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

// defaultConcurrency is the number of runs indexed in parallel when
// no explicit concurrency value is configured.
const defaultConcurrency = 4

// passRecordTimeout bounds the write that stores a finished pass. The write
// runs on a context without cancellation, so that a pass a shutdown cut short
// still leaves its row behind, and this is what stops that outliving the
// shutdown itself.
const passRecordTimeout = 10 * time.Second

// Indexer is a background service that periodically scans storage
// and upserts indexed run/suite data into the index store.
type Indexer interface {
	Start(ctx context.Context) error
	Stop() error
	// RunNow triggers an immediate indexing pass. Returns true if a
	// new pass was kicked off, false if one is already running.
	RunNow() bool
	// State reports whether a pass is running right now and how often
	// passes are scheduled.
	State() State
}

// State is what the admin API reports about the indexer itself. The pass
// history lives in the index store; this is only what the store cannot know.
type State struct {
	// Running says whether a pass is in flight at this moment. A pass only
	// writes its row when it ends, so a running pass is nowhere in the
	// history until then.
	Running bool

	// StartedAt and Trigger describe the running pass. They are zero when no
	// pass is running, and StartedAt is how long it has been going.
	StartedAt time.Time
	Trigger   string

	// Interval is the configured delay between scheduled passes.
	Interval time.Duration
}

// currentPass is the pass in flight, published for State to read.
type currentPass struct {
	startedAt time.Time
	trigger   string
}

// lifecycleCtx carries the context Start was given, so a pass RunNow starts
// outlives the request that asked for it.
//
// It travels through an atomic rather than a plain field because Start writes
// it while the HTTP server is already serving: RunNow reads it from a handler
// goroutine, and an unsynchronised read is a data race whatever value it
// happens to land on.
type lifecycleCtx struct {
	ctx context.Context
}

// Compile-time interface check.
var _ Indexer = (*indexer)(nil)

// Options configures an indexer.
type Options struct {
	// Interval is the delay between indexing passes.
	Interval time.Duration

	// Concurrency bounds how many runs are indexed in parallel. Zero means
	// defaultConcurrency.
	Concurrency int

	// FailureGrace is how old a run must be before the indexer records it as
	// a failure. Below it a missing config.json means "still uploading", not
	// "broken".
	FailureGrace time.Duration

	// FailureRetry is how long a recorded failure is skipped for before the
	// indexer tries it again. It stops thousands of broken runs from costing
	// a storage round-trip on every pass, while still letting a run whose
	// upload finished late heal on its own.
	FailureRetry time.Duration
}

type indexer struct {
	log              logrus.FieldLogger
	store            indexstore.Store
	reader           storage.Reader
	opts             Options
	onLiveRunIndexed func(runID string)
	// lifecycle carries the context Start was given. See the type.
	lifecycle atomic.Pointer[lifecycleCtx]
	done      chan struct{}
	wg        sync.WaitGroup
	running   atomic.Bool // prevents overlapping indexing passes
	// current describes the pass running right now, nil when none is. It is
	// claimed a moment after running, so a State read caught in between says
	// a pass is running without saying since when.
	current atomic.Pointer[currentPass]
	dbMu    sync.Mutex // serializes DB writes to avoid SQLite contention
}

// NewIndexer creates a new background indexer. `onLiveRunIndexed`, when
// non-nil, is invoked with the run_id right after the canonical Run
// row is upserted (and the matching live_runs row deleted). Used by
// the API to drop log-stream state for the run since the live panel
// is about to be superseded by the static run detail view.
func NewIndexer(
	log logrus.FieldLogger,
	store indexstore.Store,
	reader storage.Reader,
	opts Options,
	onLiveRunIndexed func(runID string),
) Indexer {
	if opts.Concurrency <= 0 {
		opts.Concurrency = defaultConcurrency
	}

	return &indexer{
		log:              log.WithField("component", "indexer"),
		store:            store,
		reader:           reader,
		opts:             opts,
		onLiveRunIndexed: onLiveRunIndexed,
		done:             make(chan struct{}),
	}
}

// Start launches a background goroutine that runs an immediate indexing
// pass and then ticks at the configured interval. The first pass is
// asynchronous so the caller (the API server) is not blocked.
func (idx *indexer) Start(ctx context.Context) error {
	idx.setLifecycle(ctx)

	idx.log.WithFields(logrus.Fields{
		"interval":      idx.opts.Interval.String(),
		"concurrency":   idx.opts.Concurrency,
		"failure_grace": idx.opts.FailureGrace.String(),
		"failure_retry": idx.opts.FailureRetry.String(),
	}).Info("Starting indexer")

	idx.wg.Add(1)

	go func() {
		defer idx.wg.Done()

		// Run one pass immediately.
		idx.runPass(ctx, indexstore.IndexerPassTriggerStartup)

		ticker := time.NewTicker(idx.opts.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				idx.runPass(ctx, indexstore.IndexerPassTriggerSchedule)
			case <-idx.done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	return nil
}

// Stop signals the indexer goroutine to stop and waits for it.
func (idx *indexer) Stop() error {
	close(idx.done)
	idx.wg.Wait()

	idx.log.Info("Indexer stopped")

	return nil
}

// RunNow triggers an immediate indexing pass in a background goroutine.
// It returns true if a new pass was started, false if one is already running.
// The running flag is claimed synchronously via CAS so concurrent callers
// get an accurate answer before the goroutine is scheduled. The pass uses
// the lifecycle context from Start, not a request-scoped context.
func (idx *indexer) RunNow() bool {
	if !idx.running.CompareAndSwap(false, true) {
		return false
	}

	ctx := idx.lifecycleContext()

	idx.wg.Add(1)

	go func() {
		defer idx.wg.Done()

		idx.runPassInner(ctx, indexstore.IndexerPassTriggerManual)
	}()

	return true
}

// setLifecycle publishes the context passes run under.
func (idx *indexer) setLifecycle(ctx context.Context) {
	idx.lifecycle.Store(&lifecycleCtx{ctx: ctx})
}

// lifecycleContext returns the context Start published. The HTTP server
// listens before Start runs, so a pass triggered in that window gets a
// background context: a nil one would panic in the pass goroutine and take
// the process with it.
func (idx *indexer) lifecycleContext() context.Context {
	if life := idx.lifecycle.Load(); life != nil {
		return life.ctx
	}

	return context.Background()
}

// State reports what the index store cannot: whether a pass is in flight,
// since when, and how often passes are scheduled.
func (idx *indexer) State() State {
	state := State{
		Running:  idx.running.Load(),
		Interval: idx.opts.Interval,
	}

	if pass := idx.current.Load(); pass != nil {
		state.StartedAt = pass.startedAt
		state.Trigger = pass.trigger
	}

	return state
}

// runPass attempts to run one indexing pass if no other pass is active.
// Used by the periodic ticker and initial startup pass.
func (idx *indexer) runPass(ctx context.Context, trigger string) {
	if !idx.running.CompareAndSwap(false, true) {
		return
	}

	idx.runPassInner(ctx, trigger)
}

// runPassInner executes one full indexing pass across all discovery paths.
// The caller must have already set running to true; this method resets it
// on return. Whatever the pass does, it records a row describing itself, so
// the admin page can chart how long passes take and what they find.
func (idx *indexer) runPassInner(ctx context.Context, trigger string) {
	defer idx.running.Store(false)

	start := time.Now()

	// Publish the pass before any work, so an admin watching the page sees
	// how long it has been going rather than only that it is going.
	idx.current.Store(&currentPass{startedAt: start.UTC(), trigger: trigger})
	defer idx.current.Store(nil)

	paths := idx.reader.DiscoveryPaths()

	pass := &indexstore.IndexerPass{
		StartedAt:      start.UTC(),
		Trigger:        trigger,
		Status:         indexstore.IndexerPassStatusCompleted,
		DiscoveryPaths: len(paths),
	}

	idx.log.WithFields(logrus.Fields{
		"discovery_paths": len(paths),
		"trigger":         trigger,
	}).Info("Indexing pass started")

	for _, dp := range paths {
		if idx.stopping(ctx) {
			pass.Status = indexstore.IndexerPassStatusCancelled

			break
		}

		stats, err := idx.indexDiscoveryPath(ctx, dp)
		if err != nil {
			idx.log.WithError(err).
				WithField("discovery_path", dp).
				Warn("Indexing pass failed for discovery path")

			pass.Error = err.Error()
		}

		// A path that failed part-way still did the work it got through, so
		// its counters are folded in either way.
		stats.addTo(pass)
	}

	pass.FinishedAt = time.Now().UTC()
	pass.DurationMs = pass.FinishedAt.Sub(pass.StartedAt).Milliseconds()

	idx.log.WithFields(logrus.Fields{
		"duration":       time.Since(start).Round(time.Millisecond),
		"status":         pass.Status,
		"runs_indexed":   pass.RunsIndexed,
		"runs_reindexed": pass.RunsReindexed,
		"runs_failed":    pass.RunsFailed,
	}).Info("Indexing pass completed")

	idx.recordPass(ctx, pass)
}

// stopping reports whether the pass must give up, either because the
// lifecycle context is done or because Stop was called.
func (idx *indexer) stopping(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	case <-idx.done:
		return true
	default:
		return false
	}
}

// recordPass stores the finished pass. A shutdown cancels the pass context,
// and a cancelled pass is exactly the one worth having a record of, so the
// write runs on a context that carries the values but not the cancellation.
func (idx *indexer) recordPass(
	ctx context.Context, pass *indexstore.IndexerPass,
) {
	writeCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx), passRecordTimeout,
	)
	defer cancel()

	// Same reason the per-run writes serialize: SQLite has one writer.
	idx.dbMu.Lock()
	err := idx.store.RecordIndexerPass(writeCtx, pass)
	idx.dbMu.Unlock()

	if err != nil {
		idx.log.WithError(err).Warn("Failed to record indexing pass")
	}
}

// pathStats is what one discovery path contributed to a pass. The counters the
// worker pool touches are atomic; the rest are settled by the scan before the
// pool starts.
type pathStats struct {
	storageRuns int
	indexedRuns int
	skipped     int

	indexed   atomic.Int64
	reindexed atomic.Int64
	failed    atomic.Int64
}

// addTo folds one path's counters into the pass totals. A nil receiver adds
// nothing, which is what a path that failed before it counted anything did.
func (p *pathStats) addTo(pass *indexstore.IndexerPass) {
	if p == nil {
		return
	}

	pass.StorageRuns += p.storageRuns
	pass.IndexedRuns += p.indexedRuns
	pass.SkippedFailures += p.skipped
	pass.RunsIndexed += int(p.indexed.Load())
	pass.RunsReindexed += int(p.reindexed.Load())
	pass.RunsFailed += int(p.failed.Load())
}

// indexDiscoveryPath performs incremental indexing for a single
// discovery path. It discovers new runs and re-indexes incomplete ones
// using a bounded worker pool for parallel processing. It returns what the
// path contributed to the pass, which is nil only when the path failed before
// it did anything.
func (idx *indexer) indexDiscoveryPath(
	ctx context.Context, dp string,
) (*pathStats, error) {
	// List all run IDs from storage.
	storageIDs, err := idx.reader.ListRunIDs(ctx, dp)
	if err != nil {
		return nil, fmt.Errorf("listing storage run IDs: %w", err)
	}

	// List already-indexed run IDs.
	indexedIDs, err := idx.store.ListRunIDs(ctx, dp)
	if err != nil {
		return nil, fmt.Errorf("listing indexed run IDs: %w", err)
	}

	// List incomplete run IDs that need re-indexing.
	incompleteIDs, err := idx.store.ListIncompleteRunIDs(ctx, dp)
	if err != nil {
		return nil, fmt.Errorf("listing incomplete run IDs: %w", err)
	}

	stats := &pathStats{
		storageRuns: len(storageIDs),
		indexedRuns: len(indexedIDs),
	}

	indexedSet := make(map[string]struct{}, len(indexedIDs))
	for _, id := range indexedIDs {
		indexedSet[id] = struct{}{}
	}

	incompleteSet := make(map[string]struct{}, len(incompleteIDs))
	for _, id := range incompleteIDs {
		incompleteSet[id] = struct{}{}
	}

	dpLog := idx.log.WithField("discovery_path", dp)

	// Runs that already failed to index. Recent ones are skipped outright:
	// re-reading thousands of broken runs every pass is what made a pass take
	// half an hour and flooded the log.
	failures, err := idx.loadFailures(ctx, dp, storageIDs, dpLog)
	if err != nil {
		return nil, err
	}

	// Build list of runs that need indexing.
	type runTask struct {
		runID          string
		alreadyIndexed bool
	}

	var tasks []runTask

	for _, id := range storageIDs {
		_, alreadyIndexed := indexedSet[id]
		_, isIncomplete := incompleteSet[id]

		if alreadyIndexed && !isIncomplete {
			continue
		}

		if _, muted := failures.skip[id]; muted {
			stats.skipped++

			continue
		}

		tasks = append(tasks, runTask{
			runID:          id,
			alreadyIndexed: alreadyIndexed,
		})
	}

	newCount := 0
	for _, t := range tasks {
		if !t.alreadyIndexed {
			newCount++
		}
	}

	dpLog.WithFields(logrus.Fields{
		"storage_runs":      len(storageIDs),
		"indexed_runs":      len(indexedIDs),
		"new_runs":          newCount,
		"incomplete_runs":   len(incompleteIDs),
		"recorded_failures": len(failures.recorded),
		"skipped_failures":  stats.skipped,
	}).Info("Scanning discovery path")

	if len(tasks) == 0 {
		return stats, nil
	}

	// Process runs concurrently with bounded parallelism.
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(idx.opts.Concurrency)

	for _, task := range tasks {
		g.Go(func() error {
			// Check for cancellation before starting work.
			select {
			case <-gCtx.Done():
				return gCtx.Err()
			case <-idx.done:
				return nil
			default:
			}

			_, wasRecorded := failures.recorded[task.runID]

			if err := idx.indexRun(
				gCtx, dp, task.runID, task.alreadyIndexed,
			); err != nil {
				stats.failed.Add(1)

				idx.handleIndexFailure(
					gCtx, dp, task.runID, err, wasRecorded, dpLog,
				)

				return nil //nolint:nilerr // record and continue
			}

			// A run that healed must not keep its record, or the retry
			// interval would keep muting a run that now indexes fine.
			if wasRecorded {
				if cErr := idx.clearFailure(
					gCtx, dp, task.runID,
				); cErr != nil {
					dpLog.WithError(cErr).
						WithField("run_id", task.runID).
						Warn("Failed to clear index failure")
				}
			}

			action := "indexed"
			if task.alreadyIndexed {
				action = "reindexed"

				stats.reindexed.Add(1)
			} else {
				stats.indexed.Add(1)
			}

			dpLog.WithField("run_id", task.runID).
				WithField("action", action).
				Info("Indexed run")

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		// The pool got through some of the tasks, so the counters still
		// describe real work and go back with the error.
		return stats, fmt.Errorf("indexing runs: %w", err)
	}

	if count := stats.indexed.Load() + stats.reindexed.Load(); count > 0 {
		dpLog.WithField("count", count).
			Info("Discovery path indexing complete")
	}

	return stats, nil
}

// indexFailureState is one pass's view of the recorded failures for a
// discovery path.
type indexFailureState struct {
	// recorded holds every run that already has a failure record, so the
	// pass knows a failure is a repeat and not news.
	recorded map[string]struct{}

	// skip holds the subset whose last attempt is still inside the retry
	// interval. Those runs are not read from storage at all.
	skip map[string]struct{}
}

// loadFailures reads the recorded failures for a discovery path and splits
// them into the muted set and the rest. It also drops records whose run has
// since left storage, which is what makes an admin deleting a run's data
// clean up the record on the next pass.
func (idx *indexer) loadFailures(
	ctx context.Context,
	dp string,
	storageIDs []string,
	dpLog logrus.FieldLogger,
) (*indexFailureState, error) {
	records, err := idx.store.ListIndexFailuresByPath(ctx, dp)
	if err != nil {
		return nil, fmt.Errorf("listing index failures: %w", err)
	}

	state := &indexFailureState{
		recorded: make(map[string]struct{}, len(records)),
		skip:     make(map[string]struct{}, len(records)),
	}

	if len(records) == 0 {
		return state, nil
	}

	inStorage := make(map[string]struct{}, len(storageIDs))
	for _, id := range storageIDs {
		inStorage[id] = struct{}{}
	}

	muteBefore := time.Now().UTC().Add(-idx.opts.FailureRetry)

	for i := range records {
		record := &records[i]

		// The run is gone from storage, so the record has nothing left to
		// describe.
		if _, ok := inStorage[record.RunID]; !ok {
			if cErr := idx.store.ClearIndexFailure(
				ctx, dp, record.RunID,
			); cErr != nil {
				dpLog.WithError(cErr).
					WithField("run_id", record.RunID).
					Warn("Failed to prune index failure")
			}

			continue
		}

		state.recorded[record.RunID] = struct{}{}

		if record.LastAttemptAt.After(muteBefore) {
			state.skip[record.RunID] = struct{}{}
		}
	}

	return state, nil
}

// clearFailure drops a failure record, serializing the write with the other
// per-run writes of the pass.
func (idx *indexer) clearFailure(
	ctx context.Context, dp, runID string,
) error {
	idx.dbMu.Lock()
	defer idx.dbMu.Unlock()

	return idx.store.ClearIndexFailure(ctx, dp, runID)
}

// handleIndexFailure decides what a failed run earns. A run younger than the
// grace period is left alone: its config.json may still be uploading, and
// marking it broken would be wrong. An older one gets a record, so the pass
// can skip it next time and an admin can find it.
//
// The first failure is logged at warn. Repeats drop to debug, because the
// record is the durable signal and thousands of warn lines a pass are not.
func (idx *indexer) handleIndexFailure(
	ctx context.Context,
	dp, runID string,
	cause error,
	wasRecorded bool,
	dpLog logrus.FieldLogger,
) {
	log := dpLog.WithError(cause).WithField("run_id", runID)

	if ts := indexstore.RunIDTimestamp(runID); ts > 0 {
		if age := time.Since(time.Unix(ts, 0)); age < idx.opts.FailureGrace {
			log.WithField("age", age.Round(time.Second)).
				Debug("Skipped failed run inside the grace period")

			return
		}
	}

	// Same reason indexRun serializes its writes: the pass runs these
	// concurrently and SQLite does not enjoy that.
	idx.dbMu.Lock()
	err := idx.store.RecordIndexFailure(ctx, dp, runID, cause.Error())
	idx.dbMu.Unlock()

	if err != nil {
		log.WithError(err).Warn("Failed to record index failure")

		return
	}

	if wasRecorded {
		log.Debug("Failed to index run again")

		return
	}

	log.Warn("Failed to index run")
}

// indexRun reads config.json and optionally result.json for a run,
// builds index models, and upserts them into the store. The two file
// reads are performed concurrently to reduce latency.
func (idx *indexer) indexRun(
	ctx context.Context, dp, runID string, isReindex bool,
) error {
	// Read config.json, result.json, and result.block-logs.json concurrently.
	var (
		configData, resultData, blockLogsData []byte
		configErr, resultErr, blockLogsErr    error
		fileWg                                sync.WaitGroup
	)

	fileWg.Add(3) //nolint:mnd // three files

	go func() {
		defer fileWg.Done()

		configData, configErr = idx.reader.GetRunFile(
			ctx, dp, runID, "config.json",
		)
	}()

	go func() {
		defer fileWg.Done()

		resultData, resultErr = idx.reader.GetRunFile(
			ctx, dp, runID, "result.json",
		)
	}()

	go func() {
		defer fileWg.Done()

		blockLogsData, blockLogsErr = idx.reader.GetRunFile(
			ctx, dp, runID, "result.block-logs.json",
		)
	}()

	fileWg.Wait()

	if configErr != nil {
		return fmt.Errorf("reading config.json: %w", configErr)
	}

	if configData == nil {
		return fmt.Errorf("config.json not found")
	}

	if resultErr != nil {
		idx.log.WithError(resultErr).WithField("run_id", runID).
			Debug("Failed to read result.json, continuing without it")

		resultData = nil
	}

	if blockLogsErr != nil {
		blockLogsData = nil
	}

	// Build an IndexEntry using the existing executor logic.
	entry, err := executor.BuildIndexEntryFromData(
		runID, configData, resultData,
	)
	if err != nil {
		return fmt.Errorf("building index entry: %w", err)
	}

	// Override tests_total from suite summary when available and
	// upsert the suite record.
	if entry.SuiteHash != "" {
		summaryData, sErr := idx.reader.GetSuiteFile(
			ctx, dp, entry.SuiteHash, "summary.json",
		)
		if sErr == nil && summaryData != nil {
			var summary struct {
				Tests    json.RawMessage        `json:"tests"`
				Metadata *config.MetadataConfig `json:"metadata"`
			}

			if json.Unmarshal(summaryData, &summary) == nil {
				// Count tests by unmarshalling tests array length.
				var tests []json.RawMessage
				if json.Unmarshal(summary.Tests, &tests) == nil &&
					len(tests) > 0 {
					entry.Tests.TestsTotal = len(tests)
				}

				// Extract suite name from metadata labels.
				suiteName := ""
				if summary.Metadata != nil {
					if n, ok := summary.Metadata.Labels["name"]; ok {
						suiteName = n
					}
				}

				suite := &indexstore.Suite{
					SuiteHash:     entry.SuiteHash,
					DiscoveryPath: dp,
					Name:          suiteName,
					TestsTotal:    entry.Tests.TestsTotal,
					IndexedAt:     time.Now().UTC(),
				}

				if uErr := idx.store.UpsertSuite(ctx, suite); uErr != nil {
					idx.log.WithError(uErr).
						WithField("suite_hash", entry.SuiteHash).
						Warn("Failed to upsert suite")
				}
			}
		}
	}

	// Serialize steps stats to JSON.
	stepsJSON := ""
	if entry.Tests != nil && entry.Tests.Steps != nil {
		b, mErr := json.Marshal(entry.Tests.Steps)
		if mErr == nil {
			stepsJSON = string(b)
		}
	}

	// Serialize metadata labels to JSON.
	metadataJSON := ""
	if len(entry.Metadata) > 0 {
		b, mErr := json.Marshal(entry.Metadata)
		if mErr == nil {
			metadataJSON = string(b)
		}
	}

	now := time.Now().UTC()

	run := &indexstore.Run{
		DiscoveryPath:     dp,
		RunID:             runID,
		Timestamp:         entry.Timestamp,
		TimestampEnd:      entry.TimestampEnd,
		SuiteHash:         entry.SuiteHash,
		Status:            entry.Status,
		TerminationReason: entry.TerminationReason,
		HasResult:         len(resultData) > 0,
		InstanceID:        entry.Instance.ID,
		Client:            entry.Instance.Client,
		Image:             entry.Instance.Image,
		RollbackStrategy:  entry.Instance.RollbackStrategy,
		TestsTotal:        entry.Tests.TestsTotal,
		TestsPassed:       entry.Tests.TestsPassed,
		TestsFailed:       entry.Tests.TestsFailed,
		StepsJSON:         stepsJSON,
		MetadataJSON:      metadataJSON,
		IndexedAt:         now,
	}

	if isReindex {
		run.ReindexedAt = &now
	}

	// Serialize DB writes to avoid SQLite BUSY errors under concurrency.
	idx.dbMu.Lock()
	defer idx.dbMu.Unlock()

	if err := idx.store.UpsertRun(ctx, run); err != nil {
		return fmt.Errorf("upserting run: %w", err)
	}

	// Once a run has been indexed from disk, drop any live entry for it so
	// the UI doesn't see both the live and the canonical row at once. We
	// don't fail the whole run on a delete error.
	if err := idx.store.DeleteLiveRun(ctx, dp, runID); err != nil {
		idx.log.WithError(err).WithField("run_id", runID).
			Debug("Failed to delete live run entry")
	}

	// Drop any live log-stream state for this run so lingering UI
	// WebSockets get a clean run_ended signal and the buffer is freed.
	if idx.onLiveRunIndexed != nil {
		idx.onLiveRunIndexed(runID)
	}

	// Index test stats if result.json is present and suite hash is set.
	if len(resultData) > 0 && entry.SuiteHash != "" {
		if err := idx.indexTestStats(
			ctx, entry.SuiteHash, runID, entry, resultData,
		); err != nil {
			idx.log.WithError(err).WithField("run_id", runID).
				Warn("Failed to index test stats")
		}
	}

	// Index block logs if result.block-logs.json is present.
	if len(blockLogsData) > 0 && entry.SuiteHash != "" {
		if err := idx.indexTestStatsBlockLogs(
			ctx, entry.SuiteHash, runID,
			entry.Instance.Client, blockLogsData,
		); err != nil {
			idx.log.WithError(err).WithField("run_id", runID).
				Warn("Failed to index test stats block logs")
		}
	}

	return nil
}

// indexTestStats extracts per-test stats from result.json and bulk-inserts
// them into the store.
func (idx *indexer) indexTestStats(
	ctx context.Context,
	suiteHash, runID string,
	entry *executor.IndexEntry,
	resultData []byte,
) error {
	// Use AccumulateRunResult to extract per-test durations.
	suiteStats := make(executor.SuiteStats)

	run := executor.RunInfo{
		RunID:        runID,
		Client:       entry.Instance.Client,
		Timestamp:    entry.Timestamp,
		TimestampEnd: entry.TimestampEnd,
	}

	executor.AccumulateRunResult(&suiteStats, resultData, run)

	// Collect all stats for bulk insert.
	var testStats []*indexstore.TestStat

	for testName, td := range suiteStats {
		for _, dur := range td.Durations {
			ts := &indexstore.TestStat{
				SuiteHash:    suiteHash,
				TestName:     testName,
				RunID:        runID,
				Client:       dur.Client,
				TotalGasUsed: dur.GasUsed,
				TotalTimeNs:  dur.Time,
				TotalMGasS:   indexstore.ComputeMGasS(dur.GasUsed, dur.Time),
				RunStart:     dur.RunStart,
				RunEnd:       dur.RunEnd,
			}

			if dur.Steps != nil {
				if dur.Steps.Setup != nil {
					ts.SetupGasUsed = dur.Steps.Setup.GasUsed
					ts.SetupTimeNs = dur.Steps.Setup.Time
					ts.SetupMGasS = indexstore.ComputeMGasS(
						dur.Steps.Setup.GasUsed, dur.Steps.Setup.Time,
					)
					ts.SetupRPCCallsCount = dur.Steps.Setup.RPCCallsCount

					if dur.Steps.Setup.ResourceTotals != nil {
						r := dur.Steps.Setup.ResourceTotals
						ts.SetupResourceCPUUsec = r.CPUUsec
						ts.SetupResourceMemDelta = r.MemoryDelta
						ts.SetupResourceMemBytes = r.MemoryBytes
						ts.SetupResourceDiskReadB = r.DiskReadBytes
						ts.SetupResourceDiskWriteB = r.DiskWriteBytes
						ts.SetupResourceDiskReadOps = r.DiskReadIOPS
						ts.SetupResourceDiskWriteOps = r.DiskWriteIOPS
					}
				}

				if dur.Steps.Test != nil {
					ts.TestGasUsed = dur.Steps.Test.GasUsed
					ts.TestTimeNs = dur.Steps.Test.Time
					ts.TestMGasS = indexstore.ComputeMGasS(
						dur.Steps.Test.GasUsed, dur.Steps.Test.Time,
					)
					ts.TestRPCCallsCount = dur.Steps.Test.RPCCallsCount

					if dur.Steps.Test.ResourceTotals != nil {
						r := dur.Steps.Test.ResourceTotals
						ts.TestResourceCPUUsec = r.CPUUsec
						ts.TestResourceMemDelta = r.MemoryDelta
						ts.TestResourceMemBytes = r.MemoryBytes
						ts.TestResourceDiskReadB = r.DiskReadBytes
						ts.TestResourceDiskWriteB = r.DiskWriteBytes
						ts.TestResourceDiskReadOps = r.DiskReadIOPS
						ts.TestResourceDiskWriteOps = r.DiskWriteIOPS
					}
				}
			}

			testStats = append(testStats, ts)
		}
	}

	if err := idx.store.ReplaceTestStats(ctx, runID, testStats); err != nil {
		return fmt.Errorf("replacing test stats: %w", err)
	}

	return nil
}

// blockLogEntry is the per-test JSON shape inside result.block-logs.json.
type blockLogEntry struct {
	Level string `json:"level"`
	Msg   string `json:"msg"`
	Block struct {
		Number  uint64 `json:"number"`
		Hash    string `json:"hash"`
		GasUsed uint64 `json:"gas_used"`
		TxCount int    `json:"tx_count"`
	} `json:"block"`
	Timing struct {
		ExecutionMs float64 `json:"execution_ms"`
		StateReadMs float64 `json:"state_read_ms"`
		StateHashMs float64 `json:"state_hash_ms"`
		CommitMs    float64 `json:"commit_ms"`
		TotalMs     float64 `json:"total_ms"`
	} `json:"timing"`
	Throughput struct {
		MgasPerSec float64 `json:"mgas_per_sec"`
	} `json:"throughput"`
	StateReads struct {
		Accounts     int `json:"accounts"`
		StorageSlots int `json:"storage_slots"`
		Code         int `json:"code"`
		CodeBytes    int `json:"code_bytes"`
	} `json:"state_reads"`
	StateWrites struct {
		Accounts        int `json:"accounts"`
		AccountsDeleted int `json:"accounts_deleted"`
		StorageSlots    int `json:"storage_slots"`
		SlotsDeleted    int `json:"storage_slots_deleted"`
		Code            int `json:"code"`
		CodeBytes       int `json:"code_bytes"`
	} `json:"state_writes"`
	Cache struct {
		Account struct {
			Hits    int     `json:"hits"`
			Misses  int     `json:"misses"`
			HitRate float64 `json:"hit_rate"`
		} `json:"account"`
		Storage struct {
			Hits    int     `json:"hits"`
			Misses  int     `json:"misses"`
			HitRate float64 `json:"hit_rate"`
		} `json:"storage"`
		Code struct {
			Hits      int     `json:"hits"`
			Misses    int     `json:"misses"`
			HitRate   float64 `json:"hit_rate"`
			HitBytes  int     `json:"hit_bytes"`
			MissBytes int     `json:"miss_bytes"`
		} `json:"code"`
	} `json:"cache"`
}

// indexTestStatsBlockLogs extracts per-test block logs from
// result.block-logs.json and bulk-inserts them into the store.
func (idx *indexer) indexTestStatsBlockLogs(
	ctx context.Context,
	suiteHash, runID, client string,
	data []byte,
) error {
	// The file is a map of test name -> single block log entry.
	var testMap map[string]blockLogEntry
	if err := json.Unmarshal(data, &testMap); err != nil {
		return fmt.Errorf("unmarshalling block logs: %w", err)
	}

	logs := make([]*indexstore.TestStatsBlockLog, 0, len(testMap))

	for testName, e := range testMap {
		logs = append(logs, &indexstore.TestStatsBlockLog{
			SuiteHash:                 suiteHash,
			RunID:                     runID,
			TestName:                  testName,
			Client:                    client,
			BlockNumber:               e.Block.Number,
			BlockHash:                 e.Block.Hash,
			BlockGasUsed:              e.Block.GasUsed,
			BlockTxCount:              e.Block.TxCount,
			TimingExecutionMs:         e.Timing.ExecutionMs,
			TimingStateReadMs:         e.Timing.StateReadMs,
			TimingStateHashMs:         e.Timing.StateHashMs,
			TimingCommitMs:            e.Timing.CommitMs,
			TimingTotalMs:             e.Timing.TotalMs,
			ThroughputMgasPerSec:      e.Throughput.MgasPerSec,
			StateReadAccounts:         e.StateReads.Accounts,
			StateReadStorageSlots:     e.StateReads.StorageSlots,
			StateReadCode:             e.StateReads.Code,
			StateReadCodeBytes:        e.StateReads.CodeBytes,
			StateWriteAccounts:        e.StateWrites.Accounts,
			StateWriteAccountsDeleted: e.StateWrites.AccountsDeleted,
			StateWriteStorageSlots:    e.StateWrites.StorageSlots,
			StateWriteSlotsDeleted:    e.StateWrites.SlotsDeleted,
			StateWriteCode:            e.StateWrites.Code,
			StateWriteCodeBytes:       e.StateWrites.CodeBytes,
			CacheAccountHits:          e.Cache.Account.Hits,
			CacheAccountMisses:        e.Cache.Account.Misses,
			CacheAccountHitRate:       e.Cache.Account.HitRate,
			CacheStorageHits:          e.Cache.Storage.Hits,
			CacheStorageMisses:        e.Cache.Storage.Misses,
			CacheStorageHitRate:       e.Cache.Storage.HitRate,
			CacheCodeHits:             e.Cache.Code.Hits,
			CacheCodeMisses:           e.Cache.Code.Misses,
			CacheCodeHitRate:          e.Cache.Code.HitRate,
			CacheCodeHitBytes:         e.Cache.Code.HitBytes,
			CacheCodeMissBytes:        e.Cache.Code.MissBytes,
		})
	}

	if err := idx.store.ReplaceTestStatsBlockLogs(
		ctx, runID, logs,
	); err != nil {
		return fmt.Errorf("replacing test stats block logs: %w", err)
	}

	return nil
}
