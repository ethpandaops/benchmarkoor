package indexstore_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
)

func TestParseQueryParams_ValidFilters(t *testing.T) {
	allowed := map[string]bool{
		"client": true, "status": true,
		"tests_failed": true, "has_result": true,
	}

	tests := []struct {
		name    string
		raw     url.Values
		wantLen int
	}{
		{
			name:    "eq operator",
			raw:     url.Values{"client": {"eq.geth"}},
			wantLen: 1,
		},
		{
			name:    "neq operator",
			raw:     url.Values{"status": {"neq.running"}},
			wantLen: 1,
		},
		{
			name:    "gt operator",
			raw:     url.Values{"tests_failed": {"gt.5"}},
			wantLen: 1,
		},
		{
			name:    "gte operator",
			raw:     url.Values{"tests_failed": {"gte.10"}},
			wantLen: 1,
		},
		{
			name:    "lt operator",
			raw:     url.Values{"tests_failed": {"lt.3"}},
			wantLen: 1,
		},
		{
			name:    "lte operator",
			raw:     url.Values{"tests_failed": {"lte.0"}},
			wantLen: 1,
		},
		{
			name:    "like operator",
			raw:     url.Values{"client": {"like.ge%"}},
			wantLen: 1,
		},
		{
			name:    "in operator",
			raw:     url.Values{"client": {"in.geth,reth"}},
			wantLen: 1,
		},
		{
			name:    "is null",
			raw:     url.Values{"status": {"is.null"}},
			wantLen: 1,
		},
		{
			name:    "is true",
			raw:     url.Values{"has_result": {"is.true"}},
			wantLen: 1,
		},
		{
			name:    "is false",
			raw:     url.Values{"has_result": {"is.false"}},
			wantLen: 1,
		},
		{
			name: "multiple filters",
			raw: url.Values{
				"client": {"eq.geth"},
				"status": {"eq.completed"},
			},
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, err := indexstore.ParseQueryParams(tt.raw, allowed)
			require.NoError(t, err)
			assert.Len(t, params.Filters, tt.wantLen)
		})
	}
}

