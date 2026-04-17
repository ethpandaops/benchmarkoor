package api

import (
	"context"
	"time"
)

// staleWatcherInterval is how often the stale watcher scans the live_runs
// table. The threshold for considering a row stale comes from
// api.ingest.stale_threshold.
const staleWatcherInterval = 30 * time.Second

// startStaleLiveRunsWatcher launches a background goroutine that
// periodically deletes live_runs rows whose runners have stopped reporting.
// Only started when ingest is configured.
func (s *server) startStaleLiveRunsWatcher(ctx context.Context) {
	if s.cfg.Ingest == nil || s.cfg.Ingest.Token == "" || s.indexStore == nil {
		return
	}

	threshold := s.cfg.Ingest.GetStaleThreshold()

	s.log.WithField("threshold", threshold).Info("Stale live-runs watcher started")

	s.wg.Add(1)

	go func() {
		defer s.wg.Done()

		ticker := time.NewTicker(staleWatcherInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				cutoff := time.Now().UTC().Add(-threshold)
				purgedIDs, err := s.indexStore.DeleteStaleLiveRuns(ctx, cutoff)
				if err != nil {
					s.log.WithError(err).Warn("Failed to purge stale live runs")

					continue
				}

				if len(purgedIDs) > 0 {
					s.log.WithField("deleted", len(purgedIDs)).
						Info("Purged stale live runs")

					// Notify any UI clients still watching a purged
					// run — their runner died and the DB row is gone,
					// so the log panel should show "run ended" rather
					// than hanging on a dead stream.
					if s.wsHub != nil {
						for _, id := range purgedIDs {
							s.wsHub.DropRun(id)
						}
					}
				}

				// Also evict idle log-stream hubs (no runner, no UI,
				// nothing happening past the same cutoff). Their
				// buffers sit in API memory until we drop them.
				if s.wsHub != nil {
					s.wsHub.EvictIdle(cutoff)
				}
			case <-s.done:
				return
			}
		}
	}()
}
