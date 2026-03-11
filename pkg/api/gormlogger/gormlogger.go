package gormlogger

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const maxSQLLength = 500

// Logger adapts logrus to GORM's logger.Interface, surfacing slow queries
// and errors while keeping normal queries silent.
type Logger struct {
	log           logrus.FieldLogger
	slowThreshold time.Duration
}

// Compile-time interface check.
var _ gormlogger.Interface = (*Logger)(nil)

// New creates a Logger that warns on queries slower than slowThreshold.
func New(log logrus.FieldLogger, slowThreshold time.Duration) *Logger {
	return &Logger{
		log:           log,
		slowThreshold: slowThreshold,
	}
}

// LogMode is a no-op; log level is controlled by logrus configuration.
func (l *Logger) LogMode(_ gormlogger.LogLevel) gormlogger.Interface {
	return l
}

// Info is a no-op; informational GORM messages are not surfaced.
func (l *Logger) Info(_ context.Context, _ string, _ ...any) {}

// Warn forwards GORM warnings to logrus.
func (l *Logger) Warn(_ context.Context, msg string, args ...any) {
	l.log.Warnf(msg, args...)
}

// Error forwards GORM errors to logrus.
func (l *Logger) Error(_ context.Context, msg string, args ...any) {
	l.log.Errorf(msg, args...)
}

// Trace is called by GORM after every query. It logs errors and slow
// queries while keeping normal operations silent.
func (l *Logger) Trace(
	_ context.Context,
	begin time.Time,
	fc func() (sql string, rows int64),
	err error,
) {
	duration := time.Since(begin)
	sql, rows := fc()
	sql = truncateSQL(sql)

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return
		}

		if isContextCanceled(err) {
			l.log.WithField("duration", duration).
				WithField("canceled", true).
				WithField("sql", sql).
				Warn("Query canceled")

			return
		}

		l.log.WithError(err).
			WithField("duration", duration).
			WithField("rows", rows).
			WithField("sql", sql).
			Error("Query error")

		return
	}

	if l.slowThreshold > 0 && duration >= l.slowThreshold {
		l.log.WithField("duration", duration).
			WithField("rows", rows).
			WithField("sql", sql).
			Warn(fmt.Sprintf("Slow query (>= %s)", l.slowThreshold))
	}
}

// isContextCanceled checks whether err is caused by context cancellation
// or deadline exceeded. It uses errors.Is first, then falls back to
// string matching for SQLite driver compatibility.
func isContextCanceled(err error) bool {
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	msg := err.Error()

	return strings.Contains(msg, "context canceled") ||
		strings.Contains(msg, "context deadline exceeded")
}

// truncateSQL shortens a SQL string to maxSQLLength to prevent log bloat.
func truncateSQL(sql string) string {
	if len(sql) > maxSQLLength {
		return sql[:maxSQLLength] + "..."
	}

	return sql
}
