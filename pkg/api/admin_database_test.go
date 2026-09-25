package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
)

func getDatabaseStats(t *testing.T, s *server) databaseStatsResponse {
	t.Helper()

	rec := httptest.NewRecorder()
	s.handleDatabaseStats(rec, httptest.NewRequest(
		http.MethodGet, "/api/v1/admin/database", nil,
	))
	require.Equal(t, http.StatusOK, rec.Code)

	var resp databaseStatsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	return resp
}

// TestHandleDatabaseStats_ReportsAndCaches covers the report shape and the
// cache. The report counts every row in every table, so it must not run again
// on each poll of the admin page.
func TestHandleDatabaseStats_ReportsAndCaches(t *testing.T) {
	s := newIndexTestServer(t)
	ctx := context.Background()

	require.NoError(t, s.indexStore.UpsertRun(ctx, &indexstore.Run{
		DiscoveryPath: "dp", RunID: "run-1",
		SuiteHash: "suite-1", Timestamp: 100, Status: "completed",
	}))

	resp := getDatabaseStats(t, s)
	require.NotNil(t, resp.Stats)
	assert.NotEmpty(t, resp.GatheredAt)
	assert.Equal(t, "sqlite", resp.Stats.Driver)
	assert.NotEmpty(t, resp.Stats.Tables)
	assert.Equal(t, int64(100), resp.Stats.OldestRun)
	require.Len(t, resp.Stats.TopSuites, 1)
	assert.Equal(t, "suite-1", resp.Stats.TopSuites[0].SuiteHash)

	// A run added now is not visible until the cache expires.
	require.NoError(t, s.indexStore.UpsertRun(ctx, &indexstore.Run{
		DiscoveryPath: "dp", RunID: "run-2",
		SuiteHash: "suite-1", Timestamp: 200, Status: "completed",
	}))

	cached := getDatabaseStats(t, s)
	assert.Equal(t, resp.GatheredAt, cached.GatheredAt)
	assert.Equal(t, int64(100), cached.Stats.NewestRun)

	// Ageing the cache past its TTL lets the next request rebuild.
	stale := time.Now().Add(-2 * databaseStatsTTL)

	s.dbStatsMu.Lock()
	s.dbStatsAt = stale
	s.dbStatsMu.Unlock()

	fresh := getDatabaseStats(t, s)
	assert.Equal(t, int64(200), fresh.Stats.NewestRun, "the report rebuilt")

	// GatheredAt has second resolution, so compare the stamp the cache holds
	// rather than the rendered string, which can repeat within a second.
	s.dbStatsMu.Lock()
	gatheredAt := s.dbStatsAt
	s.dbStatsMu.Unlock()

	assert.True(t, gatheredAt.After(stale), "the cache stamp moved forward")
}
