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
				count, err := s.indexStore.DeleteStaleLiveRuns(ctx, cutoff)
				if err != nil {
					s.log.WithError(err).Warn("Failed to purge stale live runs")

					continue
				}

				if count > 0 {
					s.log.WithField("deleted", count).
						Info("Purged stale live runs")
				}
			case <-s.done:
				return
			}
		}
	}()
}
