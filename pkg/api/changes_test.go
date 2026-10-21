package api

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
)

func points(values ...float64) []seriesPoint {
	out := make([]seriesPoint, len(values))
	for i, v := range values {
		out[i] = seriesPoint{runID: fmt.Sprintf("run-%d", i), runStart: int64(i), value: v}
	}

	return out
}

// repeat returns n copies of v.
func repeat(v float64, n int) []float64 {
	return slices.Repeat([]float64{v}, n)
}

// The same numbers as the compareWithBaseline tests of the UI, so both
// implementations of the rule give the same result.
func TestCompareWithBaseline(t *testing.T) {
	t.Run("latest run", func(t *testing.T) {
		step, baseline, ok := compareWithBaseline(points(90, 100, 110, 100, 80))
		require.True(t, ok)

		assert.Equal(t, "run-4", step.RunID)
		assert.Equal(t, 1, step.Runs)
		assert.InDelta(t, 80, step.Median, 1e-9)
		assert.InDelta(t, 100, baseline.Median, 1e-9)
		assert.Equal(t, 4, baseline.Runs)
		assert.InDelta(t, -20, baseline.Change, 1e-9)
		assert.InDelta(t, 7.413, baseline.Noise, 1e-3)

		_, _, ok = compareWithBaseline(points(100, 100, 50))
		assert.False(t, ok, "three earlier runs are needed")
	})

	t.Run("an old step stays found", func(t *testing.T) {
		// 20 runs at 100, then 10 runs at 70 with a little noise.
		values := append(repeat(100, 20), 70, 71, 69, 70, 72, 70, 68, 70, 71, 70)

		step, baseline, ok := compareWithBaseline(points(values...))
		require.True(t, ok)

		assert.Equal(t, "run-20", step.RunID)
		assert.Equal(t, "run-19", baseline.LastRunID)
		assert.Equal(t, int64(19), baseline.LastRunStart)
		assert.Equal(t, 10, step.Runs)
		assert.InDelta(t, 70, step.Median, 1e-9)
		assert.InDelta(t, 100, baseline.Median, 1e-9)
		assert.Equal(t, 10, baseline.Runs, "only the 10 runs before the step")
		assert.InDelta(t, -30, baseline.Change, 1e-9)
	})

	t.Run("a step after a step", func(t *testing.T) {
		// Up from 100 to 150, then down to 120: the latest level is 120,
		// compared with 150.
		values := append(append(repeat(100, 10), repeat(150, 15)...), repeat(120, 5)...)

		step, baseline, ok := compareWithBaseline(points(values...))
		require.True(t, ok)

		assert.Equal(t, "run-25", step.RunID)
		assert.InDelta(t, 150, baseline.Median, 1e-9)
		assert.InDelta(t, -20, baseline.Change, 1e-9)
	})

	t.Run("a fixed regression", func(t *testing.T) {
		// Down to 70 for 10 runs, then back at 100: the latest level is the
		// fix, an improvement against 70.
		values := append(append(repeat(100, 15), repeat(70, 10)...), repeat(100, 5)...)

		step, baseline, ok := compareWithBaseline(points(values...))
		require.True(t, ok)

		assert.Equal(t, "run-25", step.RunID)
		assert.InDelta(t, 70, baseline.Median, 1e-9)
		assert.Positive(t, baseline.Change)
	})

	t.Run("noise is no step", func(t *testing.T) {
		values := []float64{100, 103, 98, 101, 97, 102, 99, 104, 100, 96, 101, 99}

		step, baseline, ok := compareWithBaseline(points(values...))
		require.True(t, ok)

		assert.Equal(t, 1, step.Runs, "the latest run against the runs before it")
		assert.Equal(t, 10, baseline.Runs)
		assert.Less(t, math.Abs(baseline.Change), 5.0)
	})
}

func TestParseChangeSteps(t *testing.T) {
	assert.Equal(t, "test", parseChangeSteps("").String())
	assert.Equal(t, "test", parseChangeSteps("cleanup").String())
	assert.Equal(t, "setup", parseChangeSteps("setup").String())
	assert.Equal(t, "setup,test", parseChangeSteps("test,setup,cleanup").String())
}

