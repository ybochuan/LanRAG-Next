package lineage

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func TestGraphPreservesDiamondEdgesAndScopeAndCycles(t *testing.T) {
	r, db, _, _ := registryFixture(t)
	ctx := context.Background()
	err := db.Write(ctx, func(tx *sql.Tx) error {
		for _, id := range []string{"a", "b", "c", "d"} {
			if _, err := tx.Exec("INSERT INTO ext_lineage_node VALUES(?,'7','kb','document_version',?,'2026-11-05T00:00:00Z')", id, id); err != nil {
				return err
			}
		}
		if _, err := tx.Exec("INSERT INTO ext_lineage_node VALUES('foreign','8','kb','document_version','private','2026-11-05T00:00:00Z')"); err != nil {
			return err
		}
		for i, pair := range [][2]string{{"a", "b"}, {"a", "c"}, {"b", "d"}, {"c", "d"}, {"d", "a"}} {
			if _, err := tx.Exec("INSERT INTO ext_lineage_edge VALUES(?,'7','kb',?,?,'supersedes','governance_critical','2026-11-05T00:00:00Z')", fmt.Sprint(i), pair[0], pair[1]); err != nil {
				return err
			}
		}
		_, err := tx.Exec("INSERT INTO ext_lineage_edge VALUES('derived','7','kb','a','d','derived_from','reconstructable','2026-11-05T00:00:00Z')")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	graph, err := r.Graph(ctx, fixtureScope, "version-ancestry", "a")
	if err != nil || len(graph.Nodes) != 4 || len(graph.Edges) != 5 || graph.Truncated {
		t.Fatalf("diamond/cycle: %+v %v", graph, err)
	}
	for _, node := range graph.Nodes {
		if node.ID == "foreign" {
			t.Fatal("foreign node leaked")
		}
	}
	graph, err = r.Graph(ctx, fixtureScope, "provenance", "a")
	if err != nil || len(graph.Edges) != 1 || graph.Edges[0].From != "a" || graph.Edges[0].To != "d" {
		t.Fatalf("derived direction or edge types: %+v %v", graph, err)
	}
	if _, err = r.Graph(ctx, provenance.Scope{TenantID: 8, KnowledgeBaseID: "kb"}, "provenance", "a"); err == nil {
		t.Fatal("foreign root admitted")
	}
	if _, err = r.Graph(ctx, fixtureScope, "unsupported", ""); err == nil {
		t.Fatal("unknown graph mode")
	}
}

func TestGraphReportsBoundedNodeWindow(t *testing.T) {
	r, db, _, _ := registryFixture(t)
	err := db.Write(t.Context(), func(tx *sql.Tx) error {
		for i := 0; i < 260; i++ {
			if _, err := tx.Exec("INSERT INTO ext_lineage_node VALUES(?,'7','kb','chunk',?,'2026-11-05T00:00:00Z')", fmt.Sprint(i), fmt.Sprint(i)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	graph, err := r.Graph(t.Context(), fixtureScope, "provenance", "")
	if err != nil || len(graph.Nodes) != 256 || !graph.Truncated {
		t.Fatalf("node limit not reported: %d %v %v", len(graph.Nodes), graph.Truncated, err)
	}
}

func TestGraphRelationsCrossIncludesCrossKBNodesAndEdges(t *testing.T) {
	r, db, _, _ := registryFixture(t)
	ctx := t.Context()
	now := time.Now().UTC()
	stamp := now.Format(time.RFC3339Nano)
	if _, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{
		EventID: "source-event", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "source-doc",
		ToState: lifecycle.Active, Reason: "fixture", OccurredAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec("INSERT INTO ext_lineage_node VALUES('source-node','7','kb','document_version','source-doc',?)", stamp); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO ext_lineage_node VALUES('target-node','7','other','document_version','target-doc',?)", stamp); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO ext_document_version_family VALUES('family-source','7','kb','source-doc','','source-node','source-node',1,?,?)", stamp, stamp); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO ext_document_version VALUES('source-node','7','kb','family-source','source-doc',?,1,'source-event','','text',?)", strings.Repeat("a", 64), stamp); err != nil {
			return err
		}
		_, err := tx.Exec("INSERT INTO ext_lineage_edge_cross_kb VALUES('cross-edge','7','kb','source-node','other','target-node','amends','governance_critical',?)", stamp)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	graph, err := r.Graph(ctx, fixtureScope, "relations-cross", "source-doc")
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 2 || len(graph.Edges) != 1 || graph.Edges[0].ID != "cross-edge" || graph.Edges[0].Type != "amends" {
		t.Fatalf("cross graph = %+v", graph)
	}
	byID := map[string]GraphNode{}
	for _, node := range graph.Nodes {
		byID[node.ID] = node
	}
	if byID["source-node"].KnowledgeBaseID != "kb" || byID["target-node"].KnowledgeBaseID != "other" {
		t.Fatalf("cross node KBs = %+v", graph.Nodes)
	}
	same, err := r.Graph(ctx, fixtureScope, "relations", "source-doc")
	if err != nil {
		t.Fatal(err)
	}
	if len(same.Nodes) != 1 || len(same.Edges) != 0 {
		t.Fatalf("same-KB graph leaked cross edge: %+v", same)
	}
	restricted, err := r.WithKnowledgeBases([]string{"kb"}).Graph(ctx, fixtureScope, "relations-cross", "source-doc")
	if err != nil || len(restricted.Nodes) != 1 || len(restricted.Edges) != 0 {
		t.Fatalf("cross graph leaked disallowed KB: %+v %v", restricted, err)
	}
}

func TestGraphProjectsOwnershipOnlyForMatchingObservedVersions(t *testing.T) {
	r, db, reader, _ := registryFixture(t)
	ctx := t.Context()
	parentHash, childHash := strings.Repeat("a", 32), strings.Repeat("b", 32)
	if _, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{
		EventID: "child-event", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "child",
		ToState: lifecycle.Active, Reason: "test", OccurredAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	observe := func(id, fileHash string) string {
		t.Helper()
		snapshot := fixtureSnapshot(t, id)
		snapshot.DocumentID = id
		snapshot.HeaderFields["file_hash"] = sha([]byte(strconv.Quote(fileHash)))
		snapshot.HeaderHash = headerDigest(snapshot.HeaderFields)
		snapshot.RootHash = rootDigest(snapshot)
		reader.snapshot = snapshot
		v, err := r.Refresh(ctx, fixtureScope, id)
		if err != nil {
			t.Fatal(err)
		}
		return v.ID
	}
	parent, child := observe("doc", parentHash), observe("child", childHash)
	importID, err := db.PrepareAttachmentImport(ctx, 7, "kb", "doc", parentHash, strings.Repeat("c", 64), childHash)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.ClaimAttachmentUpload(ctx, importID); err != nil {
		t.Fatal(err)
	}
	if err = db.BindAttachments(ctx, 7, "kb", "doc", parentHash, []store.AttachmentBinding{{ImportID: importID, AttachmentID: "child"}}); err != nil {
		t.Fatal(err)
	}
	graph, err := r.Graph(ctx, fixtureScope, "relations", "")
	if err != nil || len(graph.Edges) != 1 || graph.Edges[0] != (GraphEdge{ID: "attachment:child", Type: "attachment_of", From: child, To: parent}) {
		t.Fatalf("attachment graph = %+v, %v", graph.Edges, err)
	}
	graph, err = r.Graph(ctx, fixtureScope, "provenance", "")
	if err != nil || len(graph.Edges) != 0 {
		t.Fatalf("ownership leaked into provenance: %+v, %v", graph.Edges, err)
	}
	observe("doc", strings.Repeat("d", 32))
	graph, err = r.Graph(ctx, fixtureScope, "relations", "")
	if err != nil || len(graph.Edges) != 0 {
		t.Fatalf("old attachment projected onto replacement: %+v, %v", graph.Edges, err)
	}
}

func TestGraphProjectsAttachmentWithoutVersionFamily(t *testing.T) {
	r, db, _, _ := registryFixture(t)
	ctx := t.Context()
	parentHash, childHash := strings.Repeat("a", 32), strings.Repeat("b", 32)
	importID, err := db.PrepareAttachmentImport(ctx, 7, "kb", "doc", parentHash, strings.Repeat("c", 64), childHash)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.ClaimAttachmentUpload(ctx, importID); err != nil {
		t.Fatal(err)
	}
	if err = db.BindAttachments(ctx, 7, "kb", "doc", parentHash, []store.AttachmentBinding{{ImportID: importID, AttachmentID: "child"}}); err != nil {
		t.Fatal(err)
	}
	graph, err := r.Graph(ctx, fixtureScope, "relations", "doc")
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 2 || len(graph.Edges) != 1 || graph.Edges[0] != (GraphEdge{ID: "attachment:child", Type: "attachment_of", From: "document:child", To: "document:doc"}) {
		t.Fatalf("unversioned attachment graph = %+v", graph)
	}
	full, err := r.Graph(ctx, fixtureScope, "relations", "")
	if err != nil || len(full.Nodes) != 2 || len(full.Edges) != 1 {
		t.Fatalf("unversioned attachment full graph = %+v, %v", full, err)
	}
}

func TestSplitBranchesShareAncestryWithoutLeakingOtherModesOrScopes(t *testing.T) {
	r, db, reader, _ := registryFixture(t)
	ctx := t.Context()
	now := time.Now().UTC()
	stamp := now.Format(time.RFC3339Nano)
	observe := func(scope provenance.Scope, id string) Version {
		t.Helper()
		if _, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{
			EventID: id + "-active", TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID,
			DocumentID: id, ToState: lifecycle.Active, Reason: "fixture", OccurredAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		snapshot := fixtureSnapshot(t, id)
		snapshot.DocumentID, snapshot.TenantID, snapshot.KnowledgeBaseID = id, scope.TenantID, scope.KnowledgeBaseID
		snapshot.RootHash = rootDigest(snapshot)
		reader.snapshot = snapshot
		v, err := r.Refresh(ctx, scope, id)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	seed := func(scope provenance.Scope, batch string, published int) (Version, []Version) {
		t.Helper()
		source := observe(scope, batch+"-source")
		parts := []Version{observe(scope, batch+"-a"), observe(scope, batch+"-b"), observe(scope, batch+"-c")}
		err := db.Write(ctx, func(tx *sql.Tx) error {
			if _, err := tx.Exec(`INSERT INTO ext_revision_split_batches
				(tenant_id,knowledge_base_id,batch_id,source_document_id,source_version_id,status,revision,created_at)
				VALUES(?,?,?,?,?,'approved',2,?)`, scopeArgs(scope, batch, source.DocumentID, source.ID, stamp)...); err != nil {
				return err
			}
			for i, part := range parts {
				status, proposal := "indexed", part.DocumentID+"-proposal"
				var version, publishedAt any
				if i < published {
					status, version, publishedAt = "published", part.ID, stamp
				}
				if _, err := tx.Exec(`INSERT INTO ext_revision_proposals
					(tenant_id,knowledge_base_id,proposal_id,source_document_id,source_version_id,family_id,
					trigger_guidance_ids,proposed_patch,target_content_hash,status,revision,publication_key,target_document_id,created_at,published_at)
					VALUES(?,?,?,?,?,?,'[]','Split text',?,?,3,?,?,?,?)`,
					scopeArgs(scope, proposal, source.DocumentID, source.ID, part.FamilyID,
						sha([]byte("Split text")), status, proposal, part.DocumentID, stamp, publishedAt)...); err != nil {
					return err
				}
				if _, err := tx.Exec(`INSERT INTO ext_revision_split_parts
					(tenant_id,knowledge_base_id,batch_id,proposal_id,position,title,target_version_id)
					VALUES(?,?,?,?,?,?,?)`, scopeArgs(scope, batch, proposal, i+1, part.DocumentID, version)...); err != nil {
					return err
				}
			}
			_, err := tx.Exec(`UPDATE ext_revision_split_batches SET first_proposal_id=?
				WHERE tenant_id=? AND knowledge_base_id=? AND batch_id=?`,
				parts[0].DocumentID+"-proposal", strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, batch)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return source, parts
	}
	source, parts := seed(fixtureScope, "domestic", 2)
	foreignScope := provenance.Scope{TenantID: fixtureScope.TenantID, KnowledgeBaseID: "other"}
	_, foreign := seed(foreignScope, "foreign", 1)

	full, err := r.Graph(ctx, fixtureScope, "version-ancestry", "")
	if err != nil || full.Truncated || len(full.Edges) != 2 {
		t.Fatalf("version graph did not return the two published branches: %+v, %v", full.Edges, err)
	}
	for _, part := range parts[:2] {
		history, err := r.Graph(ctx, fixtureScope, "version-ancestry", part.ID)
		if err != nil || history.Truncated || len(history.Nodes) != 2 || len(history.Edges) != 1 {
			t.Fatalf("rooted split graph: %+v, %v", history, err)
		}
		edge := history.Edges[0]
		if edge.Type != "split_from" || edge.From != part.ID || edge.To != source.ID {
			t.Fatalf("wrong split direction or source: %+v", edge)
		}
		got, err := r.Query(ctx, fixtureScope, part.ID, "version-ancestry")
		if err != nil || len(got) != 1 || got[0] != (Ancestor{NodeID: source.ID, EdgeType: "split_from", Depth: 1}) {
			t.Fatalf("ancestry should reach the common source: %+v, %v", got, err)
		}
	}
	got, err := r.Query(ctx, fixtureScope, parts[2].ID, "version-ancestry")
	if err != nil || len(got) != 0 {
		t.Fatalf("indexed member appeared in version history before publish: %+v, %v", got, err)
	}
	for _, mode := range []string{"provenance", "relations"} {
		snapshot, err := r.Graph(ctx, fixtureScope, mode, "")
		if err != nil || len(snapshot.Edges) != 0 {
			t.Fatalf("split history leaked into %s: %+v, %v", mode, snapshot.Edges, err)
		}
	}
	if _, err := r.Graph(ctx, fixtureScope, "version-ancestry", foreign[0].ID); err != ErrEvidence {
		t.Fatalf("foreign split root admitted: %v", err)
	}
}
