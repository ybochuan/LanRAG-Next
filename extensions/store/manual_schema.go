package store

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
)

// ErrManualSchema means existing DDL cannot be proven to be an exact, complete
// prefix of our migrations. Never reset, skip statements, or stamp this schema.
var ErrManualSchema = errors.New("untracked extension schema differs from published migrations; database preserved")

type schemaReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func schemaSnapshot(ctx context.Context, q schemaReader) (map[string]string, error) {
	rows, err := q.QueryContext(ctx, "SELECT type,name,tbl_name,COALESCE(sql,'') FROM sqlite_schema WHERE name NOT GLOB 'sqlite_*' AND tbl_name <> 'ext_schema_migrations' ORDER BY type,name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]string{}
	for rows.Next() {
		var kind, name, table, ddl string
		if err := rows.Scan(&kind, &name, &table, &ddl); err != nil {
			return nil, err
		}
		// Preserve quoted whitespace and comment boundaries; only line-ending
		// differences and outer whitespace are irrelevant to an exact snapshot.
		result[kind+":"+name] = table + ":" + strings.TrimSpace(strings.ReplaceAll(ddl, "\r\n", "\n"))
	}
	return result, rows.Err()
}

// adoptManualSchema runs inside the same write transaction as normal migration.
// Only an exact known DDL prefix with valid foreign keys may gain history rows.
func adoptManualSchema(ctx context.Context, tx *sql.Tx) error {
	var history int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE name='ext_schema_migrations'").Scan(&history); err != nil {
		return err
	}
	if history != 0 {
		return nil
	}
	actual, err := schemaSnapshot(ctx, tx)
	if err != nil || len(actual) == 0 {
		return err
	}
	reference, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return err
	}
	defer reference.Close()
	reference.SetMaxOpenConns(1)
	migrations := []string{lifecycleMigration, ingestTagMigration, parserMigration, lifecyclePolicyMigration, reviewAlertsMigration, provenanceMigration, lifecycleConsistencyMigration, lineageSourcesMigration, supplementsMigration, provenanceRunsMigration, businessMigration}
	for i, migration := range migrations {
		if _, err := reference.ExecContext(ctx, migration); err != nil {
			return err
		}
		expected, err := schemaSnapshot(ctx, reference)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(actual, expected) {
			continue
		}
		rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
		if err != nil {
			return err
		}
		invalid := rows.Next()
		rowErr := rows.Err()
		rows.Close()
		if invalid {
			return ErrManualSchema
		}
		if rowErr != nil {
			return rowErr
		}
		if _, err := tx.ExecContext(ctx, "CREATE TABLE ext_schema_migrations(version INTEGER PRIMARY KEY)"); err != nil {
			return err
		}
		for n := 1; n <= i+1; n++ {
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(?)", n); err != nil {
				return err
			}
		}
		return nil
	}
	return ErrManualSchema
}
