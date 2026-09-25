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

	// Volume describes the filesystem holding the database. This is what
	// decides whether a cleanup is urgent.
	//
	// Used and Free do not add up to Total: a filesystem reserves a slice for
	// root that an ordinary process cannot touch. A usage share is
	// Used/(Used+Free), which is what df prints; Total-Free would count the
	// reserve as occupied and read ~5% full on an empty ext4 volume.
	VolumeTotalBytes int64 `json:"volume_total_bytes,omitempty"`
	VolumeUsedBytes  int64 `json:"volume_used_bytes,omitempty"`
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

	// Estimated marks a count read from the primary-key span instead of a
	// COUNT(*). See estimatedRows.
	Estimated bool `json:"estimated,omitempty"`
}

// SuiteUsage is how much of the database one suite accounts for. Deleting a
// suite's runs cascades to its test stats and block logs, so this is roughly
// what a cleanup frees.
type SuiteUsage struct {
	SuiteHash     string `json:"suite_hash"`
	Name          string `json:"name,omitempty"`
	DiscoveryPath string `json:"discovery_path,omitempty"`
	Runs          int64  `json:"runs"`

	// Tests is the sum of tests_total over the suite's runs. It stands in for
	// the suite's test_stats rows, which are too many to count per request.
	Tests int64 `json:"tests"`

	// LastRun is unix seconds of the newest run, 0 when unknown.
	LastRun int64 `json:"last_run,omitempty"`
}

// DatabaseStats gathers the index database's size and contents.
//
// Every query here must stay cheap however large the database grows, because
// the report is built per request. test_stats and test_stats_block_logs hold
// tens of millions of rows in production, where one COUNT(*) ran for more
// than half an hour. So the report never scans them: their row counts are
// estimates, and the per-suite breakdown reads only the runs table.
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

		table := TableStat{Name: name, Estimated: isPerTestModel(model.value)}

		if table.Estimated {
			rows, err := s.estimatedRows(ctx, model.value)
			if err != nil {
				return nil, fmt.Errorf("estimating %s: %w", name, err)
			}

			table.Rows = rows
		} else if err := s.readDB.WithContext(ctx).
			Model(model.value).
			Count(&table.Rows).Error; err != nil {
			return nil, fmt.Errorf("counting %s: %w", name, err)
		}

		stats.Tables = append(stats.Tables, table)
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

// isPerTestModel reports whether a model holds a row per test per run. These
// tables grow by thousands of rows with every run, so the report must not
// count or group them.
func isPerTestModel(model any) bool {
	switch model.(type) {
	case *TestStat, *TestStatsBlockLog:
		return true
	default:
		return false
	}
}

// estimatedRows reads a table's size from its primary-key span, MAX(id) -
// MIN(id) + 1. Each end is one index lookup, so the cost does not grow with
// the table. IDs only grow and deletes leave gaps, so the figure is an upper
// bound: a purge in the middle of the ID range is not reflected in it.
func (s *store) estimatedRows(ctx context.Context, model any) (int64, error) {
	lowest, err := s.columnExtreme(ctx, model, "MIN", "id")
	if err != nil {
		return 0, err
	}

	highest, err := s.columnExtreme(ctx, model, "MAX", "id")
	if err != nil {
		return 0, err
	}

	if highest == 0 {
		return 0, nil
	}

	return highest - lowest + 1, nil
}

// columnExtreme reads MIN or MAX of one indexed column, 0 on an empty table.
//
// Each extreme is a query of its own on purpose. SQLite answers a lone MIN()
// or MAX() of an indexed column from one end of the index, but a query that
// asks for both falls back to a scan of the whole index.
func (s *store) columnExtreme(
	ctx context.Context, model any, fn, column string,
) (int64, error) {
	var value int64
	if err := s.readDB.WithContext(ctx).
		Model(model).
		Select("COALESCE(" + fn + "(" + column + "), 0)").
		Scan(&value).Error; err != nil {
		return 0, fmt.Errorf("reading %s(%s): %w", fn, column, err)
	}

	return value, nil
}

// addRunSpan records how far back the runs table reaches.
func (s *store) addRunSpan(ctx context.Context, stats *DatabaseStats) error {
	oldest, err := s.columnExtreme(ctx, &Run{}, "MIN", "timestamp")
	if err != nil {
		return fmt.Errorf("reading run span: %w", err)
	}

	newest, err := s.columnExtreme(ctx, &Run{}, "MAX", "timestamp")
	if err != nil {
		return fmt.Errorf("reading run span: %w", err)
	}

	stats.OldestRun = oldest
	stats.NewestRun = newest

	return nil
}

// addTopSuites records the suites with the most runs, and roughly what
// deleting each would take with it. It reads only the runs table: a run
// carries its own test count, so the suite's test_stats rows need no scan.
func (s *store) addTopSuites(ctx context.Context, stats *DatabaseStats) error {
	var top []struct {
		SuiteHash string
		Runs      int64
		Tests     int64
		LastRun   int64
	}

	if err := s.readDB.WithContext(ctx).
		Model(&Run{}).
		Select("suite_hash, COUNT(*) AS runs, " +
			"COALESCE(SUM(tests_total), 0) AS tests, " +
			"MAX(timestamp) AS last_run").
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

	names, err := s.suiteLabels(ctx, hashes)
	if err != nil {
		return err
	}

	for _, row := range top {
		usage := SuiteUsage{
			SuiteHash: row.SuiteHash,
			Runs:      row.Runs,
			Tests:     row.Tests,
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
	file := sqliteFilePath(path)

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

	if info, err := os.Stat(file); err == nil {
		stats.FileBytes = info.Size()
	}

	if info, err := os.Stat(file + "-wal"); err == nil {
		stats.WALBytes = info.Size()
	}

	volume, err := volumeUsage(filepath.Dir(file))
	if err != nil {
		s.log.WithError(err).Debug("Reading volume usage")

		return
	}

	stats.VolumeTotalBytes = volume.Total
	stats.VolumeUsedBytes = volume.Used
	stats.VolumeFreeBytes = volume.Free
}

// sqliteFilePath reduces a configured SQLite path to the file on disk. The
// path is handed straight to the driver, which accepts a DSN as well as a
// plain path, so "file:/data/index.db?_pragma=busy_timeout(5000)" is valid
// config. Stat'ing that verbatim would silently report no size and no volume
// at all, which is exactly when the report matters most.
func sqliteFilePath(path string) string {
	file := strings.TrimPrefix(path, "file:")

	if idx := strings.IndexByte(file, '?'); idx >= 0 {
		file = file[:idx]
	}

	return file
}
