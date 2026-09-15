package indexstore

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrRunNotFound is returned when a run ID does not exist in the index.
var ErrRunNotFound = errors.New("run not found")

// MarkRunForDeletion queues a run for deletion by stamping
// deletion_requested_at. It is idempotent: a run that is already queued
// keeps its original position in the queue. Returns ErrRunNotFound when
// no run with the given ID is indexed.
func (s *store) MarkRunForDeletion(
	ctx context.Context, runID string,
) error {
	now := time.Now().UTC()

	res := s.db.WithContext(ctx).
		Model(&Run{}).
		Where("run_id = ? AND deletion_requested_at IS NULL", runID).
		Updates(map[string]any{
			"deletion_requested_at": now,
			"deletion_error":        "",
		})
	if res.Error != nil {
		return fmt.Errorf("marking run for deletion: %w", res.Error)
	}

	if res.RowsAffected > 0 {
		s.runsGen.Add(1)

		return nil
	}

	// Nothing changed: either the run is already queued or it does not
	// exist. Tell them apart so the caller can report a missing run.
	var count int64
	if err := s.db.WithContext(ctx).
		Model(&Run{}).
		Where("run_id = ?", runID).
		Count(&count).Error; err != nil {
		return fmt.Errorf("looking up run: %w", err)
	}

	if count == 0 {
		return ErrRunNotFound
	}

	return nil
}

// ListRunsPendingDeletion returns every queued run in queue order: the
// oldest deletion request first, with the row ID as a tie-breaker so the
// order is stable across drivers.
func (s *store) ListRunsPendingDeletion(
	ctx context.Context,
) ([]Run, error) {
	var runs []Run
	if err := s.readDB.WithContext(ctx).
		Where("deletion_requested_at IS NOT NULL").
		Order("deletion_requested_at ASC, id ASC").
		Find(&runs).Error; err != nil {
		return nil, fmt.Errorf("listing runs pending deletion: %w", err)
	}

	return runs, nil
}

// SetRunDeletionError records the error of a failed deletion attempt on
// the queued run so the API and UI can surface it. The run stays queued.
func (s *store) SetRunDeletionError(
	ctx context.Context, runID, msg string,
) error {
	if err := s.db.WithContext(ctx).
		Model(&Run{}).
		Where("run_id = ?", runID).
		Update("deletion_error", msg).Error; err != nil {
		return fmt.Errorf("recording run deletion error: %w", err)
	}

	s.runsGen.Add(1)

	return nil
}

// UnmarkRunForDeletion takes a run out of the deletion queue and clears
// any recorded error. Returns ErrRunNotFound when no run with the given
// ID is indexed; a run that is not queued is a no-op.
func (s *store) UnmarkRunForDeletion(
	ctx context.Context, runID string,
) error {
	res := s.db.WithContext(ctx).
		Model(&Run{}).
		Where("run_id = ? AND deletion_requested_at IS NOT NULL", runID).
		Updates(map[string]any{
			"deletion_requested_at": nil,
			"deletion_error":        "",
		})
	if res.Error != nil {
		return fmt.Errorf("unmarking run for deletion: %w", res.Error)
	}

	if res.RowsAffected > 0 {
		s.runsGen.Add(1)

		return nil
	}

	var count int64
	if err := s.db.WithContext(ctx).
		Model(&Run{}).
		Where("run_id = ?", runID).
		Count(&count).Error; err != nil {
		return fmt.Errorf("looking up run: %w", err)
	}

	if count == 0 {
		return ErrRunNotFound
	}

	return nil
}
