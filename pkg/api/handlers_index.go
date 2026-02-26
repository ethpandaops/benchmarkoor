package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

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

	durations, err := s.indexStore.ListTestDurationsBySuite(
		r.Context(), suiteHash,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"listing test durations: " + err.Error()})

		return
	}

	// Group by test name and build the SuiteStats shape.
	stats := make(executor.SuiteStats, len(durations))

	for i := range durations {
		d := &durations[i]

		var steps *executor.RunDurationStepsStats
		if d.StepsJSON != "" {
			var s executor.RunDurationStepsStats
			if json.Unmarshal([]byte(d.StepsJSON), &s) == nil {
				steps = &s
			}
		}

		rd := &executor.RunDuration{
			ID:       d.RunID,
			Client:   d.Client,
			GasUsed:  d.GasUsed,
			Time:     d.TimeNs,
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
