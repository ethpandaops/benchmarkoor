package indexstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/api/gormlogger"
	"github.com/ethpandaops/benchmarkoor/pkg/api/sqlitedsn"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Store provides persistence for the indexed benchmark data.
type Store interface {
	Start(ctx context.Context) error
	Stop() error

	UpsertRun(ctx context.Context, run *Run) error
	UpsertLiveRun(ctx context.Context, report *LiveRunReport) error
	ListLiveRuns(ctx context.Context) ([]LiveRun, error)
	DeleteLiveRun(ctx context.Context, discoveryPath, runID string) error
	DeleteStaleLiveRuns(ctx context.Context, olderThan time.Time) ([]string, error)
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
	ListTestStatsBySuiteRecent(
		ctx context.Context, suiteHash string, maxRunsPerClient int,
	) ([]TestStat, error)
	ListTestHistory(
		ctx context.Context, suiteHash, testName string, maxRunsPerClient int,
	) ([]TestHistoryEntry, error)
	ListRecentRunIDs(
		ctx context.Context, suiteHash string, maxRunsPerClient int,
	) ([]string, error)
	ScanTestChangeStats(
		ctx context.Context, suiteHash string, runIDs []string,
		fn func(*TestChangeStat),
	) error
	DeleteTestStatsForRun(ctx context.Context, runID string) error

	ListAllRuns(ctx context.Context) ([]Run, error)

	// RunsGeneration returns a monotonic counter that is bumped on every
	// mutation of the runs table. Callers can use it as a cheap cache key to
	// detect whether the runs list has changed since a prior read.
	RunsGeneration() uint64

	// TestStatsGeneration returns a counter that is bumped each time the
	// test stats of a run of the suite are replaced. Callers use it with the
	// run list as a cache key: a re-index rewrites the stats of a run but
	// keeps the run list the same.
	TestStatsGeneration(suiteHash string) uint64

	GetRunByRunID(ctx context.Context, runID string) (*Run, error)
	DeleteRun(ctx context.Context, runID string) error
	DeleteRunCascade(ctx context.Context, runID string) error
	DeleteOrphanedSuite(ctx context.Context, suiteHash string) error

	// Run deletion queue. See deletion.go.
	MarkRunForDeletion(ctx context.Context, runID string) error
	UnmarkRunForDeletion(ctx context.Context, runID string) error
	MarkRunsForDeletionBySuite(
		ctx context.Context, suiteHash string,
	) (*SuiteDeletionResult, error)
	ListRunsPendingDeletion(ctx context.Context) ([]Run, error)
	SetRunDeletionError(ctx context.Context, runID, msg string) error

	// DatabaseStats reports the size and contents of the index database.
	// It uses only cheap queries, so it is safe to call per request: the
	// per-test tables get estimated row counts. See dbstats.go.
	DatabaseStats(ctx context.Context) (*DatabaseStats, error)

	// Index failures: runs storage exposes that the indexer cannot index,
	// plus their own deletion queue. See index_failure.go.
	RecordIndexFailure(
		ctx context.Context, discoveryPath, runID, msg string,
	) error
	ClearIndexFailure(
		ctx context.Context, discoveryPath, runID string,
	) error
	ListIndexFailuresByPath(
		ctx context.Context, discoveryPath string,
	) ([]IndexFailure, error)
	ListIndexFailures(
		ctx context.Context, limit, offset int,
	) ([]IndexFailure, error)
	CountIndexFailures(ctx context.Context) (int64, error)
	MarkIndexFailureForDeletion(ctx context.Context, runID string) error
	UnmarkIndexFailureForDeletion(ctx context.Context, runID string) error
	MarkAllIndexFailuresForDeletion(ctx context.Context) (int64, error)
	UnmarkAllIndexFailuresForDeletion(ctx context.Context) (int64, error)
	ListIndexFailuresPendingDeletion(
		ctx context.Context,
	) ([]IndexFailure, error)
	SetIndexFailureDeletionError(
		ctx context.Context, discoveryPath, runID, msg string,
	) error

	// Indexer passes: one row per finished indexing pass, so the admin page
	// can show how long they take and what they do. See indexer_pass.go.
	RecordIndexerPass(ctx context.Context, pass *IndexerPass) error
	ListIndexerPasses(
		ctx context.Context, limit int,
	) ([]IndexerPass, error)

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

	// runsGen is bumped on every runs-table mutation so readers can cheaply
	// detect staleness. See RunsGeneration.
	runsGen atomic.Uint64

	// testStatsGen holds a counter per suite hash. See TestStatsGeneration.
	testStatsGenMu sync.Mutex
	testStatsGen   map[string]uint64
}