// stat is one run of a test with 100M gas in the test step that takes timeNs.
func stat(test, client string, runStart, timeNs, rejectedNs int64) indexstore.TestChangeStat {
	return indexstore.TestChangeStat{
		TestName:           test,
		Client:             client,
		RunID:              fmt.Sprintf("%s-%d", client, runStart),
		RunStart:           runStart,
		SetupGasUsed:       50_000_000,
		SetupTimeNs:        1_000_000_000,
		TestGasUsed:        100_000_000,
		TestTimeNs:         timeNs,
		TestRejectedTimeNs: rejectedNs,
	}
}

func TestBuildSuiteChanges(t *testing.T) {
	var stats []indexstore.TestChangeStat

	// geth: 1s per run, then 2s in the latest run, so 100 to 50 MGas/s.
	for i, timeNs := range []int64{1e9, 1e9, 1e9, 1e9, 2e9} {
		stats = append(stats, stat("mgas_test", "geth", int64(i+1), timeNs, 0))
	}

	// reth has too few runs for a baseline.
	for i := range 3 {
		stats = append(stats, stat("mgas_test", "reth", int64(i+1), 1e9, 0))
	}

	// An invalid-payload test is compared on the time to reject.
	for i, rejectedNs := range []int64{1000, 1000, 1000, 1000, 1900} {
		// The client reports no gas for the invalid payload, so no step time.
		stats = append(stats, stat("invalid_test", "geth", int64(i+1), 0, rejectedNs))
	}

	resp := buildSuiteChanges(stats, parseChangeSteps("test"))
	require.Len(t, resp.Tests, 2)

	invalid, mgas := resp.Tests[0], resp.Tests[1]

	assert.Equal(t, "invalid_test", invalid.Name)
	assert.Equal(t, changeMetricRejected, invalid.Metric)
	require.Len(t, invalid.Clients, 1)
	assert.InDelta(t, 1900, invalid.Clients[0].Step.Median, 1e-9)
	assert.InDelta(t, 90, invalid.Clients[0].Baseline.Change, 1e-9)
	// The min. block time compares with the time to reject.
	assert.InDelta(t, 1900, invalid.Clients[0].Step.DurationNs, 1e-9)
	assert.InDelta(t, 1000, invalid.Clients[0].Baseline.DurationNs, 1e-9)

	assert.Equal(t, "mgas_test", mgas.Name)
	assert.Equal(t, changeMetricMGas, mgas.Metric)
	require.Len(t, mgas.Clients, 1, "reth has too few runs")

	geth := mgas.Clients[0]
	assert.Equal(t, "geth", geth.Client)
	assert.Equal(t, "geth-5", geth.Step.RunID)
	assert.InDelta(t, 50, geth.Step.Median, 1e-9)
	assert.InDelta(t, 100, geth.Baseline.Median, 1e-9)
	assert.InDelta(t, -50, geth.Baseline.Change, 1e-9)
	assert.InDelta(t, 2e9, geth.Step.DurationNs, 1e-9)
	assert.InDelta(t, 1e9, geth.Baseline.DurationNs, 1e-9)

	// With the setup step, each run has 50M more gas and 1s more time.
	withSetup := buildSuiteChanges(stats, parseChangeSteps("setup,test"))
	geth = withSetup.Tests[1].Clients[0]
	assert.InDelta(t, 50, geth.Step.Median, 1e-9, "150M gas in 3s")
	assert.InDelta(t, 75, geth.Baseline.Median, 1e-9, "150M gas in 2s")
	assert.InDelta(t, 3e9, geth.Step.DurationNs, 1e-9)
}

