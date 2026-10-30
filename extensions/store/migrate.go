package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
)

//go:embed migrations/001_lifecycle.sql
var lifecycleMigration string

//go:embed migrations/002_ingest_taggov.sql
var ingestTagMigration string

func (db *DB) migrate(ctx context.Context) error {
	return db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS ext_schema_migrations(version INTEGER PRIMARY KEY)"); err != nil {
			return err
		}
		var version int
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM ext_schema_migrations").Scan(&version); err != nil {
			return err
		}
		if version > 2 {
			return fmt.Errorf("unsupported extension schema version %d", version)
		}
		if version < 1 {
			if _, err := tx.ExecContext(ctx, lifecycleMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(1)"); err != nil {
				return err
			}
		}
		if version < 2 {
			if _, err := tx.ExecContext(ctx, ingestTagMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(2)"); err != nil {
				return err
			}
		}
		return nil
	})
}