// migratedModels is every model the index database owns, in the order they
// are migrated. DatabaseStats walks the same list, so a new table shows up in
// the admin report without anyone remembering to add it there.
var migratedModels = []struct{ value any }{
	{value: &Run{}},
	{value: &TestStat{}},
	{value: &TestStatsBlockLog{}},
	{value: &Suite{}},
	{value: &LiveRun{}},
	{value: &IndexFailure{}},
	{value: &IndexerPass{}},
}

// migratedValues returns the models as AutoMigrate wants them.
func migratedValues() []any {
	values := make([]any, 0, len(migratedModels))
	for _, model := range migratedModels {
		values = append(values, model.value)
	}

	return values
}

// tableName resolves a model to its table name through GORM's naming
// strategy, so the report never hardcodes a name the schema could change.
func (s *store) tableName(model any) (string, error) {
	stmt := &gorm.Statement{DB: s.readDB}
	if err := stmt.Parse(model); err != nil {
		return "", fmt.Errorf("resolving table name: %w", err)
	}

	return stmt.Table, nil
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
		migratedValues()...,
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
	dsn := sqlitedsn.Build(s.cfg.SQLite.Path, sqlitePragmas)

	writeDB, err := gorm.Open(sqlite.Open(dsn), gormCfg)
	if err != nil {
		return fmt.Errorf("opening index database (write): %w", err)
	}

	writeSQLDB, err := writeDB.DB()
	if err != nil {
		return fmt.Errorf("getting underlying sql.DB (write): %w", err)
	}

	// Single writer prevents "database is locked" contention.
	writeSQLDB.SetMaxOpenConns(1)

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
	readDB, err := gorm.Open(sqlite.Open(dsn), gormCfg)
	if err != nil {
		return fmt.Errorf("opening index database (read): %w", err)
	}

	readSQLDB, err := readDB.DB()
	if err != nil {
		return fmt.Errorf("getting underlying sql.DB (read): %w", err)
	}

	readSQLDB.SetMaxOpenConns(4)

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

// runUpsertColumns lists the columns a re-index overwrites on conflict. It is
// every column except the primary key (id), the conflict target
// (discovery_path, run_id), and indexed_at. Listing them explicitly (rather
// than UpdateAll) still overwrites fields reset to their zero value (e.g.
// tests_failed back to 0, a cleared status), which a struct-based Assign/Updates
// would skip — while preserving indexed_at as the original first-index time.
// reindexed_at is updated so each re-index records when it happened.
var runUpsertColumns = []string{
	"timestamp", "timestamp_end", "suite_hash", "status",
	"termination_reason", "has_result", "instance_id", "client",
	"image", "rollback_strategy", "tests_total", "tests_passed",
	"tests_failed", "steps_json", "metadata_json", "reindexed_at",
}

// UpsertRun inserts or updates a run record keyed by discovery_path + run_id.
//
// It uses an INSERT ... ON CONFLICT DO UPDATE so a re-index overwrites the run's
// columns, including fields reset to their zero value. The original indexed_at
// is deliberately preserved (see runUpsertColumns); only reindexed_at tracks the
// latest re-index.
func (s *store) UpsertRun(ctx context.Context, run *Run) error {
	if err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "discovery_path"}, {Name: "run_id"}},
			DoUpdates: clause.AssignmentColumns(runUpsertColumns),
		}).
		Create(run).Error; err != nil {
		return fmt.Errorf("upserting run: %w", err)
	}

	s.runsGen.Add(1)

	return nil
}

