package lineage

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
)

// This sealed-input fixture is test-only; production uses persisted run facts.
type childSealFixture struct{ version string }

func (s childSealFixture) SealedCopy(_ context.Context, scope provenance.Scope, copy string, b provenance.Binding, evidence string) (provenance.CopyProof, error) {
	return provenance.CopyProof{Scope: scope, CopyID: copy, Binding: b, EvidenceID: evidence, EvidenceHash: strings.Repeat("b", 64), RunID: "test-run", Sources: []provenance.SourceRef{{DocumentID: "child", VersionID: s.version}}}, nil
}

type inheritedSealFixture struct{ child, other string }

func (s inheritedSealFixture) SealedCopy(_ context.Context, scope provenance.Scope, copy string, b provenance.Binding, evidence string) (provenance.CopyProof, error) {
	p := provenance.CopyProof{Scope: scope, CopyID: copy, Binding: b, EvidenceID: evidence, EvidenceHash: strings.Repeat("b", 64), RunID: "test-run", Sources: []provenance.SourceRef{{DocumentID: "child", VersionID: s.child}}}
	if copy != "parent-copy" {
		p.Sources = []provenance.SourceRef{{DocumentID: "parent2", VersionID: s.other}}
		p.Parents = []provenance.ParentRef{{CopyID: "parent-copy", Revision: 1}}
	}
	return p, nil
}

func TestDerivedCopyRefreshesInheritedSourceDependencies(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "native_parent_changed"}[changed], func(t *testing.T) {
			s, db, reader := supplementFixture(t)
			ctx := context.Background()
			if _, e := s.Put(ctx, command("declare")); e != nil {
				t.Fatal(e)
			}
			child, e := s.registry.Refresh(ctx, fixtureScope, "child")
			if e != nil {
				t.Fatal(e)
			}
			other, e := s.registry.Refresh(ctx, fixtureScope, "parent2")
			if e != nil {
				t.Fatal(e)
			}
			a := NewAuthority(s.registry, inheritedSealFixture{child.ID, other.ID})
			l := provenance.New(db, a)
			for _, v := range []Version{child, other} {
				if _, e = l.RegisterSource(ctx, provenance.SourceRequest{Scope: fixtureScope, OperationID: "register-" + v.DocumentID, DocumentID: v.DocumentID, EvidenceID: v.ID}); e != nil {
					t.Fatal(e)
				}
			}
			b := provenance.Binding{UserID: "user", SessionID: "session", ResourceID: "resource", ContentVersion: "v1", SHA256: strings.Repeat("a", 64)}
			if _, e = l.Save(ctx, provenance.SaveRequest{Scope: fixtureScope, OperationID: "parent", CopyID: "parent-copy", Binding: b, EvidenceID: "sealed"}); e != nil {
				t.Fatal(e)
			}
			if changed {
				reader.mu.Lock()
				reader.bodies["parent"] = "new native parent content"
				reader.mu.Unlock()
			}
			receipt, e := l.Save(ctx, provenance.SaveRequest{Scope: fixtureScope, OperationID: "derived", CopyID: "derived-copy", Binding: b, EvidenceID: "sealed"})
			if changed {
				if e == nil || receipt.Revision != 0 {
					t.Fatal("inherited dependency admitted", receipt, e)
				}
				e = db.View(ctx, func(tx *sql.Tx) error {
					var n int
					if e := tx.QueryRow("SELECT COUNT(*) FROM ext_provenance_manifests").Scan(&n); e != nil {
						return e
					}
					if n != 1 {
						t.Fatal("failed derived admission persisted a manifest")
					}
					return nil
				})
				if e != nil {
					t.Fatal(e)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			m, e := l.Snapshot(ctx, fixtureScope, "derived-copy")
			if e != nil || len(m.Sources) != 2 {
				t.Fatal("wrong inherited model-source union", m, e)
			}
			for _, ref := range m.Sources {
				if ref.DocumentID == "parent" {
					t.Fatal("permission-only parent stored as model source")
				}
			}
			d, e := l.CheckRead(ctx, provenance.ReadRequest{Scope: fixtureScope, CopyID: "derived-copy", Revision: receipt.Revision, Binding: b})
			if e != nil || !d.Allowed {
				t.Fatal("valid inherited copy denied", d, e)
			}
		})
	}
}

