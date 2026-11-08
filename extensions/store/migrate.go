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

//go:embed migrations/012_qa_feedback.sql
var qaFeedbackMigration string

//go:embed migrations/013_guidance_budget.sql
var guidanceBudgetMigration string

//go:embed migrations/014_attachment_semantics.sql
var attachmentSemanticsMigration string

//go:embed migrations/015_attachment_links.sql
var attachmentLinksMigration string

//go:embed migrations/016_attachment_cleanup.sql
var attachmentCleanupMigration string

//go:embed migrations/017_document_relations.sql
var documentRelationsMigration string

//go:embed migrations/018_attachment_revisions.sql
var attachmentRevisionsMigration string

//go:embed migrations/019_revision_splits.sql
var revisionSplitsMigration string

//go:embed migrations/020_repeal_notices.sql
var repealNoticesMigration string

//go:embed migrations/021_clause_relations.sql
var clauseRelationsMigration string

//go:embed migrations/022_parser_renditions.sql
var parserRenditionsMigration string

// LatestSchemaVersion is the last published migration supported by this Host.
const LatestSchemaVersion = 22

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
		if version > LatestSchemaVersion {
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
		if version < 12 {
			if _, err := tx.ExecContext(ctx, qaFeedbackMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(12)"); err != nil {
				return err
			}
		}
		if version < 13 {
			if _, err := tx.ExecContext(ctx, guidanceBudgetMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(13)"); err != nil {
				return err
			}
		}
		if version < 14 {
			if _, err := tx.ExecContext(ctx, attachmentSemanticsMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(14)"); err != nil {
				return err
			}
		}
		if version < 15 {
			if _, err := tx.ExecContext(ctx, attachmentLinksMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(15)"); err != nil {
				return err
			}
		}
		if version < 16 {
			if _, err := tx.ExecContext(ctx, attachmentCleanupMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(16)"); err != nil {
				return err
			}
		}
		if version < 17 {
			if _, err := tx.ExecContext(ctx, documentRelationsMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(17)"); err != nil {
				return err
			}
		}
		if version < 18 {
			if _, err := tx.ExecContext(ctx, attachmentRevisionsMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(18)"); err != nil {
				return err
			}
		}
		if version < 19 {
			if _, err := tx.ExecContext(ctx, revisionSplitsMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(19)"); err != nil {
				return err
			}
		}
		if version < 20 {
			if _, err := tx.ExecContext(ctx, repealNoticesMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(20)"); err != nil {
				return err
			}
		}
		if version < 21 {
			if _, err := tx.ExecContext(ctx, clauseRelationsMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(21)"); err != nil {
				return err
			}
		}
		if version < 22 {
			if _, err := tx.ExecContext(ctx, parserRenditionsMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(22)"); err != nil {
				return err
			}
		}
		return nil
	})
}