func TestHandleSuiteChanges(t *testing.T) {
	s := newIndexTestServer(t)
	ctx := context.Background()

	addRun := func(i int, timeNs int64) {
		runID := fmt.Sprintf("run-%d", i)
		require.NoError(t, s.indexStore.UpsertRun(ctx, &indexstore.Run{
			DiscoveryPath: "dp",
			RunID:         runID,
			Timestamp:     int64(i),
			SuiteHash:     "suite",
			Status:        "completed",
			Client:        "geth",
		}))
		require.NoError(t, s.indexStore.ReplaceTestStats(ctx, runID, []*indexstore.TestStat{{
			SuiteHash: "suite", RunID: runID, TestName: "t1", Client: "geth",
			RunStart: int64(i), TestGasUsed: 100_000_000, TestTimeNs: timeNs,
		}}))
	}

	call := func(query string) suiteChangesResponse {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("hash", "suite")
		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/index/suites/suite/changes"+query, nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		s.handleSuiteChanges(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var resp suiteChangesResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

		return resp
	}

	// 34 runs: only the latest 30 are read, so the old slow runs do not count.
	for i := 1; i <= 34; i++ {
		timeNs := int64(1e9)
		if i <= 4 {
			timeNs = 10e9
		}

		addRun(i, timeNs)
	}

	resp := call("")
	require.Len(t, resp.Tests, 1)
	geth := resp.Tests[0].Clients[0]
	assert.Equal(t, "run-34", geth.Step.RunID)
	assert.Equal(t, 10, geth.Baseline.Runs)
	assert.InDelta(t, 0, geth.Baseline.Change, 1e-9)

	// A window of 50 reads the slow runs too, and finds the step after them.
	geth = call("?window=50").Tests[0].Clients[0]
	assert.Equal(t, "run-5", geth.Step.RunID)
	assert.Equal(t, 30, geth.Step.Runs)
	assert.InDelta(t, 900, geth.Baseline.Change, 1e-9)

	// A second call is served from the cache.
	require.Len(t, s.changeCache, 2)
	assert.Equal(t, resp, call(""))
	require.Len(t, s.changeCache, 2)

	// A new run changes the key, so the response is never stale. Two slow
	// runs are a step that the next call still finds.
	addRun(35, 2e9)
	addRun(36, 2e9)

	geth = call("").Tests[0].Clients[0]
	assert.Equal(t, "run-35", geth.Step.RunID)
	assert.Equal(t, 2, geth.Step.Runs)
	assert.InDelta(t, -50, geth.Baseline.Change, 1e-9)

	// Other steps are a different response, and so is an unknown window,
	// which falls back to the default.
	call("?steps=setup,test")
	assert.Len(t, s.changeCache, 4)
	assert.Equal(t, call(""), call("?window=7"))

	// The cached gzip copy is served as is, and a known ETag gets a 304.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("hash", "suite")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/index/suites/suite/changes", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Header.Set("Accept-Encoding", "gzip")

	rec := httptest.NewRecorder()
	s.handleSuiteChanges(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))

	reader, err := gzip.NewReader(rec.Body)
	require.NoError(t, err)

	plain, err := io.ReadAll(reader)
	require.NoError(t, err)

	var gzipped suiteChangesResponse
	require.NoError(t, json.Unmarshal(plain, &gzipped))
	assert.Equal(t, "run-35", gzipped.Tests[0].Clients[0].Step.RunID)

	req.Header.Set("If-None-Match", rec.Header().Get("ETag"))

	rec = httptest.NewRecorder()
	s.handleSuiteChanges(rec, req)
	assert.Equal(t, http.StatusNotModified, rec.Code)
	assert.Zero(t, rec.Body.Len())

	// A re-index rewrites the stats of runs but keeps the run list. Stats of
	// another suite do not touch this suite's cache.
	entries := len(s.changeCache)

	require.NoError(t, s.indexStore.ReplaceTestStats(ctx, "other-run", []*indexstore.TestStat{{
		SuiteHash: "other", RunID: "other-run", TestName: "t1", Client: "geth",
	}}))
	call("")
	assert.Len(t, s.changeCache, entries, "served from the cache")

	for _, i := range []int{35, 36} {
		runID := fmt.Sprintf("run-%d", i)
		require.NoError(t, s.indexStore.ReplaceTestStats(ctx, runID, []*indexstore.TestStat{{
			SuiteHash: "suite", RunID: runID, TestName: "t1", Client: "geth",
			RunStart: int64(i), TestGasUsed: 100_000_000, TestTimeNs: 1e9,
		}}))
	}

	geth = call("").Tests[0].Clients[0]
	assert.Equal(t, "run-36", geth.Step.RunID)
	assert.InDelta(t, 0, geth.Baseline.Change, 1e-9)
}