type changingCopyAuthority struct {
	*Authority
	afterVerify func() error
	verifies    int
}

func (a *changingCopyAuthority) VerifyCurrent(ctx context.Context, s provenance.Scope, refs []provenance.SourceRef) error {
	a.verifies++
	if e := a.Authority.VerifyCurrent(ctx, s, refs); e != nil {
		return e
	}
	if a.afterVerify != nil {
		f := a.afterVerify
		a.afterVerify = nil
		return f()
	}
	return nil
}

func TestCopyPermissionRejectsDependencyAddedAfterNativeCheck(t *testing.T) {
	for _, mode := range []string{"read", "save"} {
		t.Run(mode, func(t *testing.T) {
			s, db, _ := supplementFixture(t)
			ctx := context.Background()
			if _, e := s.Put(ctx, command("declare-child")); e != nil {
				t.Fatal(e)
			}
			v, e := s.registry.Refresh(ctx, fixtureScope, "child")
			if e != nil {
				t.Fatal(e)
			}
			a := &changingCopyAuthority{Authority: NewAuthority(s.registry, childSealFixture{v.ID})}
			ledger := provenance.New(db, a)
			if _, e = ledger.RegisterSource(ctx, provenance.SourceRequest{Scope: fixtureScope, OperationID: "register", DocumentID: "child", EvidenceID: v.ID}); e != nil {
				t.Fatal(e)
			}
			binding := provenance.Binding{UserID: "user", SessionID: "session", ResourceID: "resource", ContentVersion: "v1", SHA256: strings.Repeat("a", 64)}
			save := provenance.SaveRequest{Scope: fixtureScope, OperationID: "save", CopyID: "copy", Binding: binding, EvidenceID: "sealed"}
			if mode == "read" {
				if _, e = ledger.Save(ctx, save); e != nil {
					t.Fatal(e)
				}
			}
			// The actual second declaration commits between remote proof and final view.
			a.afterVerify = func() error {
				cmd := command("grow-parent")
				cmd.Scope.DocumentID = "parent"
				cmd.Parents = []string{"parent2"}
				_, e := s.Put(ctx, cmd)
				return e
			}
			if mode == "save" {
				if receipt, e := ledger.Save(ctx, save); e == nil || receipt.Revision != 0 {
					t.Fatal("unverified new ancestor admitted", receipt, e)
				}
				e = db.View(ctx, func(tx *sql.Tx) error {
					var n int
					if e := tx.QueryRow("SELECT COUNT(*) FROM ext_provenance_manifests").Scan(&n); e != nil {
						return e
					}
					if n != 0 {
						t.Fatal("failed admission partially committed")
					}
					return nil
				})
				if e != nil {
					t.Fatal(e)
				}
			} else {
				req := provenance.ReadRequest{Scope: fixtureScope, CopyID: "copy", Revision: 1, Binding: binding}
				if d, _ := ledger.CheckRead(ctx, req); d.Allowed {
					t.Fatal("expanded dependency admitted without this request's proof")
				}
				if d, e := ledger.CheckRead(ctx, req); e != nil || !d.Allowed {
					t.Fatal("fresh retry with full closure should allow", d, e)
				}
				m, e := ledger.Snapshot(ctx, fixtureScope, "copy")
				if e != nil || len(m.Sources) != 1 || m.Sources[0].DocumentID != "child" {
					t.Fatal("governance parents fabricated as model inputs", m, e)
				}
				before := a.verifies
				req.Binding.UserID = "other"
				if d, _ := ledger.CheckRead(ctx, req); d.Allowed || a.verifies != before {
					t.Fatal("foreign binding caused proof I/O or permit")
				}
			}
		})
	}
}

