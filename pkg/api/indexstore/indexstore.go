package indexstore

import (
	"context"
	"fmt"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Store provides persistence for the indexed benchmark data.
type Store interface {
	Start(ctx context.Context) error
	Stop() error

	UpsertRun(ctx context.Context, run *Run) error
	ListRuns(ctx context.Context, discoveryPath string) ([]Run, error)
	ListRunIDs(ctx context.Context, discoveryPath string) ([]string, error)
	ListIncompleteRunIDs(
		ctx context.Context, discoveryPath string,
	) ([]string, error)

	UpsertTestDuration(ctx context.Context, d *TestDuration) error
	BulkUpsertTestDurations(
		ctx context.Context, durations []*TestDuration,
	) error
	ListTestDurationsBySuite(
		ctx context.Context, suiteHash string,
	) ([]TestDuration, error)
	DeleteTestDurationsForRun(ctx context.Context, runID string) error

	ListAllRuns(ctx context.Context) ([]Run, error)
}

// Compile-time interface check.
var _ Store = (*store)(nil)

type store struct {
	log logrus.FieldLogger
	cfg *config.APIDatabaseConfig
	db  *gorm.DB
}

// NewStore creates a new index Store backed by the configured database driver.
func NewStore(
	log logrus.FieldLogger,
	cfg *config.APIDatabaseConfig,
) Store {
	return &store{
		log: log.WithField("component", "indexstore"),
		cfg: cfg,
	}
}

// Start opens the database connection and runs migrations.
func (s *store) Start(ctx context.Context) error {
	var dialector gorm.Dialector

	gormCfg := &gorm.Config{
		Logger: logger.Discard,
	}

	switch s.cfg.Driver {
	case "sqlite":
		dialector = sqlite.Open(s.cfg.SQLite.Path)
	case "postgres":
		dsn := fmt.Sprintf(
			"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			s.cfg.Postgres.Host,
			s.cfg.Postgres.Port,
			s.cfg.Postgres.User,
			s.cfg.Postgres.Password,
			s.cfg.Postgres.Database,
			s.cfg.Postgres.SSLMode,
		)
		dialector = postgres.Open(dsn)
	default:
		return fmt.Errorf("unsupported database driver: %s", s.cfg.Driver)
	}

	db, err := gorm.Open(dialector, gormCfg)
	if err != nil {
		return fmt.Errorf("opening index database: %w", err)
	}

	s.db = db

	if err := s.db.WithContext(ctx).AutoMigrate(
		&Run{},
		&TestDuration{},
	); err != nil {
		return fmt.Errorf("running index migrations: %w", err)
	}

	s.log.WithField("driver", s.cfg.Driver).
		Info("Index database connected")

	return nil
}

// Stop closes the underlying database connection.
func (s *store) Stop() error {
	if s.db == nil {
		return nil
	}

	sqlDB, err := s.db.DB()
	if err != nil {
		return fmt.Errorf("getting underlying db: %w", err)
	}

	return sqlDB.Close()
}

// UpsertRun inserts or updates a run record keyed by discovery_path + run_id.
func (s *store) UpsertRun(ctx context.Context, run *Run) error {
	result := s.db.WithContext(ctx).
		Where("discovery_path = ? AND run_id = ?",
			run.DiscoveryPath, run.RunID).
		Assign(run).
		FirstOrCreate(run)
	if result.Error != nil {
		return fmt.Errorf("upserting run: %w", result.Error)
	}

	return nil
}

// ListRuns returns all runs for a given discovery path ordered by timestamp.
func (s *store) ListRuns(
	ctx context.Context, discoveryPath string,
) ([]Run, error) {
	var runs []Run
	if err := s.db.WithContext(ctx).
		Where("discovery_path = ?", discoveryPath).
		Order("timestamp DESC").
		Find(&runs).Error; err != nil {
		return nil, fmt.Errorf("listing runs: %w", err)
	}

	return runs, nil
}

// ListAllRuns returns all runs across all discovery paths.
func (s *store) ListAllRuns(ctx context.Context) ([]Run, error) {
	var runs []Run
	if err := s.db.WithContext(ctx).
		Order("timestamp DESC").
		Find(&runs).Error; err != nil {
		return nil, fmt.Errorf("listing all runs: %w", err)
	}

	return runs, nil
}

// ListRunIDs returns just the run IDs for a given discovery path.
func (s *store) ListRunIDs(
	ctx context.Context, discoveryPath string,
) ([]string, error) {
	var ids []string
	if err := s.db.WithContext(ctx).
		Model(&Run{}).
		Where("discovery_path = ?", discoveryPath).
		Pluck("run_id", &ids).Error; err != nil {
		return nil, fmt.Errorf("listing run ids: %w", err)
	}

	return ids, nil
}

// terminalStatuses are run statuses that will not change.
var terminalStatuses = []string{"completed", "cancelled", "container_died"}

// ListIncompleteRunIDs returns run IDs where the result has not been indexed
// and the status is not terminal (still potentially in progress).
func (s *store) ListIncompleteRunIDs(
	ctx context.Context, discoveryPath string,
) ([]string, error) {
	var ids []string
	if err := s.db.WithContext(ctx).
		Model(&Run{}).
		Where("discovery_path = ? AND has_result = ? AND status NOT IN ?",
			discoveryPath, false, terminalStatuses).
		Pluck("run_id", &ids).Error; err != nil {
		return nil, fmt.Errorf("listing incomplete run ids: %w", err)
	}

	return ids, nil
}

// UpsertTestDuration inserts or updates a test duration record.
func (s *store) UpsertTestDuration(
	ctx context.Context, d *TestDuration,
) error {
	result := s.db.WithContext(ctx).
		Where("suite_hash = ? AND test_name = ? AND run_id = ?",
			d.SuiteHash, d.TestName, d.RunID).
		Assign(d).
		FirstOrCreate(d)
	if result.Error != nil {
		return fmt.Errorf("upserting test duration: %w", result.Error)
	}

	return nil
}

// BulkUpsertTestDurations inserts or updates multiple test duration records
// in a single transaction. For each record it deletes-then-creates to avoid
// the overhead of individual FirstOrCreate round-trips.
func (s *store) BulkUpsertTestDurations(
	ctx context.Context, durations []*TestDuration,
) error {
	if len(durations) == 0 {
		return nil
	}

	const batchSize = 100

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := 0; i < len(durations); i += batchSize {
			end := i + batchSize
			if end > len(durations) {
				end = len(durations)
			}

			batch := durations[i:end]

			if err := tx.CreateInBatches(batch, len(batch)).Error; err != nil {
				return fmt.Errorf("bulk inserting test durations: %w", err)
			}
		}

		return nil
	})
}

// ListTestDurationsBySuite returns all test duration entries for a suite hash.
func (s *store) ListTestDurationsBySuite(
	ctx context.Context, suiteHash string,
) ([]TestDuration, error) {
	var durations []TestDuration
	if err := s.db.WithContext(ctx).
		Where("suite_hash = ?", suiteHash).
		Find(&durations).Error; err != nil {
		return nil, fmt.Errorf("listing test durations: %w", err)
	}

	return durations, nil
}

// DeleteTestDurationsForRun removes all test duration entries for a run ID.
func (s *store) DeleteTestDurationsForRun(
	ctx context.Context, runID string,
) error {
	if err := s.db.WithContext(ctx).
		Where("run_id = ?", runID).
		Delete(&TestDuration{}).Error; err != nil {
		return fmt.Errorf("deleting test durations for run: %w", err)
	}

	return nil
}
