package taggov

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
)

func TestRelationProjectionReconcilesOnlyRelationNamespace(t *testing.T) {
	ctx := t.Context()
	db := tagDatabase(t)
	tags := fixtureStore()
	tags.tags["rel-old"] = Tag{ID: "rel-old", Name: "gov:relation:cites"}
	tags.tags["rel-amends"] = Tag{ID: "rel-amends", Name: "gov:relation:amends"}
	tags.ids = append(tags.ids, "rel-old")
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO ext_lineage_node VALUES('v-source','7','kb-a','document_version','doc-a','now');
			INSERT INTO ext_lineage_node VALUES('v-target','7','kb-a','document_version','doc-b','now');
			INSERT INTO ext_document_relations
			(tenant_id,knowledge_base_id,relation_id,document_id,target_knowledge_base_id,target_document_id,
			 source_version_id,target_version_id,edge_type,status,confidence,revision,edge_id,cross_edge_id)
			VALUES('7','kb-a','rel-1','doc-a','kb-a','doc-b','v-source','v-target','amends','active',1,1,NULL,NULL)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	writer, err := NewWriter(tags, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	p := NewProjector(db, lifecycle.NewStore(db), NewMapping(db, staticCatalog{[]Tag{tags.tags["rel-amends"]}}), writer)
	intent := outboxItem("relation-1", "rel-1", 1)
	if err := p.Handle(ctx, intent); err != nil {
		t.Fatal(err)
	}
	if !equalIDs(tags.ids, []string{"business-a", "business-b", "old-state", "finance", "rel-amends"}) {
		t.Fatalf("relation projection lost or retained tags: %v", tags.ids)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE ext_document_relations SET status='revoked',revision=2 WHERE relation_id='rel-1'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.Handle(ctx, outboxItem("relation-2", "rel-1", 2)); err != nil {
		t.Fatal(err)
	}
	if !equalIDs(tags.ids, []string{"business-a", "business-b", "old-state", "finance"}) {
		t.Fatalf("revoked relation tag remains: %v", tags.ids)
	}
}

func TestRelationTypesIncludesCrossKBIncomingDocument(t *testing.T) {
	ctx := t.Context()
	db := tagDatabase(t)
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO ext_lineage_node VALUES('v-source','7','kb-a','document_version','doc-a','now');
			INSERT INTO ext_lineage_node VALUES('v-target','7','kb-b','document_version','doc-b','now');
			INSERT INTO ext_document_relations
			(tenant_id,knowledge_base_id,relation_id,document_id,target_knowledge_base_id,target_document_id,
			 source_version_id,target_version_id,edge_type,status,confidence,revision,edge_id,cross_edge_id)
			VALUES('7','kb-a','rel-cross','doc-a','kb-b','doc-b','v-source','v-target','interprets','active',1,1,NULL,NULL)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	writer, err := NewWriter(fixtureStore(), 4)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	p := NewProjector(db, lifecycle.NewStore(db), NewMapping(db, staticCatalog{}), writer)
	types, err := p.relationTypes(ctx, Scope{TenantID: 7, KnowledgeBaseID: "kb-b", DocumentID: "doc-b"})
	if err != nil || !equalStrings(types, []string{"interprets"}) {
		t.Fatalf("incoming cross-KB relation types=%v err=%v", types, err)
	}
}

func outboxItem(id, relationID string, revision int64) outbox.Item {
	return outbox.Item{
		ID: id, TenantID: 7, KnowledgeBaseID: "kb-a", ProjectionType: RelationProjectionType,
		AggregateType: "document", AggregateID: "doc-a", CausationType: RelationCausation,
		CausationID: fmt.Sprintf("%s:%d", relationID, revision),
		Payload:     json.RawMessage(`{}`), CreatedAt: time.Now().UTC(),
	}
}
