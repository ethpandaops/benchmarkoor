package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
	"github.com/ethpandaops/benchmarkoor/pkg/api/storage"
	"github.com/ethpandaops/benchmarkoor/pkg/executor"
	"github.com/sirupsen/logrus"
)

// Indexer is a background service that periodically scans storage
// and upserts indexed run/suite data into the index store.
type Indexer interface {
	Start(ctx context.Context) error
	Stop() error
}

// Compile-time interface check.
var _ Indexer = (*indexer)(nil)

type indexer struct {
	log      logrus.FieldLogger
	store    indexstore.Store
	reader   storage.Reader
	interval time.Duration
	done     chan struct{}
	wg       sync.WaitGroup
}

// NewIndexer creates a new background indexer.
func NewIndexer(
	log logrus.FieldLogger,
	store indexstore.Store,
	reader storage.Reader,
	interval time.Duration,
) Indexer {
	return &indexer{
		log:      log.WithField("component", "indexer"),
		store:    store,
		reader:   reader,
		interval: interval,
		done:     make(chan struct{}),
	}
}

// Start launches a background goroutine that runs an immediate indexing
// pass and then ticks at the configured interval. The first pass is
// asynchronous so the caller (the API server) is not blocked.
func (idx *indexer) Start(ctx context.Context) error {
	idx.log.WithField("interval", idx.interval.String()).
		Info("Starting indexer")

	idx.wg.Add(1)

	go func() {
		defer idx.wg.Done()

		// Run one pass immediately.
		idx.runPass(ctx)

		ticker := time.NewTicker(idx.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				idx.runPass(ctx)
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

// runPass executes one full indexing pass across all discovery paths.
func (idx *indexer) runPass(ctx context.Context) {
	start := time.Now()
	paths := idx.reader.DiscoveryPaths()

	idx.log.WithField("discovery_paths", len(paths)).
		Info("Indexing pass started")

	for _, dp := range paths {
		select {
		case <-ctx.Done():
			return
		case <-idx.done:
			return
		default:
		}

		if err := idx.indexDiscoveryPath(ctx, dp); err != nil {
			idx.log.WithError(err).
				WithField("discovery_path", dp).
				Warn("Indexing pass failed for discovery path")
		}
	}

	idx.log.WithField("duration", time.Since(start).Round(time.Millisecond)).
		Info("Indexing pass completed")
}

// indexDiscoveryPath performs incremental indexing for a single
// discovery path. It discovers new runs and re-indexes incomplete ones.
func (idx *indexer) indexDiscoveryPath(
	ctx context.Context, dp string,
) error {
	// List all run IDs from storage.
	storageIDs, err := idx.reader.ListRunIDs(ctx, dp)
	if err != nil {
		return fmt.Errorf("listing storage run IDs: %w", err)
	}

	// List already-indexed run IDs.
	indexedIDs, err := idx.store.ListRunIDs(ctx, dp)
	if err != nil {
		return fmt.Errorf("listing indexed run IDs: %w", err)
	}

	// List incomplete run IDs that need re-indexing.
	incompleteIDs, err := idx.store.ListIncompleteRunIDs(ctx, dp)
	if err != nil {
		return fmt.Errorf("listing incomplete run IDs: %w", err)
	}

	indexedSet := make(map[string]struct{}, len(indexedIDs))
	for _, id := range indexedIDs {
		indexedSet[id] = struct{}{}
	}

	incompleteSet := make(map[string]struct{}, len(incompleteIDs))
	for _, id := range incompleteIDs {
		incompleteSet[id] = struct{}{}
	}

	newCount := 0
	for _, id := range storageIDs {
		if _, ok := indexedSet[id]; !ok {
			newCount++
		}
	}

	dpLog := idx.log.WithField("discovery_path", dp)

	dpLog.WithFields(logrus.Fields{
		"storage_runs":    len(storageIDs),
		"indexed_runs":    len(indexedIDs),
		"new_runs":        newCount,
		"incomplete_runs": len(incompleteIDs),
	}).Info("Scanning discovery path")

	var indexed int

	for _, runID := range storageIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-idx.done:
			return nil
		default:
		}

		_, alreadyIndexed := indexedSet[runID]
		_, isIncomplete := incompleteSet[runID]

		if alreadyIndexed && !isIncomplete {
			continue
		}

		if err := idx.indexRun(ctx, dp, runID, alreadyIndexed); err != nil {
			idx.log.WithError(err).
				WithField("run_id", runID).
				WithField("discovery_path", dp).
				Warn("Failed to index run")

			continue
		}

		action := "indexed"
		if alreadyIndexed {
			action = "reindexed"
		}

		dpLog.WithField("run_id", runID).
			WithField("action", action).
			Info("Indexed run")

		indexed++
	}

	if indexed > 0 {
		dpLog.WithField("count", indexed).
			Info("Discovery path indexing complete")
	}

	return nil
}

// indexRun reads config.json and optionally result.json for a run,
// builds index models, and upserts them into the store.
func (idx *indexer) indexRun(
	ctx context.Context, dp, runID string, isReindex bool,
) error {
	configData, err := idx.reader.GetRunFile(ctx, dp, runID, "config.json")
	if err != nil {
		return fmt.Errorf("reading config.json: %w", err)
	}

	if configData == nil {
		return fmt.Errorf("config.json not found")
	}

	resultData, err := idx.reader.GetRunFile(ctx, dp, runID, "result.json")
	if err != nil {
		idx.log.WithError(err).WithField("run_id", runID).
			Debug("Failed to read result.json, continuing without it")

		resultData = nil
	}

	// Build an IndexEntry using the existing executor logic.
	entry, err := executor.BuildIndexEntryFromData(
		runID, configData, resultData,
	)
	if err != nil {
		return fmt.Errorf("building index entry: %w", err)
	}

	// Override tests_total from suite summary when available.
	if entry.SuiteHash != "" {
		summaryData, sErr := idx.reader.GetSuiteFile(
			ctx, dp, entry.SuiteHash, "summary.json",
		)
		if sErr == nil && summaryData != nil {
			var summary struct {
				Tests json.RawMessage `json:"tests"`
			}

			if json.Unmarshal(summaryData, &summary) == nil {
				// Count tests by unmarshalling tests array length.
				var tests []json.RawMessage
				if json.Unmarshal(summary.Tests, &tests) == nil &&
					len(tests) > 0 {
					entry.Tests.TestsTotal = len(tests)
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
		IndexedAt:         now,
	}

	if isReindex {
		run.ReindexedAt = &now
	}

	if err := idx.store.UpsertRun(ctx, run); err != nil {
		return fmt.Errorf("upserting run: %w", err)
	}

	// Index test durations if result.json is present and suite hash is set.
	if len(resultData) > 0 && entry.SuiteHash != "" {
		if err := idx.indexTestDurations(
			ctx, entry.SuiteHash, runID, entry, resultData,
		); err != nil {
			idx.log.WithError(err).WithField("run_id", runID).
				Warn("Failed to index test durations")
		}
	}

	return nil
}

// indexTestDurations extracts per-test durations from result.json
// and upserts them into the store.
func (idx *indexer) indexTestDurations(
	ctx context.Context,
	suiteHash, runID string,
	entry *executor.IndexEntry,
	resultData []byte,
) error {
	// Delete old test durations for this run before re-inserting.
	if err := idx.store.DeleteTestDurationsForRun(ctx, runID); err != nil {
		return fmt.Errorf("deleting old test durations: %w", err)
	}

	// Use AccumulateRunResult to extract per-test durations.
	stats := make(executor.SuiteStats)

	run := executor.RunInfo{
		RunID:        runID,
		Client:       entry.Instance.Client,
		Timestamp:    entry.Timestamp,
		TimestampEnd: entry.TimestampEnd,
	}

	executor.AccumulateRunResult(&stats, resultData, run)

	for testName, td := range stats {
		for _, dur := range td.Durations {
			stepsJSON := ""
			if dur.Steps != nil {
				b, err := json.Marshal(dur.Steps)
				if err == nil {
					stepsJSON = string(b)
				}
			}

			d := &indexstore.TestDuration{
				SuiteHash: suiteHash,
				TestName:  testName,
				RunID:     runID,
				Client:    dur.Client,
				GasUsed:   dur.GasUsed,
				TimeNs:    dur.Time,
				RunStart:  dur.RunStart,
				RunEnd:    dur.RunEnd,
				StepsJSON: stepsJSON,
			}

			if err := idx.store.UpsertTestDuration(ctx, d); err != nil {
				return fmt.Errorf(
					"upserting test duration for %q: %w", testName, err,
				)
			}
		}
	}

	return nil
}
