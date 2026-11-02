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

//go:embed migrations/003_parser.sql
var parserMigration string

//go:embed migrations/004_lifecycle_policy.sql
var lifecyclePolicyMigration string

//go:embed migrations/005_review_alerts.sql
var reviewAlertsMigration string

//go:embed migrations/006_copy_provenance.sql
var provenanceMigration string

//go:embed migrations/008_lineage_sources.sql
var lineageSourcesMigration string

//go:embed migrations/009_supplements.sql
var supplementsMigration string

//go:embed migrations/010_provenance_runs.sql
var provenanceRunsMigration string

//go:embed migrations/011_business_workflows.sql
var businessMigration string

//go:embed migrations/007_lifecycle_current_consistency.sql
var lifecycleConsistencyMigration string

func (db *DB) migrate(ctx context.Context) error {
	return db.Write(ctx, func(tx *sql.Tx) error {
		if err := adoptManualSchema(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS ext_schema_migrations(version INTEGER PRIMARY KEY)"); err != nil {
			return err
		}
		var version int
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM ext_schema_migrations").Scan(&version); err != nil {
			return err
		}
		if version > 11 {
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
		if version < 3 {
			if _, err := tx.ExecContext(ctx, parserMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(3)"); err != nil {
				return err
			}
		}
		if version < 4 {
			if _, err := tx.ExecContext(ctx, lifecyclePolicyMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(4)"); err != nil {
				return err
			}
		}
		if version < 5 {
			if _, err := tx.ExecContext(ctx, reviewAlertsMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(5)"); err != nil {
				return err
			}
		}
		if version < 6 {
			if _, err := tx.ExecContext(ctx, provenanceMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(6)"); err != nil {
				return err
			}
		}
		if version < 7 {
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(7)"); err != nil {
				return err
			}
		}
		// The consistency guard is code, not data. Replaying it on every writable
		// open lets a database already stamped schema 7 pick up a revised trigger
		// body instead of silently keeping the older one. The statements are
		// idempotent because every CREATE is preceded by DROP IF EXISTS, which is
		// also why this revision did not take a new version number.
		if _, err := tx.ExecContext(ctx, lifecycleConsistencyMigration); err != nil {
			return err
		}
		if version < 8 {
			if _, err := tx.ExecContext(ctx, lineageSourcesMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(8)"); err != nil {
				return err
			}
		}
		if version < 9 {
			if _, err := tx.ExecContext(ctx, supplementsMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(9)"); err != nil {
				return err
			}
		}

		if version < 10 {
			if _, err := tx.ExecContext(ctx, provenanceRunsMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(10)"); err != nil {
				return err
			}
		}
		if version < 11 {
			if _, err := tx.ExecContext(ctx, businessMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(11)"); err != nil {
				return err
			}
		}
		return nil
	})
}
