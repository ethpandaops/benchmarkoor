package indexstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// topSuiteLimit bounds the "biggest suites" breakdown. It exists to point an
// admin at what to delete, not to be a full listing.
const topSuiteLimit = 15

// DatabaseStats describes the index database, so an admin can tell whether a
// cleanup is due before the volume fills up.
type DatabaseStats struct {
	Driver string `json:"driver"`

	// Path is the SQLite file. Empty for other drivers.
	Path string `json:"path,omitempty"`

	// FileBytes is the database file itself. WALBytes is its write-ahead log,
	// which a long-running writer can leave surprisingly large.
	FileBytes int64 `json:"file_bytes,omitempty"`
	WALBytes  int64 `json:"wal_bytes,omitempty"`

	// PageSize, PageCount and FreePages come from SQLite's pragmas.
	// FreePages * PageSize is what a VACUUM would hand back.
	PageSize  int64 `json:"page_size,omitempty"`
	PageCount int64 `json:"page_count,omitempty"`
	FreePages int64 `json:"free_pages,omitempty"`

	// Volume describes the filesystem holding the database. This is the
	// number that decides whether a cleanup is urgent.
	VolumeTotalBytes int64 `json:"volume_total_bytes,omitempty"`
	VolumeFreeBytes  int64 `json:"volume_free_bytes,omitempty"`

	Tables    []TableStat  `json:"tables"`
	TopSuites []SuiteUsage `json:"top_suites"`

	// OldestRun and NewestRun are unix seconds, 0 when there are no runs.
	OldestRun int64 `json:"oldest_run,omitempty"`
	NewestRun int64 `json:"newest_run,omitempty"`
}

// TableStat is a row count for one table. Byte sizes per table are not
// reported: this build of SQLite has no dbstat virtual table, and a made-up
// estimate would be worse than an honest omission.
type TableStat struct {
	Name string `json:"name"`
	Rows int64  `json:"rows"`
}

// SuiteUsage is how much of the database one suite accounts for. Deleting a
// suite's runs cascades to its test stats and block logs, so these three
// numbers are what a cleanup actually frees.
type SuiteUsage struct {
	SuiteHash     string `json:"suite_hash"`
	Name          string `json:"name,omitempty"`
	DiscoveryPath string `json:"discovery_path,omitempty"`
	Runs          int64  `json:"runs"`
	TestStats     int64  `json:"test_stats"`
	BlockLogs     int64  `json:"block_logs"`
	// LastRun is unix seconds of the newest run, 0 when unknown.
	LastRun int64 `json:"last_run,omitempty"`
}

// DatabaseStats gathers the index database's size and contents. The row counts
// are full scans over a database that can hold tens of millions of rows, so
// callers are expected to cache the result rather than poll it.
func (s *store) DatabaseStats(ctx context.Context) (*DatabaseStats, error) {
	stats := &DatabaseStats{
		Driver:    s.cfg.Driver,
		Tables:    make([]TableStat, 0, len(migratedModels)),
		TopSuites: make([]SuiteUsage, 0, topSuiteLimit),
	}

	for _, model := range migratedModels {
		name, err := s.tableName(model.value)
		if err != nil {
			return nil, err
		}

		var rows int64
		if err := s.readDB.WithContext(ctx).
			Model(model.value).
			Count(&rows).Error; err != nil {
			return nil, fmt.Errorf("counting %s: %w", name, err)
		}

		stats.Tables = append(stats.Tables, TableStat{Name: name, Rows: rows})
	}

	if err := s.addRunSpan(ctx, stats); err != nil {
		return nil, err
	}

	if err := s.addTopSuites(ctx, stats); err != nil {
		return nil, err
	}

	if s.cfg.Driver == "sqlite" {
		s.addSQLiteStats(ctx, stats)
	}

	return stats, nil
}

// addRunSpan records how far back the runs table reaches.
func (s *store) addRunSpan(ctx context.Context, stats *DatabaseStats) error {
	var span struct {
		Oldest int64
		Newest int64
	}

	if err := s.readDB.WithContext(ctx).
		Model(&Run{}).
		Select("MIN(timestamp) AS oldest, MAX(timestamp) AS newest").
		Scan(&span).Error; err != nil {
		return fmt.Errorf("reading run span: %w", err)
	}

	stats.OldestRun = span.Oldest
	stats.NewestRun = span.Newest

	return nil
}

