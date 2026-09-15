package api

import (
	"context"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
	"github.com/sirupsen/logrus"
)

// runDeleterInterval is how often the run deleter re-scans the deletion
// queue when no request woke it. It bounds the retry delay after a failed
// deletion attempt.
const runDeleterInterval = 30 * time.Second

// startRunDeleter launches the background goroutine that drains the run
// deletion queue. Runs are deleted one at a time in the order they were
// queued: storage objects first, then the index rows. A run whose deletion
// fails keeps its place in the queue and is retried on the next pass.
//
// The queue lives in the index database, so runs queued before a restart
// are picked up by the first pass. No-op when the storage backend cannot
// delete or indexing is disabled.
func (s *server) startRunDeleter(ctx context.Context) {
	if s.storageDeleter == nil || s.indexStore == nil {
		return
	}

	s.log.WithField("interval", runDeleterInterval).
		Info("Run deleter started")

	s.wg.Add(1)

	go func() {
		defer s.wg.Done()

		// Cancel an in-flight deletion on shutdown so Stop does not wait
		// for a large storage delete. The run stays queued and the next
		// process resumes it.
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		go func() {
			select {
			case <-s.done:
				cancel()
			case <-ctx.Done():
			}
		}()

		ticker := time.NewTicker(runDeleterInterval)
		defer ticker.Stop()

		// Drain whatever a previous process left queued.
		s.drainDeletionQueue(ctx)

		for {
			select {
			case <-s.runDeleterKick:
			case <-ticker.C:
			case <-ctx.Done():
				return
			}

			s.drainDeletionQueue(ctx)
		}
	}()
}

// kickRunDeleter wakes the run deleter without blocking. The kick channel
// has a buffer of one, so several requests collapse into a single pass.
func (s *server) kickRunDeleter() {
	select {
	case s.runDeleterKick <- struct{}{}:
	default:
	}
}

// drainDeletionQueue deletes every queued run in queue order. It stops
// early when the context is cancelled.
func (s *server) drainDeletionQueue(ctx context.Context) {
	runs, err := s.indexStore.ListRunsPendingDeletion(ctx)
	if err != nil {
		s.log.WithError(err).Warn("Failed to list runs pending deletion")

		return
	}

	if len(runs) == 0 {
		return
	}

	s.log.WithField("queued", len(runs)).Info("Draining run deletion queue")

	for i := range runs {
		select {
		case <-ctx.Done():
			return
		default:
		}

		s.deleteQueuedRun(ctx, &runs[i])
	}
}

// deleteQueuedRun removes one queued run from storage and then from the
// index. Storage goes first so the indexer cannot re-discover the run
// between the two steps. A failure is recorded on the run and the run
// stays queued.
func (s *server) deleteQueuedRun(ctx context.Context, run *indexstore.Run) {
	log := s.log.WithFields(logrus.Fields{
		"run_id":         run.RunID,
		"discovery_path": run.DiscoveryPath,
	})

	if err := s.storageDeleter.DeleteRun(
		ctx, run.DiscoveryPath, run.RunID,
	); err != nil {
		log.WithError(err).Error("Failed to delete run from storage")
		s.recordDeletionError(ctx, run.RunID, "storage delete failed: "+err.Error())

		return
	}

	// Transactional: test_stats, block_logs, run, orphaned suite —
	// all or nothing.
	if err := s.indexStore.DeleteRunCascade(ctx, run.RunID); err != nil {
		log.WithError(err).Error("Failed to delete run from index")
		s.recordDeletionError(ctx, run.RunID, "index delete failed: "+err.Error())

		return
	}

	log.Info("Deleted run")
}

// recordDeletionError stores the failure on the queued run. Cancellation
// is not recorded: the run was not attempted, it was interrupted.
func (s *server) recordDeletionError(ctx context.Context, runID, msg string) {
	if ctx.Err() != nil {
		return
	}

	if err := s.indexStore.SetRunDeletionError(ctx, runID, msg); err != nil {
		s.log.WithError(err).WithField("run_id", runID).
			Warn("Failed to record run deletion error")
	}
}
