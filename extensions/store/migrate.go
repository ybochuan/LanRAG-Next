package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// LatestSchemaVersion is the last published migration supported by this Host.
const LatestSchemaVersion = 41

type schemaMigration struct {
	Version  int
	File     string
	SQL      string
	Checksum string
}

// schemaMigrations is a deterministic, contiguous catalog of every published
// migration. The checksum covers the SQL with CRLF normalized to LF so a
// checkout on Windows and Linux verifies the same bytes.
var schemaMigrations = loadSchemaMigrations()

func loadSchemaMigrations() []schemaMigration {
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		panic("extension migrations: " + err.Error())
	}
	sort.Strings(names)
	if len(names) != LatestSchemaVersion {
		panic(fmt.Sprintf("extension migrations: found %d files, expected %d", len(names), LatestSchemaVersion))
	}
	result := make([]schemaMigration, 0, len(names))
	for i, name := range names {
		base := path.Base(name)
		parts := strings.SplitN(base, "_", 2)
		if len(parts) != 2 {
			panic("extension migration has no numeric prefix: " + base)
		}
		version, err := strconv.Atoi(parts[0])
		if err != nil || version != i+1 {
			panic(fmt.Sprintf("extension migration %q is not the contiguous version %d", base, i+1))
		}
		body, err := migrationFiles.ReadFile(name)
		if err != nil {
			panic("extension migration read " + name + ": " + err.Error())
		}
		normalized := strings.ReplaceAll(string(body), "\r\n", "\n")
		sum := sha256.Sum256([]byte(normalized))
		result = append(result, schemaMigration{
			Version:  version,
			File:     base,
			SQL:      normalized,
			Checksum: fmt.Sprintf("%x", sum),
		})
	}
	return result
}

func migrationSQL(version int) string {
	if version < 1 || version > len(schemaMigrations) {
		panic(fmt.Sprintf("extension migration %d is outside the published catalog", version))
	}
	return schemaMigrations[version-1].SQL
}

func migrationChecksum(version int) string {
	if version < 1 || version > len(schemaMigrations) {
		panic(fmt.Sprintf("extension migration %d is outside the published catalog", version))
	}
	return schemaMigrations[version-1].Checksum
}

func (db *DB) migrate(ctx context.Context) error {
	return db.Write(ctx, func(tx *sql.Tx) error {
		if err := adoptManualSchema(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS ext_schema_migrations(version INTEGER PRIMARY KEY, checksum TEXT NOT NULL DEFAULT '')"); err != nil {
			return err
		}
		if err := ensureMigrationChecksumColumn(ctx, tx); err != nil {
			return err
		}
		applied, err := appliedMigrationChecksums(ctx, tx)
		if err != nil {
			return err
		}
		version := 0
		for v := 1; v <= len(applied); v++ {
			checksum, ok := applied[v]
			if !ok {
				return fmt.Errorf("extension schema migration history has a gap at version %d", v)
			}
			if v > LatestSchemaVersion {
				return fmt.Errorf("unsupported extension schema version %d", v)
			}
			expected := migrationChecksum(v)
			switch checksum {
			case "":
				// Databases created before checksums existed are backfilled once
				// from the exact published SQL rather than rejected.
				if _, err := tx.ExecContext(ctx, "UPDATE ext_schema_migrations SET checksum=? WHERE version=?", expected, v); err != nil {
					return err
				}
			case expected:
			default:
				return fmt.Errorf("extension schema migration %d checksum mismatch", v)
			}
			version = v
		}
		for v := version + 1; v <= len(schemaMigrations); v++ {
			migration := schemaMigrations[v-1]
			if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
				return fmt.Errorf("apply extension schema migration %d (%s): %w", v, migration.File, err)
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations(version,checksum) VALUES(?,?)", v, migration.Checksum); err != nil {
				return err
			}
		}
		// The consistency guard is code, not data. Replaying it on every writable
		// open lets a database already stamped schema 7 pick up a revised trigger
		// body instead of silently keeping the older one. The statements are
		// idempotent because every CREATE is preceded by DROP IF EXISTS, which is
		// also why this revision did not take a new version number.
		if _, err := tx.ExecContext(ctx, schemaMigrations[6].SQL); err != nil {
			return err
		}
		return nil
	})
}

func ensureMigrationChecksumColumn(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "PRAGMA table_info(ext_schema_migrations)")
	if err != nil {
		return err
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull, primary int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primary); err != nil {
			return err
		}
		if name == "checksum" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = tx.ExecContext(ctx, "ALTER TABLE ext_schema_migrations ADD COLUMN checksum TEXT NOT NULL DEFAULT ''")
	return err
}

func appliedMigrationChecksums(ctx context.Context, tx *sql.Tx) (map[int]string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT version, checksum FROM ext_schema_migrations ORDER BY version")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[int]string)
	for rows.Next() {
		var version int
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			return nil, err
		}
		if version < 1 {
			return nil, fmt.Errorf("invalid extension schema version %d", version)
		}
		if _, exists := result[version]; exists {
			return nil, fmt.Errorf("duplicate extension schema version %d", version)
		}
		result[version] = checksum
	}
	return result, rows.Err()
}
