package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
)

const (
	// defaultIndexerFailureLimit is the page size the admin UI gets when it
	// asks for none. maxIndexerFailureLimit caps what it can ask for: a
	// neglected deployment holds thousands of records, and the whole table in
	// one response is what the /index endpoint already taught us not to do.
	defaultIndexerFailureLimit = 200
	maxIndexerFailureLimit     = 1000
)

// indexerFailureEntry is one recorded failure in the admin listing.
type indexerFailureEntry struct {
	RunID         string `json:"run_id"`
	DiscoveryPath string `json:"discovery_path"`
	// RunTimestamp is the unix time the run ID carries, omitted when the ID
	// does not follow the naming convention.
	RunTimestamp        int64  `json:"run_timestamp,omitempty"`
	Error               string `json:"error,omitempty"`
	FailedAt            string `json:"failed_at"`
	DeletionRequestedAt string `json:"deletion_requested_at,omitempty"`
	DeletionError       string `json:"deletion_error,omitempty"`
}

// indexerFailuresResponse is one page of failures plus the total, so the UI
// can show how much work is left without reading every row.
type indexerFailuresResponse struct {
	Total   int64                 `json:"total"`
	Limit   int                   `json:"limit"`
	Offset  int                   `json:"offset"`
	Entries []indexerFailureEntry `json:"entries"`
}

// handleIndexerFailures lists the runs the indexer gave up on, newest run
// first.
func (s *server) handleIndexerFailures(
	w http.ResponseWriter, r *http.Request,
) {
	limit := clampQueryInt(
		r.URL.Query().Get("limit"),
		defaultIndexerFailureLimit, 1, maxIndexerFailureLimit,
	)
	offset := clampQueryInt(r.URL.Query().Get("offset"), 0, 0, 0)

	total, err := s.indexStore.CountIndexFailures(r.Context())
	if err != nil {
		s.log.WithError(err).Error("Failed to count index failures")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	failures, err := s.indexStore.ListIndexFailures(r.Context(), limit, offset)
	if err != nil {
		s.log.WithError(err).Error("Failed to list index failures")
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"internal error"})

		return
	}

	entries := make([]indexerFailureEntry, 0, len(failures))

	for i := range failures {
		failure := &failures[i]

		entry := indexerFailureEntry{
			RunID:         failure.RunID,
			DiscoveryPath: failure.DiscoveryPath,
			RunTimestamp:  failure.RunTimestamp,
			Error:         failure.Reason,
			FailedAt:      formatAdminTime(failure.FailedAt),
			DeletionError: failure.DeletionError,
		}

		if failure.DeletionRequestedAt != nil {
			entry.DeletionRequestedAt = formatAdminTime(
				*failure.DeletionRequestedAt,
			)
		}

		entries = append(entries, entry)
	}

	writeJSON(w, http.StatusOK, indexerFailuresResponse{
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		Entries: entries,
	})
}

// deleteIndexerFailuresRequest selects what to act on: an explicit list of run
// IDs, or every recorded failure. "All" exists because a neglected deployment
// holds thousands of these and clearing them a page at a time is not
// housekeeping.
type deleteIndexerFailuresRequest struct {
	RunIDs []string `json:"run_ids,omitempty"`
	All    bool     `json:"all,omitempty"`
}

type deleteIndexerFailuresResponse struct {
	Status string   `json:"status"`
	Queued int64    `json:"queued"`
	Errors []string `json:"errors,omitempty"`
}

type cancelIndexerFailuresResponse struct {
	Status    string   `json:"status"`
	Cancelled int64    `json:"cancelled"`
	Errors    []string `json:"errors,omitempty"`
}

