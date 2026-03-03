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
		&TestStat{},
		&TestStatsBlockLog{},
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
var terminalStatuses = []string{"completed", "failed", "cancelled", "container_died"}

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

// BulkUpsertTestStats inserts or updates multiple test stat records in a
// single transaction. For each record it deletes-then-creates to avoid the
// overhead of individual FirstOrCreate round-trips.
func (s *store) BulkUpsertTestStats(
	ctx context.Context, stats []*TestStat,
) error {
	if len(stats) == 0 {
		return nil
	}

	const batchSize = 100

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := 0; i < len(stats); i += batchSize {
			end := i + batchSize
			if end > len(stats) {
				end = len(stats)
			}

			batch := stats[i:end]

			if err := tx.CreateInBatches(batch, len(batch)).Error; err != nil {
				return fmt.Errorf("bulk inserting test stats: %w", err)
			}
		}

		return nil
	})
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
// in a single transaction using batched creates.
func (s *store) BulkInsertTestStatsBlockLogs(
	ctx context.Context, logs []*TestStatsBlockLog,
) error {
	if len(logs) == 0 {
		return nil
	}

	const batchSize = 100

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := 0; i < len(logs); i += batchSize {
			end := min(i+batchSize, len(logs))
			batch := logs[i:end]

			if err := tx.CreateInBatches(batch, len(batch)).Error; err != nil {
				return fmt.Errorf(
					"bulk inserting test stats block logs: %w", err,
				)
			}
		}

		return nil
	})
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
