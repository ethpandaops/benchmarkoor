package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
	"github.com/ethpandaops/benchmarkoor/pkg/executor"
	"github.com/go-chi/chi/v5"
)

// emptyJSONObject is spliced in for runs that have no (or malformed) steps
// JSON, preserving the historical behaviour where "steps" is always present.
var emptyJSONObject = json.RawMessage("{}")

// indexResponse is the /index payload. Field order mirrors the historical
// shape (discovery_path first, then the executor.IndexEntry fields).
type indexResponse struct {
	Generated int64        `json:"generated"`
	Entries   []indexEntry `json:"entries"`
}

type indexEntry struct {
	DiscoveryPath     string                  `json:"discovery_path"`
	RunID             string                  `json:"run_id"`
	Timestamp         int64                   `json:"timestamp"`
	TimestampEnd      int64                   `json:"timestamp_end,omitempty"`
	SuiteHash         string                  `json:"suite_hash,omitempty"`
	Instance          *executor.IndexInstance `json:"instance"`
	Tests             indexTestStats          `json:"tests"`
	Status            string                  `json:"status,omitempty"`
	TerminationReason string                  `json:"termination_reason,omitempty"`
	Metadata          json.RawMessage         `json:"metadata,omitempty"`
}

type indexTestStats struct {
	TestsTotal  int             `json:"tests_total"`
	TestsPassed int             `json:"tests_passed"`
	TestsFailed int             `json:"tests_failed"`
	Steps       json.RawMessage `json:"steps"`
}

// handleIndex returns the aggregated index of all benchmark runs from all
// discovery paths. The response shape matches executor.Index with an
// additional "discovery_path" field on each entry.
//
// The full payload is O(number of runs) and the UI polls it periodically, so
// the marshaled body is cached and keyed by the store's runs generation: while
// no run is upserted or deleted the response is served straight from the cache
// without touching the database.
func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	gen := s.indexStore.RunsGeneration()

	if body := s.cachedIndex(gen); body != nil {
		writeRawJSON(w, body)

		return
	}

	runs, err := s.indexStore.ListAllRuns(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"listing runs: " + err.Error()})

		return
	}

	// ListAllRuns already orders by timestamp descending, so no re-sort here.
	entries := make([]indexEntry, 0, len(runs))

	for i := range runs {
		run := &runs[i]

		// Splice the stored steps/metadata JSON straight through instead of
		// unmarshaling then re-marshaling. Malformed blobs fall back to the
		// historical defaults ("{}" for steps, omitted for metadata).
		steps := json.RawMessage(run.StepsJSON)
		if len(steps) == 0 || !json.Valid(steps) {
			steps = emptyJSONObject
		}

		var metadata json.RawMessage
		if run.MetadataJSON != "" && json.Valid([]byte(run.MetadataJSON)) {
			metadata = json.RawMessage(run.MetadataJSON)
		}

		entries = append(entries, indexEntry{
			DiscoveryPath:     run.DiscoveryPath,
			RunID:             run.RunID,
			Timestamp:         run.Timestamp,
			TimestampEnd:      run.TimestampEnd,
			SuiteHash:         run.SuiteHash,
			Status:            run.Status,
			TerminationReason: run.TerminationReason,
			Instance: &executor.IndexInstance{
				ID:               run.InstanceID,
				Client:           run.Client,
				Image:            run.Image,
				RollbackStrategy: run.RollbackStrategy,
			},
			Tests: indexTestStats{
				TestsTotal:  run.TestsTotal,
				TestsPassed: run.TestsPassed,
				TestsFailed: run.TestsFailed,
				Steps:       steps,
			},
			Metadata: metadata,
		})
	}

	body, err := json.Marshal(indexResponse{
		Generated: time.Now().Unix(),
		Entries:   entries,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"encoding index: " + err.Error()})

		return
	}

	s.storeCachedIndex(gen, body)
	writeRawJSON(w, body)
}

// cachedIndex returns the cached marshaled index body if it was built for the
// given runs generation, otherwise nil.
func (s *server) cachedIndex(gen uint64) []byte {
	s.indexCacheMu.Lock()
	defer s.indexCacheMu.Unlock()

	if s.indexCacheBody != nil && s.indexCacheGen == gen {
		return s.indexCacheBody
	}

	return nil
}

// storeCachedIndex records a freshly built index body for the given
// generation. A build is only allowed to replace the cache if its generation
// is at least as fresh, so a slow build for an older generation can't clobber
// a newer cached body.
func (s *server) storeCachedIndex(gen uint64, body []byte) {
	s.indexCacheMu.Lock()
	defer s.indexCacheMu.Unlock()

	if s.indexCacheBody == nil || gen >= s.indexCacheGen {
		s.indexCacheGen = gen
		s.indexCacheBody = body
	}
}

