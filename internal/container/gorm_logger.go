package container

import (
	"context"
	"io"
	stdlog "log"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/metrics"
	gormlogger "gorm.io/gorm/logger"
)

// gormSlowSQLThreshold is GORM's default slow-query threshold. Statements
// faster than this are not logged at Warn.
const gormSlowSQLThreshold = metrics.SQLiteSlowThreshold

// newGormLogger is the application database logger. It matches GORM's
// default slow-SQL threshold (200ms), Warn level, and record-not-found
// handling. ParameterizedQueries is set so slow-query logs keep the SQL
// text and timing but omit bound values such as chunk text and vectors.
func newGormLogger(w io.Writer) gormlogger.Interface {
	base := gormlogger.New(stdlog.New(w, "\r\n", stdlog.LstdFlags), gormlogger.Config{
		SlowThreshold:             gormSlowSQLThreshold,
		LogLevel:                  gormlogger.Warn,
		IgnoreRecordNotFoundError: false,
		Colorful:                  true,
		ParameterizedQueries:      true,
	})
	return &metricsGormLogger{Interface: base}
}

// metricsGormLogger records only fixed SQLite counters; SQL text and bound
// values are never passed to the metrics registry.
type metricsGormLogger struct {
	gormlogger.Interface
}

func (l *metricsGormLogger) Trace(
	ctx context.Context,
	begin time.Time,
	fc func() (sql string, rowsAffected int64),
	err error,
) {
	elapsed := time.Since(begin)
	if err == nil && elapsed < gormSlowSQLThreshold {
		l.Interface.Trace(ctx, begin, fc, err)
		return
	}

	sqlText, rowsAffected := fc()
	metrics.Default.ObserveSQLite(sqliteOperation(sqlText), elapsed, err)
	l.Interface.Trace(ctx, begin, func() (string, int64) {
		return sqlText, rowsAffected
	}, err)
}

func sqliteOperation(sqlText string) string {
	fields := strings.Fields(sqlText)
	if len(fields) == 0 {
		return "other"
	}
	switch strings.ToUpper(fields[0]) {
	case "SELECT":
		return "select"
	case "INSERT":
		return "insert"
	case "UPDATE":
		return "update"
	case "DELETE":
		return "delete"
	default:
		return "other"
	}
}
