package indexstore

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrIndexFailureNotFound is returned when no recorded index failure
// matches the given run ID.
var ErrIndexFailureNotFound = errors.New("index failure not found")

// IndexFailure records a run that storage exposes but the indexer cannot
// index, usually because the run directory never got its config.json. Without
// the record the same broken run costs an S3 round-trip and a log line on
// every pass, forever.
//
// A run only earns a row once it is old enough that an upload still in flight
// is ruled out, so a healthy new run never lands here. See the indexer's
// failure grace period.
type IndexFailure struct {
	ID            uint   `gorm:"primaryKey"`
	DiscoveryPath string `gorm:"not null;uniqueIndex:idx_index_failures_dp_run"`
	RunID         string `gorm:"not null;uniqueIndex:idx_index_failures_dp_run"`

	// RunTimestamp is the unix time the run ID carries, or 0 when the ID does
	// not follow the {timestamp}_{shortID}_{instance} convention. The run has
	// no config.json, so the ID is the only place a timestamp survives.
	RunTimestamp int64 `gorm:"index"`

	// LastError is the failure of the most recent attempt.
	LastError string `gorm:"type:text"`

	// Attempts counts the failed indexing attempts. The indexer skips a row
	// until LastAttemptAt falls outside the retry interval, so a run whose
	// upload finished late still heals without anyone intervening.
	Attempts      int
	FirstFailedAt time.Time
	LastAttemptAt time.Time `gorm:"index"`

	// DeletionRequestedAt is set when an admin queues the run's stored data
	// for deletion. DeletionError carries the last failed attempt.
	DeletionRequestedAt *time.Time `gorm:"index"`
	DeletionError       string     `gorm:"type:text"`
}

// RunIDTimestamp extracts the unix seconds a run ID carries. Run directories
// are named {timestamp}_{shortID}_{instance}, so the leading field dates a run
// whose config.json never arrived. It returns 0 when the ID does not follow
// the convention.
func RunIDTimestamp(runID string) int64 {
	head, _, found := strings.Cut(runID, "_")
	if !found {
		return 0
	}

	ts, err := strconv.ParseInt(head, 10, 64)
	if err != nil || ts <= 0 {
		return 0
	}

	return ts
}

// RecordIndexFailure stores a failed indexing attempt. The first failure
// creates the row; later ones bump the attempt count and overwrite the error,
// keeping FirstFailedAt as the moment the run first went bad.
func (s *store) RecordIndexFailure(
	ctx context.Context, discoveryPath, runID, msg string,
) error {
	now := time.Now().UTC()

	failure := &IndexFailure{
		DiscoveryPath: discoveryPath,
		RunID:         runID,
		RunTimestamp:  RunIDTimestamp(runID),
		LastError:     msg,
		Attempts:      1,
		FirstFailedAt: now,
		LastAttemptAt: now,
	}

	if err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "discovery_path"}, {Name: "run_id"},
			},
			DoUpdates: clause.Assignments(map[string]any{
				"last_error":      msg,
				"run_timestamp":   failure.RunTimestamp,
				"attempts":        gorm.Expr("index_failures.attempts + 1"),
				"last_attempt_at": now,
			}),
		}).
		Create(failure).Error; err != nil {
		return fmt.Errorf("recording index failure: %w", err)
	}

	return nil
}

// ClearIndexFailure drops the record for a run that indexed successfully. A
// run with no record is a no-op, which is the common case.
func (s *store) ClearIndexFailure(
	ctx context.Context, discoveryPath, runID string,
) error {
	if err := s.db.WithContext(ctx).
		Where("discovery_path = ? AND run_id = ?", discoveryPath, runID).
		Delete(&IndexFailure{}).Error; err != nil {
		return fmt.Errorf("clearing index failure: %w", err)
	}

	return nil
}

// ListIndexFailuresByPath returns every recorded failure for one discovery
// path. The indexer reads it once per pass to build both its skip set and the
// set of records whose run has since left storage.
func (s *store) ListIndexFailuresByPath(
	ctx context.Context, discoveryPath string,
) ([]IndexFailure, error) {
	var failures []IndexFailure
	if err := s.readDB.WithContext(ctx).
		Where("discovery_path = ?", discoveryPath).
		Find(&failures).Error; err != nil {
		return nil, fmt.Errorf("listing index failures: %w", err)
	}

	return failures, nil
}

// ListIndexFailures returns one page of recorded failures, newest run first,
// so the admin UI shows the most recent breakage at the top. Records whose run
// ID carries no timestamp sort last. A deployment can accumulate thousands of
// these, which is why the caller pages rather than reading them all.
func (s *store) ListIndexFailures(
	ctx context.Context, limit, offset int,
) ([]IndexFailure, error) {
	var failures []IndexFailure
	if err := s.readDB.WithContext(ctx).
		Order("run_timestamp DESC, id DESC").
		Limit(limit).
		Offset(offset).
		Find(&failures).Error; err != nil {
		return nil, fmt.Errorf("listing index failures: %w", err)
	}

	return failures, nil
}

