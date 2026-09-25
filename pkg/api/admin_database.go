package api

import (
	"net/http"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
)

// databaseStatsTTL is how long a gathered report is reused. The report runs a
// full row count over every table, which on a large index database is seconds
// of work on a CPU-limited pod. It answers "is a cleanup due", a question
// whose answer does not change minute to minute, so the TTL is comfortably
// longer than the UI's poll interval and most polls cost nothing.
const databaseStatsTTL = 5 * time.Minute

// databaseStatsResponse wraps the store's report with when it was taken, so
// the UI can say how fresh it is rather than implying it is live.
type databaseStatsResponse struct {
	GatheredAt string                    `json:"gathered_at"`
	Stats      *indexstore.DatabaseStats `json:"stats"`
}

// handleDatabaseStats reports the size and contents of the index database.
func (s *server) handleDatabaseStats(w http.ResponseWriter, r *http.Request) {
	if stats, at := s.cachedDatabaseStats(); stats != nil {
		writeJSON(w, http.StatusOK, databaseStatsResponse{
			GatheredAt: formatAdminTime(at),
			Stats:      stats,
		})

		return
	}

	// Only one gather at a time. Each one is a full scan of every table, and
	// the read pool holds four connections: two admin tabs open at once would
	// otherwise start two scans and starve /index of readers for the seconds
	// they take. A caller that waits here finds the fresh report on re-check.
	s.dbStatsGatherMu.Lock()
	defer s.dbStatsGatherMu.Unlock()

	if stats, at := s.cachedDatabaseStats(); stats != nil {
		writeJSON(w, http.StatusOK, databaseStatsResponse{
			GatheredAt: formatAdminTime(at),
			Stats:      stats,
		})

		return
	}

	stats, err := s.indexStore.DatabaseStats(r.Context())
	if err != nil {
		s.log.WithError(err).Error("Failed to gather database stats")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	at := time.Now().UTC()
	s.storeDatabaseStats(stats, at)

	writeJSON(w, http.StatusOK, databaseStatsResponse{
		GatheredAt: formatAdminTime(at),
		Stats:      stats,
	})
}

// cachedDatabaseStats returns the last report while it is still fresh.
func (s *server) cachedDatabaseStats() (*indexstore.DatabaseStats, time.Time) {
	s.dbStatsMu.Lock()
	defer s.dbStatsMu.Unlock()

	if s.dbStats == nil || time.Since(s.dbStatsAt) > databaseStatsTTL {
		return nil, time.Time{}
	}

	return s.dbStats, s.dbStatsAt
}

// storeDatabaseStats records a freshly gathered report.
func (s *server) storeDatabaseStats(
	stats *indexstore.DatabaseStats, at time.Time,
) {
	s.dbStatsMu.Lock()
	defer s.dbStatsMu.Unlock()

	s.dbStats = stats
	s.dbStatsAt = at
}
