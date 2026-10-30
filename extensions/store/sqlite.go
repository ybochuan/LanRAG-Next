// Package store owns the independent extension SQLite database and its connections.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

// ErrClosed means the extension database is shutting down or already closed.
var ErrClosed = errors.New("extension database is closed")

// DB reserves one writer connection and provides a separate read-only pool.
// Write callbacks must not recursively call Write or finish the supplied transaction.
type DB struct {
	writerDB  *sql.DB
	writer    *sql.Conn
	readers   *sql.DB
	gate      chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
	closeErr  error
}

// Open creates or opens an ext-only file. It refuses foreign user tables before
// enabling WAL or running migrations, so an upstream database cannot be adopted.
func Open(ctx context.Context, path string) (*DB, error) {
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file:") {
		return nil, errors.New("a filesystem path for the independent database is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if !info.Mode().IsRegular() {
			return nil, errors.New("database path is not a regular file")
		}
		if info.Size() > 0 {
			if err := checkOwnership(ctx, abs); err != nil {
				return nil, err
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return nil, err
	}
	writers, err := sql.Open("sqlite", dataSource(abs, false))
	if err != nil {
		return nil, err
	}
	writers.SetMaxOpenConns(1)
	writers.SetMaxIdleConns(1)
	conn, err := writers.Conn(ctx)
	if err != nil {
		writers.Close()
		return nil, err
	}
	db := &DB{writerDB: writers, writer: conn, gate: make(chan struct{}, 1), closed: make(chan struct{})}
	if err := db.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate extension database: %w", err)
	}
	db.readers, err = sql.Open("sqlite", dataSource(abs, true))
	if err != nil {
		db.Close()
		return nil, err
	}
	db.readers.SetMaxOpenConns(4)
	db.readers.SetMaxIdleConns(4)
	if err := db.readers.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func dataSource(path string, reader bool) string {
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath}
	q := url.Values{}
	for _, pragma := range []string{"synchronous(NORMAL)", "busy_timeout(5000)", "foreign_keys(ON)", "cache_size(-64000)", "wal_autocheckpoint(1000)"} {
		q.Add("_pragma", pragma)
	}
	if reader {
		q.Set("mode", "ro")
		q.Add("_pragma", "query_only(ON)")
	} else {
		q.Set("mode", "rwc")
		q.Set("_txlock", "immediate")
		q.Add("_pragma", "journal_mode(WAL)")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func checkOwnership(ctx context.Context, path string) error {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}
	u.RawQuery = "mode=ro"
	probe, err := sql.Open("sqlite", u.String())
	if err != nil {
		return err
	}
	defer probe.Close()
	rows, err := probe.QueryContext(ctx, "SELECT name FROM sqlite_schema WHERE type='table' AND name NOT GLOB 'sqlite_*'")
	if err != nil {
		return fmt.Errorf("inspect existing database: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		if !strings.HasPrefix(name, "ext_") {
			return fmt.Errorf("refuse database containing foreign table %q", name)
		}
	}
	return rows.Err()
}

// Close rejects new work, waits for the current writer, and releases connections.
func (db *DB) Close() error {
	db.closeOnce.Do(func() {
		close(db.closed)
		db.gate <- struct{}{}
		defer func() { <-db.gate }()
		if db.readers != nil {
			db.closeErr = errors.Join(db.closeErr, db.readers.Close())
		}
		connErr := db.writer.Close()
		if errors.Is(connErr, sql.ErrConnDone) {
			connErr = nil
		}
		db.closeErr = errors.Join(db.closeErr, connErr, db.writerDB.Close())
	})
	return db.closeErr
}