// CountIndexFailures returns how many failures are recorded.
func (s *store) CountIndexFailures(ctx context.Context) (int64, error) {
	var count int64
	if err := s.readDB.WithContext(ctx).
		Model(&IndexFailure{}).
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("counting index failures: %w", err)
	}

	return count, nil
}

// MarkIndexFailureForDeletion queues a failed run's stored data for deletion.
// It is idempotent: a record already queued keeps its place in the queue.
// Returns ErrIndexFailureNotFound when nothing matches the run ID.
func (s *store) MarkIndexFailureForDeletion(
	ctx context.Context, runID string,
) error {
	res := s.db.WithContext(ctx).
		Model(&IndexFailure{}).
		Where("run_id = ? AND deletion_requested_at IS NULL", runID).
		Updates(map[string]any{
			"deletion_requested_at": time.Now().UTC(),
			"deletion_error":        "",
		})
	if res.Error != nil {
		return fmt.Errorf("marking index failure for deletion: %w", res.Error)
	}

	if res.RowsAffected > 0 {
		return nil
	}

	// Nothing changed: the record is already queued, or absent.
	return s.indexFailureExists(ctx, runID)
}

// UnmarkIndexFailureForDeletion takes a failed run out of the deletion queue
// and clears any recorded error. A record that is not queued is a no-op.
func (s *store) UnmarkIndexFailureForDeletion(
	ctx context.Context, runID string,
) error {
	res := s.db.WithContext(ctx).
		Model(&IndexFailure{}).
		Where("run_id = ? AND deletion_requested_at IS NOT NULL", runID).
		Updates(map[string]any{
			"deletion_requested_at": nil,
			"deletion_error":        "",
		})
	if res.Error != nil {
		return fmt.Errorf(
			"unmarking index failure for deletion: %w", res.Error,
		)
	}

	if res.RowsAffected > 0 {
		return nil
	}

	return s.indexFailureExists(ctx, runID)
}

// indexFailureExists returns nil when a record for the run ID is present and
// ErrIndexFailureNotFound when it is not. It tells "already in that state"
// apart from "never existed" for the callers above.
func (s *store) indexFailureExists(ctx context.Context, runID string) error {
	var count int64
	if err := s.db.WithContext(ctx).
		Model(&IndexFailure{}).
		Where("run_id = ?", runID).
		Count(&count).Error; err != nil {
		return fmt.Errorf("looking up index failure: %w", err)
	}

	if count == 0 {
		return ErrIndexFailureNotFound
	}

	return nil
}

// MarkAllIndexFailuresForDeletion queues every recorded failure that is not
// queued yet and returns how many it added. It exists because a neglected
// deployment accumulates thousands of these, and clearing them a page at a
// time is not housekeeping.
func (s *store) MarkAllIndexFailuresForDeletion(
	ctx context.Context,
) (int64, error) {
	res := s.db.WithContext(ctx).
		Model(&IndexFailure{}).
		Where("deletion_requested_at IS NULL").
		Updates(map[string]any{
			"deletion_requested_at": time.Now().UTC(),
			"deletion_error":        "",
		})
	if res.Error != nil {
		return 0, fmt.Errorf(
			"marking all index failures for deletion: %w", res.Error,
		)
	}

	return res.RowsAffected, nil
}

// UnmarkAllIndexFailuresForDeletion empties the queue and returns how many
// records it removed.
func (s *store) UnmarkAllIndexFailuresForDeletion(
	ctx context.Context,
) (int64, error) {
	res := s.db.WithContext(ctx).
		Model(&IndexFailure{}).
		Where("deletion_requested_at IS NOT NULL").
		Updates(map[string]any{
			"deletion_requested_at": nil,
			"deletion_error":        "",
		})
	if res.Error != nil {
		return 0, fmt.Errorf(
			"unmarking all index failures for deletion: %w", res.Error,
		)
	}

	return res.RowsAffected, nil
}

// ListIndexFailuresPendingDeletion returns every queued record in queue order:
// the oldest request first, with the row ID as a tie-breaker so the order is
// stable across drivers.
func (s *store) ListIndexFailuresPendingDeletion(
	ctx context.Context,
) ([]IndexFailure, error) {
	var failures []IndexFailure
	if err := s.readDB.WithContext(ctx).
		Where("deletion_requested_at IS NOT NULL").
		Order("deletion_requested_at ASC, id ASC").
		Find(&failures).Error; err != nil {
		return nil, fmt.Errorf(
			"listing index failures pending deletion: %w", err,
		)
	}

	return failures, nil
}

// SetIndexFailureDeletionError records the error of a failed deletion attempt
// so the UI can surface it. The record stays queued and is retried.
func (s *store) SetIndexFailureDeletionError(
	ctx context.Context, discoveryPath, runID, msg string,
) error {
	if err := s.db.WithContext(ctx).
		Model(&IndexFailure{}).
		Where("discovery_path = ? AND run_id = ?", discoveryPath, runID).
		Update("deletion_error", msg).Error; err != nil {
		return fmt.Errorf("recording index failure deletion error: %w", err)
	}

	return nil
}
