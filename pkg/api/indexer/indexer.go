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
	"github.com/ethpandaops/benchmarkoor/pkg/executor"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

// defaultConcurrency is the number of runs indexed in parallel when
// no explicit concurrency value is configured.
const defaultConcurrency = 4

// Indexer is a background service that periodically scans storage
// and upserts indexed run/suite data into the index store.
type Indexer interface {
	Start(ctx context.Context) error
	Stop() error
	// RunNow triggers an immediate indexing pass. Returns true if a
	// new pass was kicked off, false if one is already running.
	RunNow() bool
}

// Compile-time interface check.
var _ Indexer = (*indexer)(nil)

type indexer struct {
	log         logrus.FieldLogger
	store       indexstore.Store
	reader      storage.Reader
	interval    time.Duration
	concurrency int
	ctx         context.Context // lifecycle context set by Start
	done        chan struct{}
	wg          sync.WaitGroup
	running     atomic.Bool // prevents overlapping indexing passes
	dbMu        sync.Mutex  // serializes DB writes to avoid SQLite contention
}

// NewIndexer creates a new background indexer.
func NewIndexer(
	log logrus.FieldLogger,
	store indexstore.Store,
	reader storage.Reader,
	interval time.Duration,
	concurrency int,
) Indexer {
	if concurrency <= 0 {
		concurrency = defaultConcurrency
	}

	return &indexer{
		log:         log.WithField("component", "indexer"),
		store:       store,
		reader:      reader,
		interval:    interval,
		concurrency: concurrency,
		done:        make(chan struct{}),
	}
}

// Start launches a background goroutine that runs an immediate indexing
// pass and then ticks at the configured interval. The first pass is
// asynchronous so the caller (the API server) is not blocked.
func (idx *indexer) Start(ctx context.Context) error {
	idx.ctx = ctx

	idx.log.WithFields(logrus.Fields{
		"interval":    idx.interval.String(),
		"concurrency": idx.concurrency,
	}).Info("Starting indexer")

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

// RunNow triggers an immediate indexing pass in a background goroutine.
// It returns true if a new pass was started, false if one is already running.
// The running flag is claimed synchronously via CAS so concurrent callers
// get an accurate answer before the goroutine is scheduled. The pass uses
// the lifecycle context from Start, not a request-scoped context.
func (idx *indexer) RunNow() bool {
	if !idx.running.CompareAndSwap(false, true) {
		return false
	}

	idx.wg.Add(1)

	go func() {
		defer idx.wg.Done()

		idx.runPassInner(idx.ctx)
	}()

	return true
}

// runPass attempts to run one indexing pass if no other pass is active.
// Used by the periodic ticker and initial startup pass.
func (idx *indexer) runPass(ctx context.Context) {
	if !idx.running.CompareAndSwap(false, true) {
		return
	}

	idx.runPassInner(ctx)
}

// runPassInner executes one full indexing pass across all discovery paths.
// The caller must have already set running to true; this method resets it
// on return.
func (idx *indexer) runPassInner(ctx context.Context) {
	defer idx.running.Store(false)

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
// discovery path. It discovers new runs and re-indexes incomplete ones
// using a bounded worker pool for parallel processing.
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

		tasks = append(tasks, runTask{
			runID:          id,
			alreadyIndexed: alreadyIndexed,
		})
	}

	dpLog := idx.log.WithField("discovery_path", dp)

	newCount := 0
	for _, t := range tasks {
		if !t.alreadyIndexed {
			newCount++
		}
	}

	dpLog.WithFields(logrus.Fields{
		"storage_runs":    len(storageIDs),
		"indexed_runs":    len(indexedIDs),
		"new_runs":        newCount,
		"incomplete_runs": len(incompleteIDs),
	}).Info("Scanning discovery path")

	if len(tasks) == 0 {
		return nil
	}

	// Process runs concurrently with bounded parallelism.
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(idx.concurrency)

	var indexed atomic.Int64

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

			if err := idx.indexRun(
				gCtx, dp, task.runID, task.alreadyIndexed,
			); err != nil {
				dpLog.WithError(err).
					WithField("run_id", task.runID).
					Warn("Failed to index run")

				return nil //nolint:nilerr // log and continue
			}

			action := "indexed"
			if task.alreadyIndexed {
				action = "reindexed"
			}

			dpLog.WithField("run_id", task.runID).
				WithField("action", action).
				Info("Indexed run")

			indexed.Add(1)

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return fmt.Errorf("indexing runs: %w", err)
	}

	if count := indexed.Load(); count > 0 {
		dpLog.WithField("count", count).
			Info("Discovery path indexing complete")
	}

	return nil
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

	// Serialize DB writes to avoid SQLite BUSY errors under concurrency.
	idx.dbMu.Lock()
	defer idx.dbMu.Unlock()

	if err := idx.store.UpsertRun(ctx, run); err != nil {
		return fmt.Errorf("upserting run: %w", err)
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
	// Delete old test stats for this run before re-inserting.
	if err := idx.store.DeleteTestStatsForRun(ctx, runID); err != nil {
		return fmt.Errorf("deleting old test stats: %w", err)
	}

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
			stepsJSON := ""
			if dur.Steps != nil {
				b, err := json.Marshal(dur.Steps)
				if err == nil {
					stepsJSON = string(b)
				}
			}

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
				StepsJSON:    stepsJSON,
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

	if err := idx.store.BulkUpsertTestStats(ctx, testStats); err != nil {
		return fmt.Errorf("bulk inserting test stats: %w", err)
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
	// Delete old block logs for this run before re-inserting.
	if err := idx.store.DeleteTestStatsBlockLogsForRun(ctx, runID); err != nil {
		return fmt.Errorf("deleting old test stats block logs: %w", err)
	}

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

	if err := idx.store.BulkInsertTestStatsBlockLogs(ctx, logs); err != nil {
		return fmt.Errorf("bulk inserting test stats block logs: %w", err)
	}

	return nil
}