// handleSuiteStats returns suite statistics for a given suite hash.
// The response shape matches executor.SuiteStats (map[string]*TestDurations).
func (s *server) handleSuiteStats(w http.ResponseWriter, r *http.Request) {
	suiteHash := chi.URLParam(r, "hash")
	if suiteHash == "" {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"suite hash is required"})

		return
	}

	// Parse max_runs_per_client: default 30, clamp to [1, 200].
	maxRuns := 30
	if v := r.URL.Query().Get("max_runs_per_client"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			maxRuns = n
		}
	}

	maxRuns = max(1, min(200, maxRuns))

	// Baseline calibration tests are excluded from stats by default;
	// pass include_baseline=true for the raw view.
	includeBaseline := r.URL.Query().Get("include_baseline") == "true"

	durations, err := s.indexStore.ListTestStatsBySuiteRecent(
		r.Context(), suiteHash, maxRuns, includeBaseline,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"listing test stats: " + err.Error()})

		return
	}

	// Group by test name and build the SuiteStats shape.
	stats := make(executor.SuiteStats, len(durations))

	for i := range durations {
		d := &durations[i]

		steps := &executor.RunDurationStepsStats{
			Setup: &executor.RunDurationStepStats{
				GasUsed:       d.SetupGasUsed,
				Time:          d.SetupTimeNs,
				RPCCallsCount: d.SetupRPCCallsCount,
				ResourceTotals: &executor.ResourceTotals{
					CPUUsec:        d.SetupResourceCPUUsec,
					MemoryDelta:    d.SetupResourceMemDelta,
					MemoryBytes:    d.SetupResourceMemBytes,
					DiskReadBytes:  d.SetupResourceDiskReadB,
					DiskWriteBytes: d.SetupResourceDiskWriteB,
					DiskReadIOPS:   d.SetupResourceDiskReadOps,
					DiskWriteIOPS:  d.SetupResourceDiskWriteOps,
				},
			},
			Test: &executor.RunDurationStepStats{
				GasUsed:       d.TestGasUsed,
				Time:          d.TestTimeNs,
				RPCCallsCount: d.TestRPCCallsCount,
				ResourceTotals: &executor.ResourceTotals{
					CPUUsec:        d.TestResourceCPUUsec,
					MemoryDelta:    d.TestResourceMemDelta,
					MemoryBytes:    d.TestResourceMemBytes,
					DiskReadBytes:  d.TestResourceDiskReadB,
					DiskWriteBytes: d.TestResourceDiskWriteB,
					DiskReadIOPS:   d.TestResourceDiskReadOps,
					DiskWriteIOPS:  d.TestResourceDiskWriteOps,
				},
			},
		}

		rd := &executor.RunDuration{
			ID:       d.RunID,
			Client:   d.Client,
			GasUsed:  d.TotalGasUsed,
			Time:     d.TotalTimeNs,
			RunStart: d.RunStart,
			RunEnd:   d.RunEnd,
			Steps:    steps,
		}

		if stats[d.TestName] == nil {
			stats[d.TestName] = &executor.TestDurations{
				Durations: make([]*executor.RunDuration, 0, 4),
			}
		}

		stats[d.TestName].Durations = append(
			stats[d.TestName].Durations, rd,
		)
	}

	// Sort durations within each test by time_ns descending.
	for _, td := range stats {
		sort.Slice(td.Durations, func(i, j int) bool {
			return td.Durations[i].Time > td.Durations[j].Time
		})
	}

	writeJSON(w, http.StatusOK, stats)
}

// handleQueryRuns handles PostgREST-style queries against the runs table.
func (s *server) handleQueryRuns(w http.ResponseWriter, r *http.Request) {
	params, err := indexstore.ParseQueryParams(
		r.URL.Query(), indexstore.AllowedRunColumns(),
	)
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{err.Error()})

		return
	}

	params.CountExact = strings.Contains(
		r.Header.Get("Prefer"), "count=exact",
	)

	result, err := s.indexStore.QueryRuns(r.Context(), params)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"querying runs: " + err.Error()})

		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleQueryTestStats handles PostgREST-style queries against the
// test_stats table.
func (s *server) handleQueryTestStats(
	w http.ResponseWriter, r *http.Request,
) {
	params, err := indexstore.ParseQueryParams(
		r.URL.Query(), indexstore.AllowedTestStatColumns(),
	)
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{err.Error()})

		return
	}

	params.CountExact = strings.Contains(
		r.Header.Get("Prefer"), "count=exact",
	)

	result, err := s.indexStore.QueryTestStats(r.Context(), params)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"querying test stats: " + err.Error()})

		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleQuerySuites handles PostgREST-style queries against the suites
// table.
func (s *server) handleQuerySuites(
	w http.ResponseWriter, r *http.Request,
) {
	params, err := indexstore.ParseQueryParams(
		r.URL.Query(), indexstore.AllowedSuiteColumns(),
	)
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{err.Error()})

		return
	}

	params.CountExact = strings.Contains(
		r.Header.Get("Prefer"), "count=exact",
	)

	result, err := s.indexStore.QuerySuites(r.Context(), params)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"querying suites: " + err.Error()})

		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleQueryTestStatsBlockLogs handles PostgREST-style queries against
// the test_stats_block_logs table.
func (s *server) handleQueryTestStatsBlockLogs(
	w http.ResponseWriter, r *http.Request,
) {
	params, err := indexstore.ParseQueryParams(
		r.URL.Query(), indexstore.AllowedTestStatsBlockLogColumns(),
	)
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{err.Error()})

		return
	}

	params.CountExact = strings.Contains(
		r.Header.Get("Prefer"), "count=exact",
	)

	result, err := s.indexStore.QueryTestStatsBlockLogs(
		r.Context(), params,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{
				"querying test stats block logs: " + err.Error(),
			})

		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleRunIndexer triggers an immediate indexing pass. It returns 409 if
// an indexing pass is already in progress.
func (s *server) handleRunIndexer(w http.ResponseWriter, r *http.Request) {
	if s.indexer == nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"indexing is not enabled"})

		return
	}

	if started := s.indexer.RunNow(); !started {
		writeJSON(w, http.StatusConflict, map[string]string{
			"status":  "already_running",
			"message": "Indexing pass already in progress",
		})

		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status":  "started",
		"message": "Indexing pass started",
	})
}