func TestParseQueryParams_InvalidColumn(t *testing.T) {
	allowed := map[string]bool{"client": true}

	_, err := indexstore.ParseQueryParams(
		url.Values{"bogus": {"eq.value"}}, allowed,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid filter column")
}

func TestParseQueryParams_InvalidOperator(t *testing.T) {
	allowed := map[string]bool{"client": true}

	_, err := indexstore.ParseQueryParams(
		url.Values{"client": {"nope.value"}}, allowed,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid operator")
}

func TestParseQueryParams_InvalidIsValue(t *testing.T) {
	allowed := map[string]bool{"status": true}

	_, err := indexstore.ParseQueryParams(
		url.Values{"status": {"is.invalid"}}, allowed,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid is value")
}

func TestParseQueryParams_InvalidFilterFormat(t *testing.T) {
	allowed := map[string]bool{"client": true}

	_, err := indexstore.ParseQueryParams(
		url.Values{"client": {"noDot"}}, allowed,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid filter format")
}

func TestParseQueryParams_LimitClamping(t *testing.T) {
	allowed := map[string]bool{"client": true}

	tests := []struct {
		name      string
		raw       url.Values
		wantLimit int
	}{
		{
			name:      "default limit",
			raw:       url.Values{},
			wantLimit: indexstore.DefaultQueryLimit,
		},
		{
			name:      "custom limit",
			raw:       url.Values{"limit": {"50"}},
			wantLimit: 50,
		},
		{
			name:      "clamped to max",
			raw:       url.Values{"limit": {"9999"}},
			wantLimit: indexstore.MaxQueryLimit,
		},
		{
			name:      "zero limit",
			raw:       url.Values{"limit": {"0"}},
			wantLimit: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, err := indexstore.ParseQueryParams(tt.raw, allowed)
			require.NoError(t, err)
			assert.Equal(t, tt.wantLimit, params.Limit)
		})
	}
}

func TestParseQueryParams_InvalidLimit(t *testing.T) {
	allowed := map[string]bool{"client": true}

	_, err := indexstore.ParseQueryParams(
		url.Values{"limit": {"abc"}}, allowed,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid limit")
}

func TestParseQueryParams_InvalidOffset(t *testing.T) {
	allowed := map[string]bool{"client": true}

	_, err := indexstore.ParseQueryParams(
		url.Values{"offset": {"-1"}}, allowed,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid offset")
}

func TestParseQueryParams_SelectValidation(t *testing.T) {
	allowed := map[string]bool{
		"client": true, "status": true, "run_id": true,
	}

	t.Run("valid select", func(t *testing.T) {
		params, err := indexstore.ParseQueryParams(
			url.Values{"select": {"client,status"}}, allowed,
		)
		require.NoError(t, err)
		assert.Equal(t, []string{"client", "status"}, params.Select)
	})

	t.Run("invalid select column", func(t *testing.T) {
		_, err := indexstore.ParseQueryParams(
			url.Values{"select": {"client,bogus"}}, allowed,
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid select column")
	})
}

func TestParseQueryParams_OrderParsing(t *testing.T) {
	allowed := map[string]bool{
		"timestamp": true, "client": true,
	}

	t.Run("single order", func(t *testing.T) {
		params, err := indexstore.ParseQueryParams(
			url.Values{"order": {"timestamp.desc"}}, allowed,
		)
		require.NoError(t, err)
		require.Len(t, params.Orders, 1)
		assert.Equal(t, "timestamp", params.Orders[0].Column)
		assert.Equal(t, "desc", params.Orders[0].Direction)
	})

	t.Run("multiple orders", func(t *testing.T) {
		params, err := indexstore.ParseQueryParams(
			url.Values{"order": {"timestamp.desc,client.asc"}}, allowed,
		)
		require.NoError(t, err)
		require.Len(t, params.Orders, 2)
		assert.Equal(t, "timestamp", params.Orders[0].Column)
		assert.Equal(t, "desc", params.Orders[0].Direction)
		assert.Equal(t, "client", params.Orders[1].Column)
		assert.Equal(t, "asc", params.Orders[1].Direction)
	})

	t.Run("invalid order column", func(t *testing.T) {
		_, err := indexstore.ParseQueryParams(
			url.Values{"order": {"bogus.asc"}}, allowed,
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid order column")
	})

	t.Run("invalid order direction", func(t *testing.T) {
		_, err := indexstore.ParseQueryParams(
			url.Values{"order": {"timestamp.sideways"}}, allowed,
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid order direction")
	})

	t.Run("invalid order format", func(t *testing.T) {
		_, err := indexstore.ParseQueryParams(
			url.Values{"order": {"nodot"}}, allowed,
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid order format")
	})
}

// Integration tests using the real SQLite-backed store.

func TestQueryRuns_Basic(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	runs := []*indexstore.Run{
		{
			DiscoveryPath: "dp/q", RunID: "r-1",
			Timestamp: 100, Status: "completed",
			Client: "geth", TestsFailed: 0, HasResult: true,
		},
		{
			DiscoveryPath: "dp/q", RunID: "r-2",
			Timestamp: 200, Status: "completed",
			Client: "reth", TestsFailed: 5, HasResult: true,
		},
		{
			DiscoveryPath: "dp/q", RunID: "r-3",
			Timestamp: 300, Status: "running",
			Client: "geth", TestsFailed: 2, HasResult: false,
		},
	}
	for _, r := range runs {
		require.NoError(t, s.UpsertRun(ctx, r))
	}

	t.Run("no filters returns all", func(t *testing.T) {
		params := &indexstore.QueryParams{
			Limit: 100,
		}
		result, err := s.QueryRuns(ctx, params)
		require.NoError(t, err)
		assert.Equal(t, int64(3), result.Total)
		assert.Len(t, result.Data, 3)
	})

	t.Run("filter by client", func(t *testing.T) {
		params := &indexstore.QueryParams{
			Filters: []indexstore.Filter{
				{Column: "client", Operator: "eq", Value: "geth"},
			},
			Limit: 100,
		}
		result, err := s.QueryRuns(ctx, params)
		require.NoError(t, err)
		assert.Equal(t, int64(2), result.Total)
	})

	t.Run("filter with in operator", func(t *testing.T) {
		params := &indexstore.QueryParams{
			Filters: []indexstore.Filter{
				{
					Column: "client", Operator: "in",
					Value: "geth,reth",
				},
			},
			Limit: 100,
		}
		result, err := s.QueryRuns(ctx, params)
		require.NoError(t, err)
		assert.Equal(t, int64(3), result.Total)
	})

	t.Run("filter with gt operator", func(t *testing.T) {
		params := &indexstore.QueryParams{
			Filters: []indexstore.Filter{
				{
					Column: "tests_failed", Operator: "gt",
					Value: "1",
				},
			},
			Limit: 100,
		}
		result, err := s.QueryRuns(ctx, params)
		require.NoError(t, err)
		assert.Equal(t, int64(2), result.Total)
	})

	t.Run("order by timestamp desc", func(t *testing.T) {
		params := &indexstore.QueryParams{
			Orders: []indexstore.Order{
				{Column: "timestamp", Direction: "desc"},
			},
			Limit: 100,
		}
		result, err := s.QueryRuns(ctx, params)
		require.NoError(t, err)

		data := result.Data.([]indexstore.RunResponse)
		require.Len(t, data, 3)
		assert.Equal(t, "r-3", data[0].RunID)
		assert.Equal(t, "r-2", data[1].RunID)
		assert.Equal(t, "r-1", data[2].RunID)
	})

	t.Run("pagination", func(t *testing.T) {
		params := &indexstore.QueryParams{
			Orders: []indexstore.Order{
				{Column: "timestamp", Direction: "asc"},
			},
			Limit:  2,
			Offset: 0,
		}
		result, err := s.QueryRuns(ctx, params)
		require.NoError(t, err)
		assert.Equal(t, int64(3), result.Total)
		assert.Len(t, result.Data, 2)
		assert.Equal(t, 2, result.Limit)
		assert.Equal(t, 0, result.Offset)

		// Page 2.
		params.Offset = 2
		result, err = s.QueryRuns(ctx, params)
		require.NoError(t, err)
		assert.Equal(t, int64(3), result.Total)
		assert.Len(t, result.Data, 1)
	})

	t.Run("is false filter", func(t *testing.T) {
		params := &indexstore.QueryParams{
			Filters: []indexstore.Filter{
				{Column: "has_result", Operator: "is", Value: "false"},
			},
			Limit: 100,
		}
		result, err := s.QueryRuns(ctx, params)
		require.NoError(t, err)
		assert.Equal(t, int64(1), result.Total)
	})
}

func TestQueryRuns_StepsJSON(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	run := &indexstore.Run{
		DiscoveryPath: "dp/json", RunID: "r-json",
		Timestamp: 100, Status: "completed",
		Client: "geth", HasResult: true,
		StepsJSON: `{"step1":{"total":5,"passed":5}}`,
	}
	require.NoError(t, s.UpsertRun(ctx, run))

	params := &indexstore.QueryParams{Limit: 100}
	result, err := s.QueryRuns(ctx, params)
	require.NoError(t, err)

	data := result.Data.([]indexstore.RunResponse)
	require.Len(t, data, 1)
	assert.NotNil(t, data[0].StepsJSON)
	assert.Contains(t, string(data[0].StepsJSON), "step1")
}

func TestQueryTestStats_Basic(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	stats := []*indexstore.TestStat{
		{
			SuiteHash: "suite-1", TestName: "TestA",
			RunID: "r-1", Client: "geth",
			TotalGasUsed: 21000, TotalTimeNs: 500000,
		},
		{
			SuiteHash: "suite-1", TestName: "TestB",
			RunID: "r-1", Client: "geth",
			TotalGasUsed: 42000, TotalTimeNs: 750000,
		},
		{
			SuiteHash: "suite-1", TestName: "TestA",
			RunID: "r-2", Client: "reth",
			TotalGasUsed: 21000, TotalTimeNs: 400000,
		},
	}
	for _, ts := range stats {
		require.NoError(t, s.UpsertTestStat(ctx, ts))
	}

	t.Run("no filters returns all", func(t *testing.T) {
		params := &indexstore.QueryParams{Limit: 100}
		result, err := s.QueryTestStats(ctx, params)
		require.NoError(t, err)
		assert.Equal(t, int64(3), result.Total)
	})

	t.Run("filter by client", func(t *testing.T) {
		params := &indexstore.QueryParams{
			Filters: []indexstore.Filter{
				{Column: "client", Operator: "eq", Value: "reth"},
			},
			Limit: 100,
		}
		result, err := s.QueryTestStats(ctx, params)
		require.NoError(t, err)
		assert.Equal(t, int64(1), result.Total)
	})

	t.Run("order by total_time_ns desc", func(t *testing.T) {
		params := &indexstore.QueryParams{
			Orders: []indexstore.Order{
				{Column: "total_time_ns", Direction: "desc"},
			},
			Limit: 100,
		}
		result, err := s.QueryTestStats(ctx, params)
		require.NoError(t, err)

		data := result.Data.([]indexstore.TestStatResponse)
		require.Len(t, data, 3)
		assert.Equal(t, int64(750000), data[0].TotalTimeNs)
		assert.Equal(t, int64(500000), data[1].TotalTimeNs)
		assert.Equal(t, int64(400000), data[2].TotalTimeNs)
	})

	t.Run("filter with in operator", func(t *testing.T) {
		params := &indexstore.QueryParams{
			Filters: []indexstore.Filter{
				{
					Column: "client", Operator: "in",
					Value: "geth,reth",
				},
			},
			Limit: 100,
		}
		result, err := s.QueryTestStats(ctx, params)
		require.NoError(t, err)
		assert.Equal(t, int64(3), result.Total)
	})
}
