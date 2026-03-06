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

	UpsertTestStat(ctx context.Context, d *TestStat) error
	BulkUpsertTestStats(
		ctx context.Context, stats []*TestStat,
	) error
	ListTestStatsBySuite(
		ctx context.Context, suiteHash string,
	) ([]TestStat, error)
	DeleteTestStatsForRun(ctx context.Context, runID string) error

	ListAllRuns(ctx context.Context) ([]Run, error)

	GetRunByRunID(ctx context.Context, runID string) (*Run, error)
	DeleteRun(ctx context.Context, runID string) error
	DeleteRunCascade(ctx context.Context, runID string) error
	DeleteOrphanedSuite(ctx context.Context, suiteHash string) error

	UpsertSuite(ctx context.Context, suite *Suite) error

	BulkInsertTestStatsBlockLogs(
		ctx context.Context, logs []*TestStatsBlockLog,
	) error
	DeleteTestStatsBlockLogsForRun(ctx context.Context, runID string) error

	QueryRuns(ctx context.Context, params *QueryParams) (*QueryResult, error)
	QueryTestStats(
		ctx context.Context, params *QueryParams,
	) (*QueryResult, error)
	QueryTestStatsBlockLogs(
		ctx context.Context, params *QueryParams,
	) (*QueryResult, error)
	QuerySuites(
		ctx context.Context, params *QueryParams,
	) (*QueryResult, error)
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

	// Set SQLite pragmas for performance and reliability.
	if s.cfg.Driver == "sqlite" {
		pragmas := []string{
			"PRAGMA journal_mode=WAL",
			"PRAGMA synchronous=NORMAL",
			"PRAGMA busy_timeout=5000",
			"PRAGMA foreign_keys=ON",
			"PRAGMA temp_store=MEMORY",
		}
		for _, p := range pragmas {
			if err := s.db.Exec(p).Error; err != nil {
				return fmt.Errorf("setting pragma %q: %w", p, err)
			}
		}
	}

	if err := s.db.WithContext(ctx).AutoMigrate(
		&Run{},
		&TestStat{},
		&TestStatsBlockLog{},
		&Suite{},
	); err != nil {
		return fmt.Errorf("running index migrations: %w", err)
	}

	// Drop legacy steps_json column from test_stats if it exists.
	if s.db.Migrator().HasColumn(&TestStat{}, "steps_json") {
		if err := s.db.Migrator().DropColumn(&TestStat{}, "steps_json"); err != nil {
			s.log.WithError(err).Warn("Failed to drop steps_json column")
		}
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

// GetRunByRunID returns a single run by its run ID.
func (s *store) GetRunByRunID(
	ctx context.Context, runID string,
) (*Run, error) {
	var run Run
	if err := s.db.WithContext(ctx).
		Where("run_id = ?", runID).
		First(&run).Error; err != nil {
		return nil, fmt.Errorf("getting run by run_id: %w", err)
	}

	return &run, nil
}

// DeleteRun removes a run record by run ID.
func (s *store) DeleteRun(
	ctx context.Context, runID string,
) error {
	if err := s.db.WithContext(ctx).
		Where("run_id = ?", runID).
		Delete(&Run{}).Error; err != nil {
		return fmt.Errorf("deleting run: %w", err)
	}

	return nil
}

// DeleteRunCascade deletes a run and all related rows (test_stats,
// test_stats_block_logs, and the orphaned suite) in a single transaction.
func (s *store) DeleteRunCascade(
	ctx context.Context, runID string,
) error {
	// Look up the run first so we know the suite_hash for orphan cleanup.
	var run Run
	if err := s.db.WithContext(ctx).
		Where("run_id = ?", runID).
		First(&run).Error; err != nil {
		return fmt.Errorf("looking up run: %w", err)
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("run_id = ?", runID).
			Delete(&TestStat{}).Error; err != nil {
			return fmt.Errorf("deleting test stats: %w", err)
		}

		if err := tx.Where("run_id = ?", runID).
			Delete(&TestStatsBlockLog{}).Error; err != nil {
			return fmt.Errorf("deleting block logs: %w", err)
		}

		if err := tx.Where("run_id = ?", runID).
			Delete(&Run{}).Error; err != nil {
			return fmt.Errorf("deleting run: %w", err)
		}

		// Clean up suite if no other runs reference it.
		if run.SuiteHash != "" {
			var count int64
			if err := tx.Model(&Run{}).
				Where("suite_hash = ?", run.SuiteHash).
				Count(&count).Error; err != nil {
				return fmt.Errorf("counting runs for suite: %w", err)
			}

			if count == 0 {
				if err := tx.Where("suite_hash = ?", run.SuiteHash).
					Delete(&Suite{}).Error; err != nil {
					return fmt.Errorf("deleting orphaned suite: %w", err)
				}
			}
		}

		return nil
	})
}

