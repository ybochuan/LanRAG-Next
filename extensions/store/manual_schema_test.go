package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"
)

func TestManualSchemaAdoption(t *testing.T) {
	for _, version := range []int{1, 3, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 22, 23, 24, 25, 26, 27, 28, 29} {
		t.Run(strconv.Itoa(version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "manual.db")
			raw, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			migrations := []string{lifecycleMigration, ingestTagMigration, parserMigration, lifecyclePolicyMigration, reviewAlertsMigration, provenanceMigration, lifecycleConsistencyMigration, lineageSourcesMigration, supplementsMigration, provenanceRunsMigration, businessMigration, qaFeedbackMigration, guidanceBudgetMigration, attachmentSemanticsMigration, attachmentLinksMigration, attachmentCleanupMigration, documentRelationsMigration, attachmentRevisionsMigration, revisionSplitsMigration, repealNoticesMigration, clauseRelationsMigration, parserRenditionsMigration, businessFamiliesMigration, familyReplacementsMigration, familyCutoverMigration, familyMemberHistoryMigration, familySuggestionsMigration, feedbackReviewContextMigration, documentAvailabilityMigration}
			for _, migration := range migrations[:version] {
				if _, err := raw.Exec(migration); err != nil {
					t.Fatal(err)
				}
			}
			if version >= 5 {
				if _, err := raw.Exec(`INSERT INTO ext_lifecycle_rules VALUES('7','keep-me',1,'{}')`); err != nil {
					t.Fatal(err)
				}
			}
			if version >= 22 {
				if _, err := raw.Exec(`
					INSERT INTO ext_lifecycle_events(event_id,tenant_id,knowledge_base_id,document_id,from_state,to_state,reason,occurred_at,stage,attempt,payload_json)
					VALUES('anchor','7','kb','doc','','active','fixture','2026-11-01T00:00:00Z','fixture',0,'{}');
					INSERT INTO ext_lineage_node VALUES('observed','7','kb','document_version','doc','2026-11-01T00:00:00Z');
					INSERT INTO ext_document_version_family VALUES('legacy-family','7','kb','doc','','observed','observed',1,'2026-11-01T00:00:00Z','2026-11-01T00:00:00Z');
					INSERT INTO ext_document_version VALUES('observed','7','kb','legacy-family','doc',printf('%064d',0),1,'anchor','','fixture','2026-11-01T00:00:00Z');
					INSERT INTO ext_revision_proposals(tenant_id,knowledge_base_id,proposal_id,source_document_id,source_version_id,family_id,trigger_guidance_ids,proposed_patch,target_content_hash,status,revision,publication_key,created_at)
					VALUES('7','kb','legacy-proposal','doc','observed','legacy-family','[]','fixture',printf('%064d',0),'pending_review',1,'legacy-key','2026-11-01T00:00:00Z');
				`); err != nil {
					t.Fatal(err)
				}
			}
			if version >= 24 {
				if _, err := raw.Exec(`INSERT INTO ext_business_document_families VALUES('7','kb','prior-family','Existing',2,'now','now');
					INSERT INTO ext_business_document_family_members VALUES('7','kb','doc','prior-family')`); err != nil {
					t.Fatal(err)
				}
			}
			if version >= 26 {
				if _, err := raw.Exec(`INSERT INTO ext_business_family_member_history
					(tenant_id,knowledge_base_id,business_family_id,document_id,status) VALUES('7','kb','prior-family','doc','active')`); err != nil {
					t.Fatal(err)
				}
			}
			raw.Close()
			db, err := OpenDurable(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if version >= 5 {
				if err := db.View(context.Background(), func(tx *sql.Tx) error {
					var revision int
					return tx.QueryRow(`SELECT revision FROM ext_lifecycle_rules WHERE knowledge_base_id='keep-me'`).Scan(&revision)
				}); err != nil {
					t.Fatal("manual facts lost:", err)
				}
			}
			if err := db.View(context.Background(), func(tx *sql.Tx) error {
				var n int
				err := tx.QueryRow("SELECT COUNT(*) FROM ext_schema_migrations").Scan(&n)
				if n != LatestSchemaVersion {
					t.Errorf("migration count=%d", n)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if version >= 22 {
				if err := db.View(t.Context(), func(tx *sql.Tx) error {
					var family, node string
					if err := tx.QueryRow(`SELECT f.family_id,v.version_node_id FROM ext_revision_proposals r
						JOIN ext_document_version_family f USING(tenant_id,knowledge_base_id,family_id)
						JOIN ext_document_version v ON v.version_node_id=r.source_version_id
						WHERE r.proposal_id='legacy-proposal'`).Scan(&family, &node); err != nil || family != "legacy-family" || node != "observed" {
						t.Fatalf("upgrade changed the old version or proposal: %q %q %v", family, node, err)
					}
					var count int
					if err := tx.QueryRow(`SELECT count(*) FROM ext_business_family_legacy_mappings`).Scan(&count); err != nil || count != 0 {
						t.Fatalf("upgrade inferred business families: %d %v", count, err)
					}
					if version >= 24 {
						if err := tx.QueryRow(`SELECT business_family_id FROM ext_business_document_family_members WHERE document_id='doc'`).Scan(&family); err != nil || family != "prior-family" {
							t.Fatalf("existing member did not survive upgrade: %q %v", family, err)
						}
						var status string
						if err := tx.QueryRow(`SELECT status FROM ext_business_family_member_history
							WHERE business_family_id='prior-family' AND document_id='doc'`).Scan(&status); err != nil || status != "active" {
							t.Fatalf("existing membership not recorded: %q %v", status, err)
						}
					}
					rows, err := tx.Query(`PRAGMA foreign_key_check`)
					if err != nil {
						return err
					}
					defer rows.Close()
					if rows.Next() {
						t.Fatal("upgrade broke foreign keys")
					}
					return rows.Err()
				}); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestManualSchemaMismatchDoesNotStampHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manual.db")
	raw, _ := sql.Open("sqlite", path)
	defer raw.Close()
	if _, err := raw.Exec("CREATE TABLE ext_lifecycle_events (unsafe TEXT)"); err != nil {
		t.Fatal(err)
	}
	if db, err := Open(context.Background(), path); err == nil {
		db.Close()
		t.Fatal("accepted incompatible manual schema")
	}
	var count int
	if err := raw.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE name='ext_schema_migrations'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("history changed count=%d err=%v", count, err)
	}
}
