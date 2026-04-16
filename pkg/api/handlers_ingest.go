package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/sirupsen/logrus"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
)

// handleIngestRun accepts a periodic status report from a benchmarkoor
// runner and upserts it into the live_runs table. Always returns 204 on
// success so the runner can fire-and-forget.
func (s *server) handleIngestRun(w http.ResponseWriter, r *http.Request) {
	// Read the body fully so we can log its decompressed size — useful
	// for spotting payload growth (the per-test gas map scales with
	// suite size). The middleware already inflates Content-Encoding:
	// gzip bodies, so this size is post-decompression.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"failed to read request body"})

		return
	}

	var report indexstore.LiveRunReport
	if err := json.Unmarshal(body, &report); err != nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"invalid request body"})

		return
	}

	if report.DiscoveryPath == "" {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"discovery_path is required"})

		return
	}

	if report.RunID == "" {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"run_id is required"})

		return
	}

	if report.Status == "" {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"status is required"})

		return
	}

	if err := s.indexStore.UpsertLiveRun(r.Context(), &report); err != nil {
		s.log.WithError(err).
			WithField("run_id", report.RunID).
			Warn("Failed to ingest live run report")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"failed to upsert live run"})

		return
	}

	fields := logrus.Fields{
		"run_id":         report.RunID,
		"discovery_path": report.DiscoveryPath,
		"status":         report.Status,
		"client":         report.Client,
		"instance_id":    report.InstanceID,
		"tests_total":    report.TestsTotal,
		"tests_passed":   report.TestsPassed,
		"tests_failed":   report.TestsFailed,
		"has_config":     len(report.Config) > 0,
		// payload_bytes is the decoded JSON size; payload_gzip_bytes is
		// the on-the-wire size (== payload_bytes when the runner did not
		// gzip). Both are useful for spotting payload growth.
		"payload_bytes":      len(body),
		"payload_gzip_bytes": compressedRequestSize(r),
	}

	if report.SuiteHash != "" {
		fields["suite_hash"] = report.SuiteHash
	}

	if report.TerminationReason != "" {
		fields["termination_reason"] = report.TerminationReason
	}

	if report.TotalGasUsedDurationNs > 0 {
		fields["total_gas_used"] = report.TotalGasUsed
		fields["total_gas_used_duration_ns"] = report.TotalGasUsedDurationNs
	}

	if len(report.Tests) > 0 {
		fields["tests_in_payload"] = len(report.Tests)
	}

	s.log.WithFields(fields).Info("Received live run report")

	w.WriteHeader(http.StatusNoContent)
}

// handleListLiveRuns returns all current live runs for the UI to merge
// with the indexed runs.
func (s *server) handleListLiveRuns(w http.ResponseWriter, r *http.Request) {
	live, err := s.indexStore.ListLiveRuns(r.Context())
	if err != nil {
		s.log.WithError(err).Warn("Failed to list live runs")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"failed to list live runs"})

		return
	}

	resp := make([]indexstore.LiveRunResponse, 0, len(live))
	for i := range live {
		resp = append(resp, toLiveRunResponse(&live[i]))
	}

	writeJSON(w, http.StatusOK, resp)
}

// toLiveRunResponse converts a LiveRun model to its JSON DTO.
func toLiveRunResponse(r *indexstore.LiveRun) indexstore.LiveRunResponse {
	resp := indexstore.LiveRunResponse{
		ID:                     r.ID,
		DiscoveryPath:          r.DiscoveryPath,
		RunID:                  r.RunID,
		Timestamp:              r.Timestamp,
		TimestampEnd:           r.TimestampEnd,
		SuiteHash:              r.SuiteHash,
		Status:                 r.Status,
		TerminationReason:      r.TerminationReason,
		InstanceID:             r.InstanceID,
		Client:                 r.Client,
		Image:                  r.Image,
		RollbackStrategy:       r.RollbackStrategy,
		TestsTotal:             r.TestsTotal,
		TestsPassed:            r.TestsPassed,
		TestsFailed:            r.TestsFailed,
		TotalGasUsed:           r.TotalGasUsed,
		TotalGasUsedDurationNs: r.TotalGasUsedDurationNs,
		LastReportedAt:         r.LastReportedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}

	if r.MetadataJSON != "" {
		var labels map[string]string
		if err := json.Unmarshal([]byte(r.MetadataJSON), &labels); err == nil {
			resp.Metadata = labels
		}
	}

	if r.ConfigJSON != "" {
		resp.Config = json.RawMessage(r.ConfigJSON)
	}

	if r.TestsJSON != "" {
		var tests map[string]indexstore.LiveTestStats
		if err := json.Unmarshal([]byte(r.TestsJSON), &tests); err == nil {
			resp.Tests = tests
		}
	}

	return resp
}
