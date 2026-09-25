package indexstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

var (
	// ErrRunNotFound is returned when a run ID does not exist in the index.
	ErrRunNotFound = errors.New("run not found")

	// ErrRunInProgress is returned when a run cannot be queued for
	// deletion because its runner is still reporting.
	ErrRunInProgress = errors.New("run is still in progress")
)

// MarkRunForDeletion queues a run for deletion by stamping
// deletion_requested_at. It is idempotent: a run that is already queued
// keeps its original position in the queue. Returns ErrRunNotFound when
// no run with the given ID is indexed and ErrRunInProgress when the run
// is still running: deleting it would race with the active runner.
func (s *store) MarkRunForDeletion(
	ctx context.Context, runID string,
) error {
	now := time.Now().UTC()

	// The status guard lives in the WHERE clause so a run that flips to
	// running between a check and the update is still refused.
	res := s.db.WithContext(ctx).
		Model(&Run{}).
		Where("run_id = ? AND deletion_requested_at IS NULL AND status != ?",
			runID, RunStatusRunning).
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

	// Nothing changed: the run is already queued, still running, or does
	// not exist. Tell them apart so the caller can report the reason.
	var run Run
	if err := s.db.WithContext(ctx).
		Select("status", "deletion_requested_at").
		Where("run_id = ?", runID).
		First(&run).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrRunNotFound
		}

		return fmt.Errorf("looking up run: %w", err)
	}

	if run.DeletionRequestedAt == nil && run.Status == RunStatusRunning {
		return ErrRunInProgress
	}

	return nil
}

// SuiteDeletionResult reports what queueing a whole suite did. Skipped counts
// the runs refused because their runner is still reporting, and AlreadyQueued
// the ones that were waiting in the queue before the request. Neither is an
// error, but both are the caller's to report: without them a repeat request
// reads as "queued 0 runs" with no reason given.
type SuiteDeletionResult struct {
	Queued        int64
	Skipped       int64
	AlreadyQueued int64
}

// MarkRunsForDeletionBySuite queues every run of a suite in one statement.
// A suite can hold thousands of runs, so resolving them to IDs and marking
// each one in turn would be a needlessly large request and a needlessly long
// transaction.
//
// It honours the same guard as MarkRunForDeletion: a run that is still
// running is left alone, because deleting it would race with the active
// runner. Runs already queued keep their place. Returns ErrRunNotFound when
// the suite has no runs at all.
func (s *store) MarkRunsForDeletionBySuite(
	ctx context.Context, suiteHash string,
) (*SuiteDeletionResult, error) {
	if suiteHash == "" {
		return nil, ErrRunNotFound
	}

	result := &SuiteDeletionResult{}

	// The update and the counts share one transaction so the three numbers
	// describe the same instant. Without it a run that finishes between the
	// update and the count is neither queued nor reported as left behind, and
	// the totals quietly fail to add up.
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var total int64
		if err := tx.Model(&Run{}).
			Where("suite_hash = ?", suiteHash).
			Count(&total).Error; err != nil {
			return fmt.Errorf("counting suite runs: %w", err)
		}

		if total == 0 {
			return ErrRunNotFound
		}

		// The status guard lives in the WHERE clause, as it does for a single
		// run: deleting a run its runner is still writing would race.
		res := tx.Model(&Run{}).
			Where(
				"suite_hash = ? AND deletion_requested_at IS NULL AND status != ?",
				suiteHash, RunStatusRunning,
			).
			Updates(map[string]any{
				"deletion_requested_at": time.Now().UTC(),
				"deletion_error":        "",
			})
		if res.Error != nil {
			return fmt.Errorf(
				"marking suite runs for deletion: %w", res.Error,
			)
		}

		result.Queued = res.RowsAffected

		if err := tx.Model(&Run{}).
			Where("suite_hash = ? AND deletion_requested_at IS NULL AND status = ?",
				suiteHash, RunStatusRunning).
			Count(&result.Skipped).Error; err != nil {
			return fmt.Errorf("counting running suite runs: %w", err)
		}

		var queued int64
		if err := tx.Model(&Run{}).
			Where("suite_hash = ? AND deletion_requested_at IS NOT NULL",
				suiteHash).
			Count(&queued).Error; err != nil {
			return fmt.Errorf("counting queued suite runs: %w", err)
		}

		result.AlreadyQueued = queued - result.Queued

		return nil
	})
	if err != nil {
		return nil, err
	}

	if result.Queued > 0 {
		s.runsGen.Add(1)
	}

	return result, nil
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
