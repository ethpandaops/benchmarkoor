package indexstore

import (
	"context"
	"fmt"
	"time"
)

// What started a pass. A manual pass is one an admin asked for from the admin
// page, so a slow pass can be told apart from a slow scheduled one.
const (
	IndexerPassTriggerStartup  = "startup"
	IndexerPassTriggerSchedule = "schedule"
	IndexerPassTriggerManual   = "manual"
)

// How a pass ended. A shutdown cut a cancelled pass short, either between
// two discovery paths or inside one, so its counters describe only the work
// it got through.
const (
	IndexerPassStatusCompleted = "completed"
	IndexerPassStatusCancelled = "cancelled"
)

// indexerPassRetention is how many passes the table keeps. The admin page
// charts the last hundred, and a deployment on a one-minute interval writes a
// row a minute, so an unbounded table would outgrow the data it describes.
const indexerPassRetention = 500

// IndexerPass records one indexing pass: how long it took and what it did.
// The indexer writes a row when a pass ends, and the admin page charts the
// recent ones, which is the only way to see a pass getting slower before it
// gets slow enough to notice.
type IndexerPass struct {
	ID        uint      `gorm:"primaryKey"`
	StartedAt time.Time `gorm:"index"`
	// FinishedAt and DurationMs describe the same interval. The duration is
	// stored rather than computed so a chart does not subtract two timestamps
	// per row.
	FinishedAt time.Time
	DurationMs int64

	// Trigger is what started the pass, and Status is how it ended. See the
	// constants above.
	Trigger string `gorm:"index"`
	Status  string `gorm:"index"`

	// DiscoveryPaths is how many paths the pass walked.
	DiscoveryPaths int

	// StorageRuns is how many run directories storage held, and IndexedRuns
	// how many of them the index already had. The gap is the backlog.
	StorageRuns int
	IndexedRuns int

	// RunsIndexed counts runs the pass added, RunsReindexed the incomplete
	// ones it read again, and RunsFailed the ones it could not index at all.
	RunsIndexed   int
	RunsReindexed int
	RunsFailed    int

	// SkippedFailures counts runs the pass never read, because their failure
	// record is still inside the retry interval.
	SkippedFailures int

	// Error is the last discovery path failure of the pass, empty when every
	// path was walked without one.
	Error string `gorm:"type:text"`
}

// RecordIndexerPass stores a finished pass and prunes the table back to the
// retention limit, so the caller never has to think about housekeeping.
func (s *store) RecordIndexerPass(
	ctx context.Context, pass *IndexerPass,
) error {
	if err := s.db.WithContext(ctx).Create(pass).Error; err != nil {
		return fmt.Errorf("recording indexer pass: %w", err)
	}

	if err := s.pruneIndexerPasses(ctx); err != nil {
		return err
	}

	return nil
}

// pruneIndexerPasses drops every pass older than the newest
// indexerPassRetention rows. It finds the ID at the retention boundary and
// deletes at or below it, which is one indexed lookup plus one ranged delete
// on both SQLite and Postgres.
func (s *store) pruneIndexerPasses(ctx context.Context) error {
	var cutoff uint

	if err := s.db.WithContext(ctx).
		Model(&IndexerPass{}).
		Select("id").
		Order("id DESC").
		Offset(indexerPassRetention).
		Limit(1).
		Scan(&cutoff).Error; err != nil {
		return fmt.Errorf("finding indexer pass cutoff: %w", err)
	}

	// Fewer rows than the retention limit: nothing to drop.
	if cutoff == 0 {
		return nil
	}

	if err := s.db.WithContext(ctx).
		Where("id <= ?", cutoff).
		Delete(&IndexerPass{}).Error; err != nil {
		return fmt.Errorf("pruning indexer passes: %w", err)
	}

	return nil
}

// ListIndexerPasses returns the most recent passes, newest first.
func (s *store) ListIndexerPasses(
	ctx context.Context, limit int,
) ([]IndexerPass, error) {
	var passes []IndexerPass
	if err := s.readDB.WithContext(ctx).
		Order("id DESC").
		Limit(limit).
		Find(&passes).Error; err != nil {
		return nil, fmt.Errorf("listing indexer passes: %w", err)
	}

	return passes, nil
}