func TestCopyPermissionIncludesSupplementDependencies(t *testing.T) {
	for _, mode := range []string{"active", "review", "parent_stale", "parent_expired", "parent_edited", "parent_offline", "missing_dependency_head"} {
		t.Run(mode, func(t *testing.T) {
			s, db, reader := supplementFixture(t)
			ctx := context.Background()
			if _, e := s.Put(ctx, command("declare")); e != nil {
				t.Fatal(e)
			}
			v, e := s.registry.Refresh(ctx, fixtureScope, "child")
			if e != nil {
				t.Fatal(e)
			}
			authority := NewAuthority(s.registry, childSealFixture{v.ID})
			ledger := provenance.New(db, authority)
			if _, e = ledger.RegisterSource(ctx, provenance.SourceRequest{Scope: fixtureScope, OperationID: "register", DocumentID: "child", EvidenceID: v.ID}); e != nil {
				t.Fatal(e)
			}
			binding := provenance.Binding{UserID: "user", SessionID: "session", ResourceID: "resource", ContentVersion: "v1", SHA256: strings.Repeat("a", 64)}
			if _, e = ledger.Save(ctx, provenance.SaveRequest{Scope: fixtureScope, OperationID: "save", CopyID: "copy", Binding: binding, EvidenceID: "sealed"}); e != nil {
				t.Fatal(e)
			}
			req := provenance.ReadRequest{Scope: fixtureScope, CopyID: "copy", Revision: 1, Binding: binding}
			if d, e := ledger.CheckRead(ctx, req); e != nil || !d.Allowed {
				t.Fatal("positive control", d, e)
			}
			parent := lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "parent"}
			switch mode {
			case "parent_stale":
				_, e = lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "parent-stale", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "parent", FromState: lifecycle.Active, ToState: lifecycle.Stale, Reason: "test", OccurredAt: time.Now()})
			case "parent_expired", "review":
				past := time.Now().Add(-time.Hour)
				annotations := lifecycle.Annotations{ReviewDue: &past}
				if mode == "parent_expired" {
					annotations = lifecycle.Annotations{EffectiveUntil: &past}
				}
				e = lifecycle.NewConfiguration(db).PutAnnotations(ctx, parent, annotations, 0)
			case "parent_edited":
				reader.mu.Lock()
				reader.bodies["parent"] = "changed native bytes"
				reader.mu.Unlock()
			case "parent_offline":
				reader.mu.Lock()
				delete(reader.bodies, "parent")
				reader.mu.Unlock()
			case "missing_dependency_head":
				e = db.Write(ctx, func(tx *sql.Tx) error {
					_, e := tx.Exec("DELETE FROM ext_supplement_current WHERE document_id='child'")
					return e
				})
			}
			if e != nil {
				t.Fatal(e)
			}
			// The child intentionally remains active until a later timer/observer sweep.
			current, e := lifecycle.NewStore(db).Current(ctx, lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "child"})
			if e != nil || current.State != lifecycle.Active {
				t.Fatal("fixture precondition", current, e)
			}
			d, e := ledger.CheckRead(ctx, req)
			want := mode == "active" || mode == "review"
			if d.Allowed != want || want && e != nil {
				t.Fatalf("copy permission mode=%s got=%+v err=%v", mode, d, e)
			}
			if !want {
				if receipt, e := ledger.Save(ctx, provenance.SaveRequest{Scope: fixtureScope, OperationID: "save-again", CopyID: "copy2", Binding: binding, EvidenceID: "sealed"}); e == nil || receipt.Revision != 0 {
					t.Fatal("invalid dependency admitted on copy registration", receipt, e)
				}
			}
		})
	}
}
