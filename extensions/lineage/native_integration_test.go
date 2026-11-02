package lineage

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

func TestNativeSourceOwnedIntegration(t *testing.T) {
	key := os.Getenv("AGENTRAG_SOURCE_E2E_KEY")
	if key == "" {
		t.Skip("opt-in owned real Core/Host source integration")
	}
	owned := os.Getenv("AGENTRAG_SOURCE_E2E_OWNED_DIR")
	nonce := os.Getenv("AGENTRAG_SOURCE_E2E_NONCE")
	if !strings.HasPrefix(filepath.Base(owned), "m2-lifecycle-ui-") || len(nonce) < 32 {
		t.Fatal("owned fixture required")
	}
	marker, e := os.ReadFile(filepath.Join(owned, ".source-e2e-owned"))
	if e != nil || string(marker) != nonce {
		t.Fatal("owned fixture marker mismatch")
	}
	tenant, e := strconv.ParseUint(os.Getenv("AGENTRAG_SOURCE_E2E_TENANT"), 10, 64)
	if e != nil {
		t.Fatal("tenant required")
	}
	scope := provenance.Scope{TenantID: tenant, KnowledgeBaseID: os.Getenv("AGENTRAG_SOURCE_E2E_KB")}
	doc := os.Getenv("AGENTRAG_SOURCE_E2E_DOC")
	client, e := weknora.NewClient(os.Getenv("AGENTRAG_SOURCE_E2E_ORIGIN"), key, tenant)
	if e != nil {
		t.Fatal("native client unavailable")
	}
	reader, e := NewHTTPReader(client, []string{scope.KnowledgeBaseID})
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, e := store.OpenDurable(ctx, filepath.Join(owned, "ext.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	registry := NewRegistry(db, reader)
	authority := NewAuthority(registry, nil)
	v, e := registry.Refresh(ctx, scope, doc)
	if e != nil {
		t.Fatal("native snapshot registration", e)
	}
	ledger := provenance.New(db, authority)
	if _, e = ledger.RegisterSource(ctx, provenance.SourceRequest{Scope: scope, OperationID: "native-source-v1", DocumentID: doc, EvidenceID: v.ID}); e != nil {
		t.Fatal(e)
	}
	refs := []provenance.SourceRef{{DocumentID: doc, VersionID: v.ID}}
	if e = authority.VerifyCurrent(ctx, scope, refs); e != nil {
		t.Fatal(e)
	}
	t.Log("[SOURCE] real authorized Core snapshot -> immutable observed version -> source ledger PASS")
	var chunks struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if e = client.JSON(ctx, http.MethodGet, "/api/v1/chunks/"+url.PathEscape(doc), url.Values{"page": {"1"}, "page_size": {"1000"}}, nil, &chunks); e != nil || len(chunks.Data) == 0 {
		t.Fatal("native chunks unavailable")
	}
	if e = client.JSON(ctx, http.MethodPut, "/api/v1/chunks/"+url.PathEscape(doc)+"/"+url.PathEscape(chunks.Data[0].ID), nil, map[string]any{"content": "M3 independently edited document content."}, &struct{}{}); e != nil {
		t.Fatal("native chunk edit", e)
	}
	var next Version
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		next, e = registry.Refresh(ctx, scope, doc)
		if e == nil && next.ID != v.ID {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if e != nil || next.ID == v.ID || next.Sequence <= v.Sequence {
		t.Fatal("native edit did not create new observed version", e)
	}
	if e = authority.VerifyCurrent(ctx, scope, refs); e == nil {
		t.Fatal("old source version allowed after native edit")
	}
	state, e := lifecycle.NewStore(db).Current(ctx, lifecycle.Scope{TenantID: tenant, KnowledgeBaseID: scope.KnowledgeBaseID, DocumentID: doc})
	if e != nil || state.State != lifecycle.Active {
		t.Fatal("ordinary edit changed lifecycle", e)
	}
	t.Log("[SOURCE] native chunk edit -> new current root; old source refused; lifecycle remains active PASS")
	if _, e = authority.SealedCopy(ctx, scope, "copy", provenance.Binding{}, "model-claimed-complete"); e == nil {
		t.Fatal("missing complete run proof accepted")
	}
	t.Log("[BOUNDARY] no sealed-run authority: model complete claim rejected PASS")
}
