package indexstore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/api/gormlogger"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
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
	ReplaceTestStats(
		ctx context.Context, runID string, stats []*TestStat,
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
	ReplaceTestStatsBlockLogs(
		ctx context.Context, runID string, logs []*TestStatsBlockLog,
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
	log    logrus.FieldLogger
	cfg    *config.APIDatabaseConfig
	db     *gorm.DB // write-only connection (single conn for SQLite)
	readDB *gorm.DB // read-only connection pool (concurrent readers)
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
	gormCfg := &gorm.Config{
		Logger: gormlogger.New(s.log, 500*time.Millisecond),
	}

	switch s.cfg.Driver {
	case "sqlite":
		if err := s.openSQLite(gormCfg); err != nil {
			return err
		}
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

		db, err := gorm.Open(postgres.Open(dsn), gormCfg)
		if err != nil {
			return fmt.Errorf("opening index database: %w", err)
		}

		s.db = db
		s.readDB = db
	default:
		return fmt.Errorf("unsupported database driver: %s", s.cfg.Driver)
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

// openSQLite opens the write and read GORM connections for SQLite.
// The write connection is limited to a single connection to prevent
// write contention. The read connection allows concurrent readers
// via WAL mode. For in-memory databases both point to the same
// instance since separate connections would create independent DBs.
func (s *store) openSQLite(gormCfg *gorm.Config) error {
	writeDB, err := gorm.Open(sqlite.Open(s.cfg.SQLite.Path), gormCfg)
	if err != nil {
		return fmt.Errorf("opening index database (write): %w", err)
	}

	writeSQLDB, err := writeDB.DB()
	if err != nil {
		return fmt.Errorf("getting underlying sql.DB (write): %w", err)
	}

	// Single writer prevents "database is locked" contention.
	writeSQLDB.SetMaxOpenConns(1)

	if err := applySQLitePragmas(writeDB); err != nil {
		return err
	}

	s.db = writeDB

	// In-memory databases cannot share state across separate
	// connections, so both reads and writes use the same instance.
	if s.cfg.SQLite.Path == ":memory:" ||
		strings.Contains(s.cfg.SQLite.Path, "mode=memory") {
		s.readDB = writeDB

		return nil
	}

	// File-backed SQLite: open a separate read pool so concurrent
	// readers are not blocked behind the single-writer connection.
	readDB, err := gorm.Open(
		sqlite.Open(s.cfg.SQLite.Path), gormCfg,
	)
	if err != nil {
		return fmt.Errorf("opening index database (read): %w", err)
	}

	readSQLDB, err := readDB.DB()
	if err != nil {
		return fmt.Errorf("getting underlying sql.DB (read): %w", err)
	}

	readSQLDB.SetMaxOpenConns(4)

	if err := applySQLitePragmas(readDB); err != nil {
		return err
	}

	s.readDB = readDB

	return nil
}

// Stop closes the underlying database connections.
func (s *store) Stop() error {
	// Close the read pool first (if it's a separate instance).
	if s.readDB != nil && s.readDB != s.db {
		readSQL, err := s.readDB.DB()
		if err != nil {
			return fmt.Errorf("getting underlying read db: %w", err)
		}

		if err := readSQL.Close(); err != nil {
			return fmt.Errorf("closing read db: %w", err)
		}
	}

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
	if err := s.readDB.WithContext(ctx).
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
	if err := s.readDB.WithContext(ctx).
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
	if err := s.readDB.WithContext(ctx).
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
	if err := s.readDB.WithContext(ctx).
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
	if err := s.readDB.WithContext(ctx).
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
	if err := s.readDB.WithContext(ctx).
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

// ReplaceTestStats atomically deletes old test stats for a run and inserts
// new ones in a single transaction with retry for transient SQLite errors.
func (s *store) ReplaceTestStats(
	ctx context.Context, runID string, stats []*TestStat,
) error {
	return s.withRetry(func() error {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("run_id = ?", runID).
				Delete(&TestStat{}).Error; err != nil {
				return fmt.Errorf(
					"deleting test stats for run: %w", err,
				)
			}

			if len(stats) == 0 {
				return nil
			}

			const batchSize = 100
			for i := 0; i < len(stats); i += batchSize {
				end := min(i+batchSize, len(stats))
				batch := stats[i:end]

				if err := tx.CreateInBatches(
					batch, len(batch),
				).Error; err != nil {
					return fmt.Errorf(
						"bulk inserting test stats: %w", err,
					)
				}
			}

			return nil
		})
	})
}

// ReplaceTestStatsBlockLogs atomically deletes old block logs for a run
// and inserts new ones in a single transaction with retry.
func (s *store) ReplaceTestStatsBlockLogs(
	ctx context.Context, runID string, logs []*TestStatsBlockLog,
) error {
	return s.withRetry(func() error {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("run_id = ?", runID).
				Delete(&TestStatsBlockLog{}).Error; err != nil {
				return fmt.Errorf(
					"deleting test stats block logs for run: %w",
					err,
				)
			}

			if len(logs) == 0 {
				return nil
			}

			const batchSize = 100
			for i := 0; i < len(logs); i += batchSize {
				end := min(i+batchSize, len(logs))
				batch := logs[i:end]

				if err := tx.CreateInBatches(
					batch, len(batch),
				).Error; err != nil {
					return fmt.Errorf(
						"bulk inserting test stats block logs: %w",
						err,
					)
				}
			}

			return nil
		})
	})
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
	q := applyQuery(s.readDB.WithContext(ctx), &Run{}, params)

	// When select is specified, scan into maps so the JSON response
	// only contains the requested columns (no zero-valued extras).
	if len(params.Select) > 0 {
		return scanMaps(q, params)
	}

	var totalPtr *int64

	if params.CountExact {
		var total int64
		if err := q.Count(&total).Error; err != nil {
			return nil, fmt.Errorf("counting runs: %w", err)
		}

		totalPtr = &total
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
		Total:  totalPtr,
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
		s.readDB.WithContext(ctx), &TestStat{}, params,
	)

	// When select is specified, scan into maps so the JSON response
	// only contains the requested columns (no zero-valued extras).
	if len(params.Select) > 0 {
		return scanMaps(q, params)
	}

	var totalPtr *int64

	if params.CountExact {
		var total int64
		if err := q.Count(&total).Error; err != nil {
			return nil, fmt.Errorf("counting test stats: %w", err)
		}

		totalPtr = &total
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
		Total:  totalPtr,
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
		s.readDB.WithContext(ctx), &TestStatsBlockLog{}, params,
	)

	// When select is specified, scan into maps so the JSON response
	// only contains the requested columns (no zero-valued extras).
	if len(params.Select) > 0 {
		return scanMaps(q, params)
	}

	var totalPtr *int64

	if params.CountExact {
		var total int64
		if err := q.Count(&total).Error; err != nil {
			return nil, fmt.Errorf(
				"counting test stats block logs: %w", err,
			)
		}

		totalPtr = &total
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
		Total:  totalPtr,
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
	q := applyQuery(s.readDB.WithContext(ctx), &Suite{}, params)

	// When select is specified, scan into maps so the JSON response
	// only contains the requested columns (no zero-valued extras).
	if len(params.Select) > 0 {
		return scanMaps(q, params)
	}

	var totalPtr *int64

	if params.CountExact {
		var total int64
		if err := q.Count(&total).Error; err != nil {
			return nil, fmt.Errorf("counting suites: %w", err)
		}

		totalPtr = &total
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
		Total:  totalPtr,
		Limit:  params.Limit,
		Offset: params.Offset,
	}, nil
}

// withRetry retries fn up to 3 times on transient SQLite errors
// (database locked, disk I/O) with exponential backoff.
func (s *store) withRetry(fn func() error) error {
	const maxAttempts = 3

	backoffs := [...]time.Duration{
		100 * time.Millisecond,
		500 * time.Millisecond,
		2500 * time.Millisecond,
	}

	var err error

	for attempt := range maxAttempts {
		err = fn()
		if err == nil {
			return nil
		}

		if !isSQLiteTransient(err) {
			return err
		}

		if attempt < maxAttempts-1 {
			s.log.WithError(err).WithField("attempt", attempt+1).
				Warn("Transient SQLite error, retrying")
			time.Sleep(backoffs[attempt])
		}
	}

	return err
}

// applySQLitePragmas sets performance and reliability pragmas on a
// SQLite GORM connection.
func applySQLitePragmas(db *gorm.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA temp_store=MEMORY",
		"PRAGMA cache_size=-64000",
		"PRAGMA mmap_size=268435456",
	}

	for _, p := range pragmas {
		if err := db.Exec(p).Error; err != nil {
			return fmt.Errorf("setting pragma %q: %w", p, err)
		}
	}

	return nil
}

// isSQLiteTransient returns true for transient SQLite errors that may
// succeed on retry.
func isSQLiteTransient(err error) bool {
	msg := err.Error()

	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "disk I/O error")
}

// scanMaps scans query results into []map[string]any so only the selected
// columns appear in the JSON response.
func scanMaps(
	q *gorm.DB, params *QueryParams,
) (*QueryResult, error) {
	var totalPtr *int64

	if params.CountExact {
		var total int64
		if err := q.Count(&total).Error; err != nil {
			return nil, fmt.Errorf("counting rows: %w", err)
		}

		totalPtr = &total
	}

	var rows []map[string]any
	if err := q.Offset(params.Offset).
		Limit(params.Limit).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("querying rows: %w", err)
	}

	return &QueryResult{
		Data:   rows,
		Total:  totalPtr,
		Limit:  params.Limit,
		Offset: params.Offset,
	}, nil
}