// DeleteOrphanedSuite deletes a suite if no runs reference it anymore.
func (s *store) DeleteOrphanedSuite(
	ctx context.Context, suiteHash string,
) error {
	if suiteHash == "" {
		return nil
	}

	var count int64
	if err := s.db.WithContext(ctx).
		Model(&Run{}).
		Where("suite_hash = ?", suiteHash).
		Count(&count).Error; err != nil {
		return fmt.Errorf("counting runs for suite: %w", err)
	}

	if count > 0 {
		return nil
	}

	if err := s.db.WithContext(ctx).
		Where("suite_hash = ?", suiteHash).
		Delete(&Suite{}).Error; err != nil {
		return fmt.Errorf("deleting orphaned suite: %w", err)
	}

	return nil
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
var terminalStatuses = []string{"completed", "failed", "cancelled", "container_died", "timeout"}

// ListIncompleteRunIDs returns run IDs where the result has not been indexed
// and the run is still potentially in progress. A run is considered
// incomplete only when it has a non-empty, non-terminal status — empty
// status means the run was abandoned and will never produce a result.
func (s *store) ListIncompleteRunIDs(
	ctx context.Context, discoveryPath string,
) ([]string, error) {
	var ids []string
	if err := s.db.WithContext(ctx).
		Model(&Run{}).
		Where("discovery_path = ? AND has_result = ? AND status != '' AND status NOT IN ?",
			discoveryPath, false, terminalStatuses).
		Pluck("run_id", &ids).Error; err != nil {
		return nil, fmt.Errorf("listing incomplete run ids: %w", err)
	}

	return ids, nil
}

// UpsertTestStat inserts or updates a test stat record.
func (s *store) UpsertTestStat(
	ctx context.Context, d *TestStat,
) error {
	result := s.db.WithContext(ctx).
		Where("suite_hash = ? AND test_name = ? AND run_id = ?",
			d.SuiteHash, d.TestName, d.RunID).
		Assign(d).
		FirstOrCreate(d)
	if result.Error != nil {
		return fmt.Errorf("upserting test stat: %w", result.Error)
	}

	return nil
}

// BulkUpsertTestStats inserts multiple test stat records in batches.
// Each batch is auto-committed independently to keep WAL/journal pressure
// low on block-storage filesystems. The caller is expected to delete old
// records before calling this (delete-then-create pattern).
func (s *store) BulkUpsertTestStats(
	ctx context.Context, stats []*TestStat,
) error {
	if len(stats) == 0 {
		return nil
	}

	const batchSize = 100

	db := s.db.WithContext(ctx)

	for i := 0; i < len(stats); i += batchSize {
		end := min(i+batchSize, len(stats))
		batch := stats[i:end]

		if err := db.CreateInBatches(batch, len(batch)).Error; err != nil {
			return fmt.Errorf("bulk inserting test stats: %w", err)
		}
	}

	return nil
}

// ListTestStatsBySuite returns all test stat entries for a suite hash.
func (s *store) ListTestStatsBySuite(
	ctx context.Context, suiteHash string,
) ([]TestStat, error) {
	var stats []TestStat
	if err := s.db.WithContext(ctx).
		Where("suite_hash = ?", suiteHash).
		Find(&stats).Error; err != nil {
		return nil, fmt.Errorf("listing test stats: %w", err)
	}

	return stats, nil
}

// DeleteTestStatsForRun removes all test stat entries for a run ID.
func (s *store) DeleteTestStatsForRun(
	ctx context.Context, runID string,
) error {
	if err := s.db.WithContext(ctx).
		Where("run_id = ?", runID).
		Delete(&TestStat{}).Error; err != nil {
		return fmt.Errorf("deleting test stats for run: %w", err)
	}

	return nil
}

// BulkInsertTestStatsBlockLogs inserts multiple test stats block log records
// in batches. Each batch is auto-committed independently to keep
// WAL/journal pressure low. The caller deletes old records first.
func (s *store) BulkInsertTestStatsBlockLogs(
	ctx context.Context, logs []*TestStatsBlockLog,
) error {
	if len(logs) == 0 {
		return nil
	}

	const batchSize = 100

	db := s.db.WithContext(ctx)

	for i := 0; i < len(logs); i += batchSize {
		end := min(i+batchSize, len(logs))
		batch := logs[i:end]

		if err := db.CreateInBatches(batch, len(batch)).Error; err != nil {
			return fmt.Errorf(
				"bulk inserting test stats block logs: %w", err,
			)
		}
	}

	return nil
}

// DeleteTestStatsBlockLogsForRun removes all test stats block log entries
// for a run ID.
func (s *store) DeleteTestStatsBlockLogsForRun(
	ctx context.Context, runID string,
) error {
	if err := s.db.WithContext(ctx).
		Where("run_id = ?", runID).
		Delete(&TestStatsBlockLog{}).Error; err != nil {
		return fmt.Errorf(
			"deleting test stats block logs for run: %w", err,
		)
	}

	return nil
}

// UpsertSuite inserts or updates a suite record keyed by suite_hash.
func (s *store) UpsertSuite(ctx context.Context, suite *Suite) error {
	result := s.db.WithContext(ctx).
		Where("suite_hash = ?", suite.SuiteHash).
		Assign(suite).
		FirstOrCreate(suite)
	if result.Error != nil {
		return fmt.Errorf("upserting suite: %w", result.Error)
	}

	return nil
}

// QueryRuns executes a flexible query against the runs table using the
// validated QueryParams. It returns paginated results with a total count.
func (s *store) QueryRuns(
	ctx context.Context, params *QueryParams,
) (*QueryResult, error) {
	q := applyQuery(s.db.WithContext(ctx), &Run{}, params)

	// When select is specified, scan into maps so the JSON response
	// only contains the requested columns (no zero-valued extras).
	if len(params.Select) > 0 {
		return scanMaps(q, params)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("counting runs: %w", err)
	}

	var runs []Run
	if err := q.Offset(params.Offset).
		Limit(params.Limit).
		Find(&runs).Error; err != nil {
		return nil, fmt.Errorf("querying runs: %w", err)
	}

	data := make([]RunResponse, 0, len(runs))
	for i := range runs {
		data = append(data, toRunResponse(&runs[i]))
	}

	return &QueryResult{
		Data:   data,
		Total:  total,
		Limit:  params.Limit,
		Offset: params.Offset,
	}, nil
}

// QueryTestStats executes a flexible query against the test_stats table
// using the validated QueryParams. It returns paginated results with a
// total count.
func (s *store) QueryTestStats(
	ctx context.Context, params *QueryParams,
) (*QueryResult, error) {
	q := applyQuery(
		s.db.WithContext(ctx), &TestStat{}, params,
	)

	// When select is specified, scan into maps so the JSON response
	// only contains the requested columns (no zero-valued extras).
	if len(params.Select) > 0 {
		return scanMaps(q, params)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("counting test stats: %w", err)
	}

	var stats []TestStat
	if err := q.Offset(params.Offset).
		Limit(params.Limit).
		Find(&stats).Error; err != nil {
		return nil, fmt.Errorf("querying test stats: %w", err)
	}

	data := make([]TestStatResponse, 0, len(stats))
	for i := range stats {
		data = append(data, toTestStatResponse(&stats[i]))
	}

	return &QueryResult{
		Data:   data,
		Total:  total,
		Limit:  params.Limit,
		Offset: params.Offset,
	}, nil
}

// QueryTestStatsBlockLogs executes a flexible query against the
// test_stats_block_logs table using the validated QueryParams. It returns
// paginated results with a total count.
func (s *store) QueryTestStatsBlockLogs(
	ctx context.Context, params *QueryParams,
) (*QueryResult, error) {
	q := applyQuery(
		s.db.WithContext(ctx), &TestStatsBlockLog{}, params,
	)

	// When select is specified, scan into maps so the JSON response
	// only contains the requested columns (no zero-valued extras).
	if len(params.Select) > 0 {
		return scanMaps(q, params)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, fmt.Errorf(
			"counting test stats block logs: %w", err,
		)
	}

	var logs []TestStatsBlockLog
	if err := q.Offset(params.Offset).
		Limit(params.Limit).
		Find(&logs).Error; err != nil {
		return nil, fmt.Errorf(
			"querying test stats block logs: %w", err,
		)
	}

	data := make([]TestStatsBlockLogResponse, 0, len(logs))
	for i := range logs {
		data = append(data, toTestStatsBlockLogResponse(&logs[i]))
	}

	return &QueryResult{
		Data:   data,
		Total:  total,
		Limit:  params.Limit,
		Offset: params.Offset,
	}, nil
}

// QuerySuites executes a flexible query against the suites table using
// the validated QueryParams. It returns paginated results with a total
// count.
func (s *store) QuerySuites(
	ctx context.Context, params *QueryParams,
) (*QueryResult, error) {
	q := applyQuery(s.db.WithContext(ctx), &Suite{}, params)

	// When select is specified, scan into maps so the JSON response
	// only contains the requested columns (no zero-valued extras).
	if len(params.Select) > 0 {
		return scanMaps(q, params)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("counting suites: %w", err)
	}

	var suites []Suite
	if err := q.Offset(params.Offset).
		Limit(params.Limit).
		Find(&suites).Error; err != nil {
		return nil, fmt.Errorf("querying suites: %w", err)
	}

	data := make([]SuiteResponse, 0, len(suites))
	for i := range suites {
		data = append(data, toSuiteResponse(&suites[i]))
	}

	return &QueryResult{
		Data:   data,
		Total:  total,
		Limit:  params.Limit,
		Offset: params.Offset,
	}, nil
}

// scanMaps scans query results into []map[string]any so only the selected
// columns appear in the JSON response.
func scanMaps(
	q *gorm.DB, params *QueryParams,
) (*QueryResult, error) {
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("counting rows: %w", err)
	}

	var rows []map[string]any
	if err := q.Offset(params.Offset).
		Limit(params.Limit).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("querying rows: %w", err)
	}

	return &QueryResult{
		Data:   rows,
		Total:  total,
		Limit:  params.Limit,
		Offset: params.Offset,
	}, nil
}
