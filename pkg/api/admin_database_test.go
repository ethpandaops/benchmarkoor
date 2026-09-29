package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

// TestHandleDatabaseStats_IsLive covers the report shape, and that every
// request builds a fresh one. The report holds no query whose cost grows with
// the per-test tables, so there is nothing to cache.
func TestHandleDatabaseStats_IsLive(t *testing.T) {
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
	assert.Equal(t, int64(100), resp.Stats.NewestRun)
	require.Len(t, resp.Stats.TopSuites, 1)
	assert.Equal(t, "suite-1", resp.Stats.TopSuites[0].SuiteHash)

	require.NoError(t, s.indexStore.UpsertRun(ctx, &indexstore.Run{
		DiscoveryPath: "dp", RunID: "run-2",
		SuiteHash: "suite-1", Timestamp: 200, Status: "completed",
	}))

	fresh := getDatabaseStats(t, s)
	assert.Equal(t, int64(200), fresh.Stats.NewestRun)
	assert.Equal(t, int64(2), fresh.Stats.TopSuites[0].Runs)
}
