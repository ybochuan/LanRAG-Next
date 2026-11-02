package lineage

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func TestSupplementOwnedSweep(t *testing.T) {
	owned := os.Getenv("AGENTRAG_SUPPLEMENT_OWNED_DIR")
	if owned == "" {
		t.Skip("opt-in owned supplement integration")
	}
	nonce := os.Getenv("AGENTRAG_SUPPLEMENT_NONCE")
	marker, e := os.ReadFile(filepath.Join(owned, ".supplement-e2e-owned"))
	if !strings.HasPrefix(filepath.Base(owned), "m2-lifecycle-ui-") || len(nonce) < 32 || e != nil || string(marker) != nonce {
		t.Fatal("owned fixture required")
	}
	tenant, e := strconv.ParseUint(os.Getenv("AGENTRAG_SUPPLEMENT_TENANT"), 10, 64)
	if e != nil || tenant == 0 {
		t.Fatal("scope required")
	}
	scope := lifecycle.Scope{TenantID: tenant, KnowledgeBaseID: os.Getenv("AGENTRAG_SUPPLEMENT_KB"), DocumentID: os.Getenv("AGENTRAG_SUPPLEMENT_CHILD")}
	db, e := store.OpenDurable(context.Background(), filepath.Join(owned, "ext.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	m := lifecycle.NewMachine(db, time.Now)
	if e = m.Sweep(context.Background(), []lifecycle.PolicyScope{{TenantID: tenant, KnowledgeBaseID: scope.KnowledgeBaseID}}); e != nil {
		t.Fatal(e)
	}
	snap, e := m.Snapshot(context.Background(), scope)
	if e != nil || snap.Current.State != lifecycle.Stale {
		t.Fatal("child not invalidated", e)
	}
	t.Log("[DEPENDENCY-SWEEP] real persisted parent invalidation -> child stale via Machine PASS")
}
