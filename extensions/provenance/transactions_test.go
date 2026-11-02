package provenance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"path/filepath"
)

var factTables = []string{"ext_provenance_sources", "ext_provenance_source_heads", "ext_provenance_manifests", "ext_provenance_copy_heads", "ext_provenance_manifest_sources", "ext_provenance_manifest_parents", "ext_provenance_receipts", "ext_provenance_audit"}

func factCounts(t *testing.T, l *Ledger) []int {
	t.Helper()
	result := make([]int, len(factTables))
	err := l.db.View(context.Background(), func(tx *sql.Tx) error {
		for i, table := range factTables {
			if err := tx.QueryRow("SELECT count(*) FROM " + table).Scan(&result[i]); err != nil {
				return err
			}
		}
		rows, err := tx.Query("PRAGMA foreign_key_check")
		if err != nil {
			return err
		}
		defer rows.Close()
		if rows.Next() {
			return errors.New("broken foreign key")
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestInconsistentLifecycleTruthCannotPermitCopy(t *testing.T) {
	l, a := setup(t)
	seedSource(t, l, a, "source")
	saveReady(t, l, a, "copy", []SourceRef{{"source", "v1"}}, nil)
	err := l.db.Write(context.Background(), func(tx *sql.Tx) error {
		_, e := tx.Exec(`INSERT INTO ext_lifecycle_events(event_id,tenant_id,knowledge_base_id,document_id,from_state,to_state,reason,occurred_at,stage,attempt,payload_json) VALUES('new-stale','7','kb','source','active','stale','fixture','2026-11-01T00:00:00Z','fixture',0,'{}')`)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	requireRead(t, l, request("copy", 1), false)
}

func TestChangedParentInvalidatesOldChildrenAndFurtherCopies(t *testing.T) {
	l, a := setup(t)
	seedSource(t, l, a, "one")
	seedSource(t, l, a, "two")
	saveReady(t, l, a, "parent", []SourceRef{{"one", "v1"}}, nil)
	saveReady(t, l, a, "child", nil, []ParentRef{{"parent", 1}})
	requireRead(t, l, request("child", 1), true)
	p := readyProof("parent", []SourceRef{{"one", "v1"}, {"two", "v1"}}, nil)
	p.EvidenceID = "correction"
	a.copies["correction"] = p
	if _, err := l.Save(context.Background(), SaveRequest{Scope: testScope, CopyID: "parent", OperationID: "correction", Binding: testBinding, EvidenceID: "correction", ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	requireRead(t, l, request("child", 1), false)
	a.copies["grandchild"] = readyProof("grandchild", nil, []ParentRef{{"child", 1}})
	if _, err := l.Save(context.Background(), SaveRequest{Scope: testScope, CopyID: "grandchild", OperationID: "grandchild", Binding: testBinding, EvidenceID: "grandchild"}); !errors.Is(err, ErrEvidence) {
		t.Fatalf("invalid parent copied: %v", err)
	}
}

func TestAdmissionRejectsCurrentHeadDependencyCycle(t *testing.T) {
	l, a := setup(t)
	seedSource(t, l, a, "doc")
	saveReady(t, l, a, "a", []SourceRef{{"doc", "v1"}}, nil)
	saveReady(t, l, a, "b", nil, []ParentRef{{"a", 1}})
	p := readyProof("a", nil, []ParentRef{{"b", 1}})
	p.EvidenceID = "a2"
	a.copies["a2"] = p
	before := factCounts(t, l)
	if _, err := l.Save(context.Background(), SaveRequest{Scope: testScope, CopyID: "a", OperationID: "a2", ExpectedRevision: 1, Binding: testBinding, EvidenceID: "a2"}); !errors.Is(err, ErrEvidence) {
		t.Fatalf("unreadable dependency admitted: %v", err)
	}
	if after := factCounts(t, l); !reflect.DeepEqual(before, after) {
		t.Fatal(before, after)
	}
	requireRead(t, l, request("a", 1), true)
	requireRead(t, l, request("b", 1), true)
}

func TestCASAcrossIndependentSQLiteWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")
	first, err := store.OpenDurable(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := store.OpenDurable(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	a := &testAuthority{sources: map[string]SourceProof{}, copies: map[string]CopyProof{}}
	one, two := New(first, a), New(second, a)
	seedSource(t, one, a, "doc")
	saveReady(t, one, a, "copy", []SourceRef{{"doc", "v1"}}, nil)
	if ok, conflicts := runConcurrent(t, func(n int) error {
		l := one
		if n%2 == 1 {
			l = two
		}
		_, err := l.Save(context.Background(), SaveRequest{Scope: testScope, CopyID: "copy", OperationID: fmt.Sprintf("writer-%d", n), Binding: testBinding, EvidenceID: "copy", ExpectedRevision: 1})
		return err
	}); ok != 1 || conflicts != 15 {
		t.Fatal(ok, conflicts)
	}
	requireRead(t, two, request("copy", 2), true)
}
func readyProof(id string, sources []SourceRef, parents []ParentRef) CopyProof {
	return CopyProof{Scope: testScope, CopyID: id, Binding: testBinding, EvidenceID: id, EvidenceHash: strings.Repeat("d", 64), RunID: "run", Sources: sources, Parents: parents}
}
func runConcurrent(t *testing.T, run func(int) error) (ok, conflicts int) {
	t.Helper()
	var wg sync.WaitGroup
	out := make(chan error, 16)
	for n := 0; n < 16; n++ {
		wg.Add(1)
		go func(n int) { defer wg.Done(); out <- run(n) }(n)
	}
	wg.Wait()
	close(out)
	for err := range out {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent result: %v", err)
		}
	}
	return
}

func TestRealConcurrentFirstWriteAndCAS(t *testing.T) {
	l, a := setup(t)
	seedSource(t, l, a, "doc")
	a.copies["copy"] = readyProof("copy", []SourceRef{{"doc", "v1"}}, nil)
	command := SaveRequest{Scope: testScope, CopyID: "copy", OperationID: "first", Binding: testBinding, EvidenceID: "copy"}
	if ok, conflicts := runConcurrent(t, func(int) error { _, err := l.Save(context.Background(), command); return err }); ok != 16 || conflicts != 0 {
		t.Fatal(ok, conflicts)
	}
	counts := factCounts(t, l)
	if counts[2] != 1 || counts[6] != 2 || counts[7] != 2 {
		t.Fatal(counts)
	}
	if ok, conflicts := runConcurrent(t, func(n int) error {
		r := command
		r.OperationID = fmt.Sprintf("cas-%d", n)
		r.ExpectedRevision = 1
		_, err := l.Save(context.Background(), r)
		return err
	}); ok != 1 || conflicts != 15 {
		t.Fatal(ok, conflicts)
	}
	requireRead(t, l, request("copy", 1), false)
	requireRead(t, l, request("copy", 2), true)
	proof := a.sources["doc"]
	proof.VersionID = "v2"
	proof.ObservationSequence = 2
	proof.EvidenceID = "v2"
	a.sources["v2"] = proof
	if ok, conflicts := runConcurrent(t, func(n int) error {
		_, err := l.RegisterSource(context.Background(), SourceRequest{Scope: testScope, OperationID: fmt.Sprintf("source-cas-%d", n), DocumentID: "doc", EvidenceID: "v2", ExpectedRevision: 1})
		return err
	}); ok != 1 || conflicts != 15 {
		t.Fatal(ok, conflicts)
	}
	requireRead(t, l, request("copy", 2), false)
}

func TestManifestEveryFactAndAuditRollsBack(t *testing.T) {
	for _, table := range factTables[2:] {
		t.Run(table, func(t *testing.T) {
			l, a := setup(t)
			seedSource(t, l, a, "doc")
			saveReady(t, l, a, "parent", []SourceRef{{"doc", "v1"}}, nil)
			a.copies["child"] = readyProof("child", nil, []ParentRef{{"parent", 1}})
			before := factCounts(t, l)
			if err := l.db.Write(context.Background(), func(tx *sql.Tx) error {
				_, err := tx.Exec("CREATE TRIGGER prov_fail BEFORE INSERT ON " + table + " BEGIN SELECT RAISE(ABORT,'injected provenance failure'); END")
				return err
			}); err != nil {
				t.Fatal(err)
			}
			r := SaveRequest{Scope: testScope, CopyID: "child", OperationID: "child", Binding: testBinding, EvidenceID: "child"}
			if receipt, err := l.Save(context.Background(), r); err == nil || receipt != (Receipt{}) {
				t.Fatal("failed transaction returned a committed receipt", receipt, err)
			}
			if after := factCounts(t, l); !reflect.DeepEqual(before, after) {
				t.Fatalf("partial facts: before=%v after=%v", before, after)
			}
			if err := l.db.Write(context.Background(), func(tx *sql.Tx) error { _, err := tx.Exec("DROP TRIGGER prov_fail"); return err }); err != nil {
				t.Fatal(err)
			}
			if _, err := l.Save(context.Background(), r); err != nil {
				t.Fatal(err)
			}
			requireRead(t, l, request("child", 1), true)
		})
	}
}

func TestSourceHeadReceiptAuditRollsBack(t *testing.T) {
	for _, table := range []string{factTables[0], factTables[1], factTables[6], factTables[7]} {
		t.Run(table, func(t *testing.T) {
			l, a := setup(t)
			seedSource(t, l, a, "doc")
			proof := a.sources["doc"]
			proof.VersionID = "v2"
			proof.ObservationSequence = 2
			proof.EvidenceID = "v2"
			a.sources["v2"] = proof
			before := factCounts(t, l)
			if err := l.db.Write(context.Background(), func(tx *sql.Tx) error {
				_, err := tx.Exec("CREATE TRIGGER prov_fail BEFORE INSERT ON " + table + " BEGIN SELECT RAISE(ABORT,'injected source failure'); END")
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := l.RegisterSource(context.Background(), SourceRequest{Scope: testScope, OperationID: "v2", DocumentID: "doc", EvidenceID: "v2", ExpectedRevision: 1}); err == nil {
				t.Fatal("fault accepted")
			}
			if after := factCounts(t, l); !reflect.DeepEqual(before, after) {
				t.Fatal(before, after)
			}
		})
	}
}

func TestProofSpoofMissingSourceAndClosedDatabaseFailClosed(t *testing.T) {
	l, a := setup(t)
	seedSource(t, l, a, "doc")
	for _, kind := range []string{"scope", "owner", "empty", "unknown", "self-parent", "missing-parent"} {
		p := readyProof(kind, []SourceRef{{"doc", "v1"}}, nil)
		switch kind {
		case "scope":
			p.Scope.TenantID = 8
		case "owner":
			p.Binding.UserID = "other"
		case "empty":
			p.Sources = nil
		case "unknown":
			p.Sources = []SourceRef{{"absent", "v1"}}
		case "self-parent":
			p.Parents = []ParentRef{{kind, 1}}
		case "missing-parent":
			p.Parents = []ParentRef{{"absent", 1}}
		}
		a.copies[kind] = p
		if _, err := l.Save(context.Background(), SaveRequest{Scope: testScope, CopyID: kind, OperationID: kind, Binding: testBinding, EvidenceID: kind}); !errors.Is(err, ErrEvidence) {
			t.Fatalf("%s: %v", kind, err)
		}
	}
	if err := l.db.Close(); err != nil {
		t.Fatal(err)
	}
	d, err := l.CheckRead(context.Background(), request("absent", 1))
	if d.Allowed || !errors.Is(err, ErrUnavailable) {
		t.Fatal(d, err)
	}
}

func TestExpiryBeforeStateSweepIsDenied(t *testing.T) {
	l, a := setup(t)
	seedSource(t, l, a, "doc")
	saveReady(t, l, a, "copy", []SourceRef{{"doc", "v1"}}, nil)
	expired := time.Now().Add(-time.Hour)
	if err := lifecycle.NewConfiguration(l.db).PutAnnotations(context.Background(), lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "doc"}, lifecycle.Annotations{EffectiveUntil: &expired}, 0); err != nil {
		t.Fatal(err)
	}
	requireRead(t, l, request("copy", 1), false)
}