// handleDeleteIndexerFailures queues failed runs so their stored data is
// removed. The request returns as soon as every record is marked; the run
// deleter does the storage deletes in the background, in queue order, and
// drops each record once its data is gone.
func (s *server) handleDeleteIndexerFailures(
	w http.ResponseWriter, r *http.Request,
) {
	if s.storageDeleter == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			errorResponse{"storage backend does not support deletion"})

		return
	}

	req, ok := decodeIndexerFailuresRequest(w, r)
	if !ok {
		return
	}

	if req.All {
		queued, err := s.indexStore.MarkAllIndexFailuresForDeletion(r.Context())
		if err != nil {
			s.log.WithError(err).
				Error("Failed to queue all index failures for deletion")
			writeJSON(w, http.StatusInternalServerError,
				errorResponse{"internal error"})

			return
		}

		s.kickRunDeleter()
		writeJSON(w, http.StatusAccepted, deleteIndexerFailuresResponse{
			Status: "ok",
			Queued: queued,
		})

		return
	}

	var (
		queued int64
		errs   []string
	)

	for _, runID := range req.RunIDs {
		err := s.indexStore.MarkIndexFailureForDeletion(r.Context(), runID)

		switch {
		case err == nil:
			queued++
		case errors.Is(err, indexstore.ErrIndexFailureNotFound):
			errs = append(errs, fmt.Sprintf("%s: not a recorded failure", runID))
		default:
			s.log.WithError(err).WithField("run_id", runID).
				Error("Failed to queue index failure for deletion")
			errs = append(errs, fmt.Sprintf("%s: %v", runID, err))
		}
	}

	if queued > 0 {
		s.kickRunDeleter()
	}

	writeJSON(w, http.StatusAccepted, deleteIndexerFailuresResponse{
		Status: "ok",
		Queued: queued,
		Errors: errs,
	})
}

// handleCancelDeleteIndexerFailures takes failed runs out of the deletion
// queue. It is best-effort: a record the worker is deleting at this moment is
// still removed. A record that is not queued counts as cancelled.
func (s *server) handleCancelDeleteIndexerFailures(
	w http.ResponseWriter, r *http.Request,
) {
	req, ok := decodeIndexerFailuresRequest(w, r)
	if !ok {
		return
	}

	if req.All {
		cancelled, err := s.indexStore.UnmarkAllIndexFailuresForDeletion(
			r.Context(),
		)
		if err != nil {
			s.log.WithError(err).
				Error("Failed to cancel all index failure deletions")
			writeJSON(w, http.StatusInternalServerError,
				errorResponse{"internal error"})

			return
		}

		writeJSON(w, http.StatusOK, cancelIndexerFailuresResponse{
			Status:    "ok",
			Cancelled: cancelled,
		})

		return
	}

	var (
		cancelled int64
		errs      []string
	)

	for _, runID := range req.RunIDs {
		err := s.indexStore.UnmarkIndexFailureForDeletion(r.Context(), runID)

		switch {
		case err == nil:
			cancelled++
		case errors.Is(err, indexstore.ErrIndexFailureNotFound):
			errs = append(errs, fmt.Sprintf("%s: not a recorded failure", runID))
		default:
			s.log.WithError(err).WithField("run_id", runID).
				Error("Failed to cancel index failure deletion")
			errs = append(errs, fmt.Sprintf("%s: %v", runID, err))
		}
	}

	writeJSON(w, http.StatusOK, cancelIndexerFailuresResponse{
		Status:    "ok",
		Cancelled: cancelled,
		Errors:    errs,
	})
}

// decodeIndexerFailuresRequest reads and validates the shared request body. It
// writes the error response itself and reports whether the caller may carry on.
func decodeIndexerFailuresRequest(
	w http.ResponseWriter, r *http.Request,
) (*deleteIndexerFailuresRequest, bool) {
	var req deleteIndexerFailuresRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"invalid request body"})

		return nil, false
	}

	if !req.All && len(req.RunIDs) == 0 {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"run_ids or all is required"})

		return nil, false
	}

	return &req, true
}

// clampQueryInt parses a query parameter into a bounded integer. A missing or
// malformed value yields fallback. A maximum of 0 means no upper bound.
func clampQueryInt(raw string, fallback, minimum, maximum int) int {
	if raw == "" {
		return fallback
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}

	if value < minimum {
		return minimum
	}

	if maximum > 0 && value > maximum {
		return maximum
	}

	return value
}

// formatAdminTime renders a timestamp the way the other admin listings do.
// A zero time renders empty rather than as year one.
func formatAdminTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return t.UTC().Format("2006-01-02T15:04:05Z")
}