// addTopSuites records the suites with the most runs, and what deleting each
// would take with it.
func (s *store) addTopSuites(ctx context.Context, stats *DatabaseStats) error {
	var top []struct {
		SuiteHash string
		Runs      int64
		LastRun   int64
	}

	if err := s.readDB.WithContext(ctx).
		Model(&Run{}).
		Select("suite_hash, COUNT(*) AS runs, MAX(timestamp) AS last_run").
		Where("suite_hash != ''").
		Group("suite_hash").
		Order("runs DESC").
		Limit(topSuiteLimit).
		Scan(&top).Error; err != nil {
		return fmt.Errorf("listing top suites: %w", err)
	}

	if len(top) == 0 {
		return nil
	}

	hashes := make([]string, 0, len(top))
	for _, row := range top {
		hashes = append(hashes, row.SuiteHash)
	}

	testStats, err := s.countBySuite(ctx, &TestStat{}, hashes)
	if err != nil {
		return err
	}

	blockLogs, err := s.countBySuite(ctx, &TestStatsBlockLog{}, hashes)
	if err != nil {
		return err
	}

	names, err := s.suiteLabels(ctx, hashes)
	if err != nil {
		return err
	}

	for _, row := range top {
		usage := SuiteUsage{
			SuiteHash: row.SuiteHash,
			Runs:      row.Runs,
			TestStats: testStats[row.SuiteHash],
			BlockLogs: blockLogs[row.SuiteHash],
			LastRun:   row.LastRun,
		}

		if suite, ok := names[row.SuiteHash]; ok {
			usage.Name = suite.Name
			usage.DiscoveryPath = suite.DiscoveryPath
		}

		stats.TopSuites = append(stats.TopSuites, usage)
	}

	return nil
}

// countBySuite counts a model's rows per suite hash, for the given hashes only.
func (s *store) countBySuite(
	ctx context.Context, model any, hashes []string,
) (map[string]int64, error) {
	var rows []struct {
		SuiteHash string
		Total     int64
	}

	if err := s.readDB.WithContext(ctx).
		Model(model).
		Select("suite_hash, COUNT(*) AS total").
		Where("suite_hash IN ?", hashes).
		Group("suite_hash").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("counting rows by suite: %w", err)
	}

	counts := make(map[string]int64, len(rows))
	for _, row := range rows {
		counts[row.SuiteHash] = row.Total
	}

	return counts, nil
}

// suiteLabels fetches the name and discovery path of the given suites so the
// breakdown reads as something other than a list of hashes.
func (s *store) suiteLabels(
	ctx context.Context, hashes []string,
) (map[string]Suite, error) {
	var suites []Suite
	if err := s.readDB.WithContext(ctx).
		Where("suite_hash IN ?", hashes).
		Find(&suites).Error; err != nil {
		return nil, fmt.Errorf("listing suites: %w", err)
	}

	byHash := make(map[string]Suite, len(suites))
	for _, suite := range suites {
		byHash[suite.SuiteHash] = suite
	}

	return byHash, nil
}

// addSQLiteStats fills in the file and volume numbers. Every field here is
// best-effort: a missing pragma or an unreadable path leaves a zero rather
// than failing the whole report, because the row counts are useful on their
// own.
func (s *store) addSQLiteStats(ctx context.Context, stats *DatabaseStats) {
	path := s.cfg.SQLite.Path
	if path == "" || path == ":memory:" ||
		strings.Contains(path, "mode=memory") {
		return
	}

	stats.Path = path

	for _, pragma := range []struct {
		name string
		into *int64
	}{
		{name: "page_size", into: &stats.PageSize},
		{name: "page_count", into: &stats.PageCount},
		{name: "freelist_count", into: &stats.FreePages},
	} {
		var value int64
		if err := s.readDB.WithContext(ctx).
			Raw("PRAGMA " + pragma.name).
			Scan(&value).Error; err != nil {
			s.log.WithError(err).
				WithField("pragma", pragma.name).
				Debug("Reading SQLite pragma")

			continue
		}

		*pragma.into = value
	}

	if info, err := os.Stat(path); err == nil {
		stats.FileBytes = info.Size()
	}

	if info, err := os.Stat(path + "-wal"); err == nil {
		stats.WALBytes = info.Size()
	}

	total, free, err := volumeUsage(filepath.Dir(path))
	if err != nil {
		s.log.WithError(err).Debug("Reading volume usage")

		return
	}

	stats.VolumeTotalBytes = total
	stats.VolumeFreeBytes = free
}
