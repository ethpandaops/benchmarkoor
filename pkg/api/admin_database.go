package api

import (
	"context"
	"net/http"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
)

// databaseStatsTimeout bounds one report. Every query in it is an index
// lookup or a walk of the runs table, so it finishes in well under a second.
// The timeout exists so that a regression shows up as a failed request, not
// as a scan that holds a read connection for half an hour.
const databaseStatsTimeout = 30 * time.Second

// databaseStatsResponse wraps the store's report with when it was taken.
type databaseStatsResponse struct {
	GatheredAt string                    `json:"gathered_at"`
	Stats      *indexstore.DatabaseStats `json:"stats"`
}

// handleDatabaseStats reports the size and contents of the index database.
//
// The report is built per request and not cached. It avoids every query whose
// cost grows with the per-test tables, so it is cheap enough to run on each
// poll. See indexstore's DatabaseStats.
func (s *server) handleDatabaseStats(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), databaseStatsTimeout)
	defer cancel()

	stats, err := s.indexStore.DatabaseStats(ctx)
	if err != nil {
		s.log.WithError(err).Error("Failed to gather database stats")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	writeJSON(w, http.StatusOK, databaseStatsResponse{
		GatheredAt: formatAdminTime(time.Now().UTC()),
		Stats:      stats,
	})
}
