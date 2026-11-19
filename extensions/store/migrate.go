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

//go:embed migrations/023_business_families.sql
var businessFamiliesMigration string

//go:embed migrations/024_family_replacements.sql
var familyReplacementsMigration string

//go:embed migrations/025_family_cutover.sql
var familyCutoverMigration string

//go:embed migrations/026_family_member_history.sql
var familyMemberHistoryMigration string

//go:embed migrations/027_family_suggestions.sql
var familySuggestionsMigration string

//go:embed migrations/028_feedback_review_context.sql
var feedbackReviewContextMigration string

//go:embed migrations/029_document_availability.sql
var documentAvailabilityMigration string

//go:embed migrations/030_cross_kb_relations.sql
var crossKBRelationsMigration string

//go:embed migrations/031_cross_kb_supplements.sql
var crossKBRelationsSupplementMigration string

//go:embed migrations/032_feedback_acceptance.sql
var feedbackAcceptanceMigration string

//go:embed migrations/033_review_acknowledgements.sql
var reviewAcknowledgementsMigration string

//go:embed migrations/034_cleanup_tasks.sql
var cleanupTasksMigration string

//go:embed migrations/035_evidence_retention.sql
var evidenceRetentionMigration string

//go:embed migrations/036_attachment_workbench.sql
var attachmentWorkbenchMigration string

//go:embed migrations/037_attachment_template_reference.sql
var attachmentTemplateReferenceMigration string

// LatestSchemaVersion is the last published migration supported by this Host.
const LatestSchemaVersion = 37

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
		if version < 23 {
			if _, err := tx.ExecContext(ctx, businessFamiliesMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(23)"); err != nil {
				return err
			}
		}
		if version < 24 {
			if _, err := tx.ExecContext(ctx, familyReplacementsMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(24)"); err != nil {
				return err
			}
		}
		if version < 25 {
			if _, err := tx.ExecContext(ctx, familyCutoverMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(25)"); err != nil {
				return err
			}
		}
		if version < 26 {
			if _, err := tx.ExecContext(ctx, familyMemberHistoryMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(26)"); err != nil {
				return err
			}
		}
		if version < 27 {
			if _, err := tx.ExecContext(ctx, familySuggestionsMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(27)"); err != nil {
				return err
			}
		}
		if version < 28 {
			if _, err := tx.ExecContext(ctx, feedbackReviewContextMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(28)"); err != nil {
				return err
			}
		}
		if version < 29 {
			if _, err := tx.ExecContext(ctx, documentAvailabilityMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(29)"); err != nil {
				return err
			}
		}
		if version < 30 {
			if _, err := tx.ExecContext(ctx, crossKBRelationsMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(30)"); err != nil {
				return err
			}
		}
		if version < 31 {
			if _, err := tx.ExecContext(ctx, crossKBRelationsSupplementMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(31)"); err != nil {
				return err
			}
		}
		if version < 32 {
			if _, err := tx.ExecContext(ctx, feedbackAcceptanceMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(32)"); err != nil {
				return err
			}
		}
		if version < 33 {
			if _, err := tx.ExecContext(ctx, reviewAcknowledgementsMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(33)"); err != nil {
				return err
			}
		}
		if version < 34 {
			if _, err := tx.ExecContext(ctx, cleanupTasksMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(34)"); err != nil {
				return err
			}
		}
		if version < 35 {
			if _, err := tx.ExecContext(ctx, evidenceRetentionMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(35)"); err != nil {
				return err
			}
		}
		if version < 36 {
			if _, err := tx.ExecContext(ctx, attachmentWorkbenchMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(36)"); err != nil {
				return err
			}
		}
		if version < 37 {
			if _, err := tx.ExecContext(ctx, attachmentTemplateReferenceMigration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO ext_schema_migrations VALUES(37)"); err != nil {
				return err
			}
		}
		return nil
	})
}
