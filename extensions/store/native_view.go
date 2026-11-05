package store

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"strings"

	"modernc.org/sqlite"
)

// SQLiteBusy includes extended result codes such as SQLITE_BUSY_SNAPSHOT.
func SQLiteBusy(err error) bool {
	var sqliteErr *sqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code()&0xff == 5
}

// ViewNative opens a separate read transaction with both files physically read-only.
// The ATTACH belongs to this request and never enters the shared connection pool.
func (db *DB) ViewNative(ctx context.Context, nativePath string, read func(*sql.Tx) error) error {
	if nativePath == "" || nativePath == ":memory:" || strings.HasPrefix(nativePath, "file:") {
		return errors.New("native SQLite filesystem path required")
	}
	select {
	case <-db.closed:
		return ErrClosed
	default:
	}
	native, err := filepath.Abs(nativePath)
	if err != nil {
		return err
	}
	readOnlyURI := func(path string) string {
		path = filepath.ToSlash(path)
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}
		return u.String()
	}
	reader, err := sql.Open("sqlite", readOnlyURI(db.path))
	if err != nil {
		return err
	}
	defer reader.Close()
	conn, err := reader.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `ATTACH DATABASE ? AS weknora`, readOnlyURI(native)); err != nil {
		return err
	}
	if _, err = conn.ExecContext(ctx, `PRAGMA query_only=ON`); err != nil {
		return err
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := read(tx); err != nil {
		return err
	}
	return tx.Commit()
}
