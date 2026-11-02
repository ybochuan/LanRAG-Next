package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// OpenReadOnly opens only an existing extension-owned file, without migrations
// or database creation. Every connection enforces mode=ro and query_only=ON.
// The caller owns the returned handle. Normal SQLite WAL read coordination may
// still use sidecars; this is not an immutable/offline snapshot reader.
func OpenReadOnly(ctx context.Context, path string) (*sql.DB, error) {
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file:") {
		return nil, errors.New("existing extension database filesystem path required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return nil, errors.New("existing nonempty extension database required")
	}
	if err = checkOwnership(ctx, abs); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dataSource(abs, true))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