// UpsertLiveRun applies a status report from a runner to the live_runs
// table. The composite key (discovery_path, run_id) identifies the row.
// Every report refreshes last_reported_at so the stale watcher can detect
// abandoned runs.
func (s *store) UpsertLiveRun(ctx context.Context, r *LiveRunReport) error {
	now := time.Now().UTC()

	// Always-written fields. We use Updates() with a map so an empty map is
	// not interpreted as "clear all fields".
	fields := map[string]any{
		"timestamp":                  r.Timestamp,
		"timestamp_end":              r.TimestampEnd,
		"suite_hash":                 r.SuiteHash,
		"status":                     r.Status,
		"termination_reason":         r.TerminationReason,
		"instance_id":                r.InstanceID,
		"client":                     r.Client,
		"image":                      r.Image,
		"rollback_strategy":          r.RollbackStrategy,
		"tests_total":                r.TestsTotal,
		"tests_passed":               r.TestsPassed,
		"tests_failed":               r.TestsFailed,
		"total_gas_used":             r.TotalGasUsed,
		"total_gas_used_duration_ns": r.TotalGasUsedDurationNs,
		"last_reported_at":           now,
	}

	if r.Metadata != nil {
		b, err := json.Marshal(r.Metadata)
		if err != nil {
			return fmt.Errorf("marshalling metadata: %w", err)
		}

		fields["metadata_json"] = string(b)
	}

	// Only overwrite config_json when the runner actually supplied one.
	// Empty Config means "no update" — preserve whatever was last stored
	// so a later zero-config report doesn't wipe an earlier good one.
	if len(r.Config) > 0 {
		fields["config_json"] = string(r.Config)
	}

	// Per-test gas map. Always write when the runner sends one (even an
	// empty map is meaningful — it means "no tests have completed yet"
	// and the heatmap should clear). Nil means the runner hasn't started
	// reporting tests, in which case we preserve the previous value.
	if r.Tests != nil {
		b, err := json.Marshal(r.Tests)
		if err != nil {
			return fmt.Errorf("marshalling tests: %w", err)
		}

		fields["tests_json"] = string(b)
	}

	seed := &LiveRun{
		DiscoveryPath:  r.DiscoveryPath,
		RunID:          r.RunID,
		LastReportedAt: now,
	}

	result := s.db.WithContext(ctx).
		Where("discovery_path = ? AND run_id = ?", r.DiscoveryPath, r.RunID).
		Assign(fields).
		FirstOrCreate(seed)
	if result.Error != nil {
		return fmt.Errorf("upserting live run: %w", result.Error)
	}

	return nil
}

// ListLiveRuns returns all live runs ordered by their start timestamp
// descending (newest first).
func (s *store) ListLiveRuns(ctx context.Context) ([]LiveRun, error) {
	var runs []LiveRun
	if err := s.readDB.WithContext(ctx).
		Order("timestamp DESC").
		Find(&runs).Error; err != nil {
		return nil, fmt.Errorf("listing live runs: %w", err)
	}

	return runs, nil
}

// DeleteLiveRun removes a live run row. Called by the on-disk indexer
// once it has indexed the canonical Run, so the live entry doesn't show
// up alongside the indexed one in the UI.
func (s *store) DeleteLiveRun(ctx context.Context, dp, runID string) error {
	if err := s.db.WithContext(ctx).
		Where("discovery_path = ? AND run_id = ?", dp, runID).
		Delete(&LiveRun{}).Error; err != nil {
		return fmt.Errorf("deleting live run: %w", err)
	}

	return nil
}

// DeleteStaleLiveRuns removes any live run whose last_reported_at is
// older than the given threshold. Returns the run_ids that were
// deleted so callers (e.g. the stale watcher) can clean up associated
// state like WebSocket hubs.
func (s *store) DeleteStaleLiveRuns(ctx context.Context, olderThan time.Time) ([]string, error) {
	// First, collect the run_ids that will be purged.
	var stale []LiveRun
	if err := s.db.WithContext(ctx).
		Select("run_id").
		Where("last_reported_at < ?", olderThan).
		Find(&stale).Error; err != nil {
		return nil, fmt.Errorf("finding stale live runs: %w", err)
	}

	if len(stale) == 0 {
		return nil, nil
	}

	// Delete the rows.
	if err := s.db.WithContext(ctx).
		Where("last_reported_at < ?", olderThan).
		Delete(&LiveRun{}).Error; err != nil {
		return nil, fmt.Errorf("deleting stale live runs: %w", err)
	}

	ids := make([]string, len(stale))
	for i, r := range stale {
		ids[i] = r.RunID
	}

	return ids, nil
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

// RunsGeneration returns the current runs-table generation counter.
func (s *store) RunsGeneration() uint64 {
	return s.runsGen.Load()
}

// TestStatsGeneration returns the test stats counter of a suite.
func (s *store) TestStatsGeneration(suiteHash string) uint64 {
	s.testStatsGenMu.Lock()
	defer s.testStatsGenMu.Unlock()

	return s.testStatsGen[suiteHash]
}

// bumpTestStatsGeneration bumps the counter of each suite of stats. It runs
// after the write commits: a reader that reads the counter before the stats
// then never caches old stats under the new value.
func (s *store) bumpTestStatsGeneration(stats []*TestStat) {
	s.testStatsGenMu.Lock()
	defer s.testStatsGenMu.Unlock()

	if s.testStatsGen == nil {
		s.testStatsGen = make(map[string]uint64, 16)
	}

	bumped := make(map[string]struct{}, 1)

	for _, st := range stats {
		if _, ok := bumped[st.SuiteHash]; ok {
			continue
		}

		bumped[st.SuiteHash] = struct{}{}
		s.testStatsGen[st.SuiteHash]++
	}
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

	s.runsGen.Add(1)

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

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
	}); err != nil {
		return err
	}

	s.runsGen.Add(1)

	return nil
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
var terminalStatuses = []string{
	RunStatusCompleted,
	RunStatusFailed,
	RunStatusCancelled,
	RunStatusContainerDied,
	RunStatusTimedOut,
}

