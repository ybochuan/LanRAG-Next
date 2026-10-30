package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
)

// Write serializes the entire BEGIN IMMEDIATE / callback / COMMIT transaction.
// Errors, context cancellation and callback panics all release the writer slot.
func (db *DB) Write(ctx context.Context, fn func(*sql.Tx) error) error {
	if fn == nil {
		return errors.New("nil write callback")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-db.closed:
		return ErrClosed
	case db.gate <- struct{}{}:
	}
	defer func() { <-db.gate }()
	select {
	case <-db.closed:
		return ErrClosed
	default:
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := db.writer.BeginTx(ctx, nil)
	// database/sql may discard a reserved connection after automatic rollback on
	// cancellation. Re-reserve before the next transaction; never replay a callback.
	if errors.Is(err, sql.ErrConnDone) || errors.Is(err, driver.ErrBadConn) {
		_ = db.writer.Close()
		conn, reserveErr := db.writerDB.Conn(ctx)
		if reserveErr != nil {
			return reserveErr
		}
		db.writer = conn
		tx, err = db.writer.BeginTx(ctx, nil)
	}
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return tx.Commit()
}

// View runs a consistent read transaction through a physically read-only connection.
func (db *DB) View(ctx context.Context, fn func(*sql.Tx) error) error {
	if fn == nil {
		return errors.New("nil view callback")
	}
	select {
	case <-db.closed:
		return ErrClosed
	default:
	}
	tx, err := db.readers.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return tx.Commit()
}
