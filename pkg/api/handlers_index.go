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

// handleIndex returns the aggregated index of all benchmark runs from all
// discovery paths. The response shape matches executor.Index with an
// additional "discovery_path" field on each entry.
func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	runs, err := s.indexStore.ListAllRuns(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"listing runs: " + err.Error()})

		return
	}

	type indexEntryWithDP struct {
		DiscoveryPath string `json:"discovery_path"`
		*executor.IndexEntry
	}

	entries := make([]indexEntryWithDP, 0, len(runs))

	for i := range runs {
		run := &runs[i]

		// Unmarshal steps JSON back to the struct.
		var steps *executor.IndexStepsStats
		if run.StepsJSON != "" {
			var s executor.IndexStepsStats
			if json.Unmarshal([]byte(run.StepsJSON), &s) == nil {
				steps = &s
			}
		}

		entry := &executor.IndexEntry{
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
			Tests: &executor.IndexTestStats{
				TestsTotal:  run.TestsTotal,
				TestsPassed: run.TestsPassed,
				TestsFailed: run.TestsFailed,
				Steps:       steps,
			},
		}

		if entry.Tests.Steps == nil {
			entry.Tests.Steps = &executor.IndexStepsStats{}
		}

		if run.MetadataJSON != "" {
			var m map[string]string
			if json.Unmarshal([]byte(run.MetadataJSON), &m) == nil {
				entry.Metadata = m
			}
		}

		entries = append(entries, indexEntryWithDP{
			DiscoveryPath: run.DiscoveryPath,
			IndexEntry:    entry,
		})
	}

	// Sort by timestamp descending.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp > entries[j].Timestamp
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"generated": time.Now().Unix(),
		"entries":   entries,
	})
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

	durations, err := s.indexStore.ListTestStatsBySuiteRecent(
		r.Context(), suiteHash, maxRuns,
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