// ListIncompleteRunIDs returns run IDs where the result has not been indexed
// and the run is still potentially in progress. A run is considered
// incomplete only when it has a non-empty, non-terminal status — empty
// status means the run was abandoned and will never produce a result.
// Runs queued for deletion are skipped: re-indexing them is wasted work.
func (s *store) ListIncompleteRunIDs(
	ctx context.Context, discoveryPath string,
) ([]string, error) {
	var ids []string
	if err := s.readDB.WithContext(ctx).
		Model(&Run{}).
		Where("discovery_path = ? AND has_result = ? AND status != '' AND status NOT IN ?"+
			" AND deletion_requested_at IS NULL",
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

// clientRunRow holds the lightweight result of the client/run_id query used
// by ListTestStatsBySuiteRecent.
type clientRunRow struct {
	Client   string
	RunID    string
	RunStart int64
}

// ListTestStatsBySuiteRecent returns test stats for the N most recent runs
// per client within a suite. This avoids fetching the entire suite history
// which can be millions of rows for large suites.
func (s *store) ListTestStatsBySuiteRecent(
	ctx context.Context, suiteHash string, maxRunsPerClient int,
) ([]TestStat, error) {
	runIDs, err := s.ListRecentRunIDs(ctx, suiteHash, maxRunsPerClient)
	if err != nil {
		return nil, err
	}

	// Fetch full test_stats for the selected run_ids. Batch the IN clause to
	// stay within SQLite's bind variable limit.
	stats := make([]TestStat, 0, len(runIDs)*100)

	for batch := range slices.Chunk(runIDs, testStatsBatchSize) {
		var batchStats []TestStat
		if err := s.readDB.WithContext(ctx).
			Where("suite_hash = ? AND run_id IN ?",
				suiteHash, batch).
			Find(&batchStats).Error; err != nil {
			return nil, fmt.Errorf(
				"listing test stats for recent runs: %w", err,
			)
		}

		stats = append(stats, batchStats...)
	}

	return stats, nil
}

// testStatsBatchSize caps the run IDs in one IN clause, to stay within
// SQLite's bind variable limit.
const testStatsBatchSize = 500

// ListRecentRunIDs returns the IDs of the N most recent runs per client of a
// suite that have test stats, newest first.
func (s *store) ListRecentRunIDs(
	ctx context.Context, suiteHash string, maxRunsPerClient int,
) ([]string, error) {
	// Step 1: list the suite's runs, newest first, from the runs table.
	//
	// The indexer writes a run's test_stats with the client and run_start of
	// its runs row, so the runs table answers "which runs, which client, and
	// when" on its own. It holds one row per run, not one per test. Grouping
	// test_stats instead sorted every row of the suite, which is 35M rows for
	// the largest suite in production and several GB of sorter memory.
	//
	// The EXISTS keeps out runs with no test stats, for example a run whose
	// result.json has no step data. It is one seek on idx_td_suite_test_run
	// per run, whose (suite_hash, run_id) prefix matches it.
	//
	// We group by client and run_id so that a run indexed under two discovery
	// paths still takes a single per-client slot.
	var rows []clientRunRow
	if err := s.readDB.WithContext(ctx).
		Model(&Run{}).
		Select("client, run_id, MAX(timestamp) AS run_start").
		Where("suite_hash = ?", suiteHash).
		Where("EXISTS (SELECT 1 FROM test_stats ts " +
			"WHERE ts.suite_hash = runs.suite_hash " +
			"AND ts.run_id = runs.run_id)").
		Group("client, run_id").
		// run_id is a deterministic tie-breaker so that, when more runs than the
		// per-client cap share the same run_start, the same runs are kept on
		// every call and across SQLite and Postgres.
		Order("run_start DESC, run_id DESC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf(
			"listing recent client runs: %w", err,
		)
	}

	// Step 2: group by client, take top N run_ids per client.
	clientRuns := make(map[string]int, 8)
	runIDs := make([]string, 0, maxRunsPerClient*4)

	for i := range rows {
		client := rows[i].Client
		if clientRuns[client] >= maxRunsPerClient {
			continue
		}

		clientRuns[client]++

		runIDs = append(runIDs, rows[i].RunID)
	}

	return runIDs, nil
}

// TestChangeStat is the part of a test stat that the change detection
// reads: the gas, the time and the time to reject of each step.
type TestChangeStat struct {
	TestName            string
	Client              string
	RunID               string
	RunStart            int64
	SetupGasUsed        uint64
	SetupTimeNs         int64
	SetupRejectedTimeNs int64
	TestGasUsed         uint64
	TestTimeNs          int64
	TestRejectedTimeNs  int64
}

// ScanTestChangeStats calls fn with the change columns of each test stat
// of the given runs of a suite. It reads 10 of the about 40 columns, so it is
// much cheaper than ListTestStatsBySuiteRecent for a suite with many tests.
//
// A large suite has more than a million of these rows, so they are not
// collected in a slice: fn keeps what it needs. The stat is only valid during
// the call, but its strings stay valid.
func (s *store) ScanTestChangeStats(
	ctx context.Context, suiteHash string, runIDs []string,
	fn func(*TestChangeStat),
) error {
	// Each test name, client and run ID repeats in many rows. One copy of
	// each keeps a large read at a fraction of the memory.
	strs := make(map[string]string, 4096)

	for batch := range slices.Chunk(runIDs, testStatsBatchSize) {
		if err := s.scanTestChangeStats(ctx, strs, suiteHash, batch, fn); err != nil {
			return fmt.Errorf("listing change test stats: %w", err)
		}
	}

	return nil
}

// scanTestChangeStats reads the change columns of one batch of runs. It
// scans the rows by hand, because the reflection of Find doubles the time of
// the read. strs holds one copy of each string.
func (s *store) scanTestChangeStats(
	ctx context.Context, strs map[string]string, suiteHash string, runIDs []string,
	fn func(*TestChangeStat),
) error {
	// Most columns allow NULL, which Scan cannot put into a number. Find
	// read a NULL as zero, and so does the COALESCE.
	rows, err := s.readDB.WithContext(ctx).
		Model(&TestStat{}).
		Select("test_name, COALESCE(client, ''), run_id, COALESCE(run_start, 0), "+
			"COALESCE(setup_gas_used, 0), COALESCE(setup_time_ns, 0), setup_rejected_time_ns, "+
			"COALESCE(test_gas_used, 0), COALESCE(test_time_ns, 0), test_rejected_time_ns").
		Where("suite_hash = ? AND run_id IN ?", suiteHash, runIDs).
		Rows()
	if err != nil {
		return err
	}

	defer func() { _ = rows.Close() }()

	intern := func(b sql.RawBytes) string {
		if v, ok := strs[string(b)]; ok {
			return v
		}

		v := string(b)
		strs[v] = v

		return v
	}

	var (
		st                      TestChangeStat
		testName, client, runID sql.RawBytes
	)

	for rows.Next() {
		if err := rows.Scan(
			&testName, &client, &runID, &st.RunStart,
			&st.SetupGasUsed, &st.SetupTimeNs, &st.SetupRejectedTimeNs,
			&st.TestGasUsed, &st.TestTimeNs, &st.TestRejectedTimeNs,
		); err != nil {
			return err
		}

		st.TestName, st.Client, st.RunID = intern(testName), intern(client), intern(runID)
		fn(&st)
	}

	return rows.Err()
}

// TestHistoryEntry is the test stat of one test in one run, with the client
// image of that run.
type TestHistoryEntry struct {
	TestStat
	Image string
}

// historyRunRow is one run of a suite, as listed by ListTestHistory.
type historyRunRow struct {
	Client   string
	RunID    string
	RunStart int64
	Image    string
}

// ListTestHistory returns the test stats of one test for the N most recent
// runs per client that ran it, newest first.
//
// It never scans test_stats. It lists the suite's runs from the runs table,
// then looks up the test in each run with one seek on idx_td_suite_test_run
// (suite_hash, run_id, test_name). It walks the runs newest first in batches
// and stops when every client has N entries, so a run without the test does
// not take a slot and old runs are not read.
func (s *store) ListTestHistory(
	ctx context.Context, suiteHash, testName string, maxRunsPerClient int,
) ([]TestHistoryEntry, error) {
	// A run indexed under two discovery paths has two runs rows. The group
	// keeps one row for it.
	var runs []historyRunRow
	if err := s.readDB.WithContext(ctx).
		Model(&Run{}).
		Select("client, run_id, MAX(timestamp) AS run_start, "+
			"MAX(image) AS image").
		Where("suite_hash = ?", suiteHash).
		Group("client, run_id").
		Order("run_start DESC, run_id DESC").
		Find(&runs).Error; err != nil {
		return nil, fmt.Errorf("listing suite runs: %w", err)
	}

	clientRuns := make(map[string]int, 8)
	for i := range runs {
		clientRuns[runs[i].Client] = 0
	}

	fullClients := 0
	history := make([]TestHistoryEntry, 0, min(len(runs), maxRunsPerClient*len(clientRuns)))

	const batchSize = 500

	for i := 0; i < len(runs) && fullClients < len(clientRuns); i += batchSize {
		batch := runs[i:min(i+batchSize, len(runs))]

		runIDs := make([]string, 0, len(batch))
		for j := range batch {
			if clientRuns[batch[j].Client] < maxRunsPerClient {
				runIDs = append(runIDs, batch[j].RunID)
			}
		}

		if len(runIDs) == 0 {
			continue
		}

		var stats []TestStat
		if err := s.readDB.WithContext(ctx).
			Where("suite_hash = ? AND run_id IN ? AND test_name = ?",
				suiteHash, runIDs, testName).
			Find(&stats).Error; err != nil {
			return nil, fmt.Errorf("listing test stats for runs: %w", err)
		}

		byRun := make(map[string]*TestStat, len(stats))
		for j := range stats {
			byRun[stats[j].RunID] = &stats[j]
		}

		// Keep the batch order, so the per-client cap keeps the newest runs.
		for j := range batch {
			run := &batch[j]

			stat, ok := byRun[run.RunID]
			if !ok || clientRuns[run.Client] >= maxRunsPerClient {
				continue
			}

			clientRuns[run.Client]++
			if clientRuns[run.Client] == maxRunsPerClient {
				fullClients++
			}

			history = append(history, TestHistoryEntry{
				TestStat: *stat,
				Image:    run.Image,
			})
		}
	}

	return history, nil
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
	err := s.withRetry(func() error {
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
	if err != nil {
		return err
	}

	// A run with no stats left drops out of the suite's run list, which
	// changes a cache key on its own. Only a run with stats needs the bump.
	s.bumpTestStatsGeneration(stats)

	return nil
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
// Uses a map for Assign so update values are not overwritten when
// FirstOrCreate populates the struct with the existing DB record.
func (s *store) UpsertSuite(ctx context.Context, suite *Suite) error {
	result := s.db.WithContext(ctx).
		Where("suite_hash = ?", suite.SuiteHash).
		Assign(map[string]any{
			"discovery_path": suite.DiscoveryPath,
			"name":           suite.Name,
			"tests_total":    suite.TestsTotal,
			"indexed_at":     suite.IndexedAt,
		}).
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

// sqlitePragmas are the pragmas every index-database connection needs. They
// ride in the DSN rather than a one-off Exec because SQLite applies them per
// connection: an Exec reaches the one pooled connection that served it, and
// the read pool opens four. A connection that missed temp_store=MEMORY spills
// the scratch b-tree of a GROUP BY or an ORDER BY to a temp file, which fails
// with "disk I/O error" on a container whose root filesystem is read-only.
// One that missed cache_size reads a large database through a 2MB page cache.
//
// cache_size and mmap_size are per connection, so the pool can hold five
// copies of each. Change them with that multiplier in mind.
var sqlitePragmas = []sqlitedsn.Pragma{
	{Name: "journal_mode", Value: "WAL"},
	{Name: "synchronous", Value: "NORMAL"},
	{Name: "busy_timeout", Value: "5000"},
	{Name: "foreign_keys", Value: "ON"},
	{Name: "temp_store", Value: "MEMORY"},
	{Name: "cache_size", Value: "-64000"},
	{Name: "mmap_size", Value: "268435456"},
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
