package lineage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/dependencies"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// SupplementCommand is native-authorized explicit governance, not model inference.
type SupplementCommand struct {
	Scope                  dependencies.Scope `json:"scope"`
	ActorID                string             `json:"actor_id"`
	OperationID            string             `json:"operation_id"`
	ExpectedRevision       int64              `json:"expected_revision"`
	Parents                []string           `json:"parents"`
	ParentKnowledgeBaseIDs []string           `json:"parent_knowledge_base_ids,omitempty"`
}

// SupplementReceipt proves a historical declaration commit, not current permission.
type SupplementReceipt struct {
	DeclarationID string `json:"declaration_id"`
	Revision      int64  `json:"revision"`
	Status        string `json:"status"`
}

// SupplementView includes current revision and effective dependency assessment.
type SupplementView struct {
	Revision               int64                   `json:"revision"`
	DeclarationID          string                  `json:"declaration_id"`
	Status                 string                  `json:"status"`
	Parents                []string                `json:"parents"`
	ParentKnowledgeBaseIDs []string                `json:"parent_knowledge_base_ids"`
	Assessment             dependencies.Assessment `json:"assessment"`
}

// Supplements owns critical declarations/edges/audit in the shared extension transaction.
type Supplements struct {
	db       *store.DB
	registry *Registry
	now      func() time.Time
}

// NewSupplements requires a configured trusted version registry for writes.
func NewSupplements(db *store.DB, r *Registry, now func() time.Time) *Supplements {
	if now == nil {
		now = time.Now
	}
	return &Supplements{db, r, now}
}
func supplementScope(s dependencies.Scope) provenance.Scope {
	return provenance.Scope{TenantID: s.TenantID, KnowledgeBaseID: s.KnowledgeBaseID}
}

func supplementDocumentKey(kb, doc string) string { return kb + "\x00" + doc }

func validateSupplementSources(a dependencies.Assessment, versions map[string]Version) error {
	for _, ref := range a.Sources {
		key := supplementDocumentKey(ref.KnowledgeBaseID, ref.DocumentID)
		version, ok := versions[key]
		if !ok {
			version = versions[ref.DocumentID]
		}
		if version.ID != ref.VersionID {
			return ErrConflict
		}
	}
	return nil
}

// Get is a scoped repository view; the native facade must authorize the child first.
func (s *Supplements) Get(ctx context.Context, scope dependencies.Scope) (SupplementView, error) {
	view := SupplementView{Parents: []string{}, ParentKnowledgeBaseIDs: []string{}}
	if s == nil || s.db == nil {
		return view, ErrUnavailable
	}
	e := s.db.View(ctx, func(tx *sql.Tx) error {
		e := tx.QueryRowContext(ctx, `SELECT d.declaration_id,d.revision,d.status FROM ext_supplement_current c JOIN ext_supplement_declarations d ON d.tenant_id=c.tenant_id AND d.knowledge_base_id=c.knowledge_base_id AND d.document_id=c.document_id AND d.declaration_id=c.declaration_id AND d.revision=c.revision WHERE c.tenant_id=? AND c.knowledge_base_id=? AND c.document_id=?`, scopeArgs(supplementScope(scope), scope.DocumentID)...).Scan(&view.DeclarationID, &view.Revision, &view.Status)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if e == nil && view.Status == "active" {
			rows, e := tx.QueryContext(ctx, `SELECT knowledge_base_id,parent_document_id FROM ext_supplement_parents WHERE declaration_id=?
				UNION ALL
				SELECT parent_knowledge_base_id,parent_document_id FROM ext_supplement_cross_parents WHERE declaration_id=?
				ORDER BY 1,2`, view.DeclarationID, view.DeclarationID)
			if e != nil {
				return e
			}
			for rows.Next() {
				var kb, id string
				if e = rows.Scan(&kb, &id); e != nil {
					rows.Close()
					return e
				}
				view.ParentKnowledgeBaseIDs = append(view.ParentKnowledgeBaseIDs, kb)
				view.Parents = append(view.Parents, id)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
		}
		view.Assessment, e = dependencies.LoadTx(ctx, tx, scope)
		return e
	})
	if e != nil {
		return SupplementView{}, ErrUnavailable
	}
	return view, nil
}
func supplementReplay(ctx context.Context, tx *sql.Tx, c SupplementCommand, hash string) (SupplementReceipt, bool, error) {
	var receipt SupplementReceipt
	var oldHash string
	e := tx.QueryRowContext(ctx, "SELECT declaration_id,revision,status,payload_hash FROM ext_supplement_declarations WHERE tenant_id=? AND knowledge_base_id=? AND operation_id=?", scopeArgs(supplementScope(c.Scope), c.OperationID)...).Scan(&receipt.DeclarationID, &receipt.Revision, &receipt.Status, &oldHash)
	if errors.Is(e, sql.ErrNoRows) {
		return receipt, false, nil
	}
	if e != nil {
		return receipt, false, e
	}
	if oldHash != hash {
		return SupplementReceipt{}, true, ErrConflict
	}
	return receipt, true, nil
}
func effectiveNow(ctx context.Context, tx *sql.Tx, scope dependencies.Scope, now time.Time, requireStarted bool) error {
	var from, until sql.NullString
	e := tx.QueryRowContext(ctx, "SELECT effective_from,effective_until FROM ext_document_annotations WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?", scopeArgs(supplementScope(scope), scope.DocumentID)...).Scan(&from, &until)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	for _, v := range []struct {
		date  sql.NullString
		start bool
	}{{from, true}, {until, false}} {
		if !v.date.Valid {
			continue
		}
		date, e := time.Parse(time.RFC3339Nano, v.date.String)
		if e != nil || v.start && requireStarted && now.Before(date) || !v.start && now.After(date) {
			return ErrEvidence
		}
	}
	return nil
}
func supplementCycle(ctx context.Context, tx *sql.Tx, scope dependencies.Scope, parents []string, parentKBs []string) error {
	type ref struct {
		kb  string
		doc string
	}
	queue := make([]ref, 0, len(parents))
	for i, doc := range parents {
		queue = append(queue, ref{parentKBs[i], doc})
	}
	seen := map[string]bool{}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current.kb == scope.KnowledgeBaseID && current.doc == scope.DocumentID {
			return ErrConflict
		}
		key := supplementDocumentKey(current.kb, current.doc)
		if seen[key] {
			continue
		}
		seen[key] = true
		if len(seen) > 64 {
			return ErrEvidence
		}
		rows, e := tx.QueryContext(ctx, `SELECT c.knowledge_base_id,p.parent_document_id
			FROM ext_supplement_current c
			JOIN ext_supplement_declarations d ON d.tenant_id=c.tenant_id AND d.knowledge_base_id=c.knowledge_base_id AND d.document_id=c.document_id AND d.declaration_id=c.declaration_id AND d.revision=c.revision
			JOIN ext_supplement_parents p ON p.declaration_id=d.declaration_id
			WHERE c.tenant_id=? AND c.knowledge_base_id=? AND c.document_id=? AND d.status='active'
			UNION ALL
			SELECT cp.parent_knowledge_base_id,cp.parent_document_id
			FROM ext_supplement_current c
			JOIN ext_supplement_declarations d ON d.tenant_id=c.tenant_id AND d.knowledge_base_id=c.knowledge_base_id AND d.document_id=c.document_id AND d.declaration_id=c.declaration_id AND d.revision=c.revision
			JOIN ext_supplement_cross_parents cp ON cp.declaration_id=d.declaration_id
			WHERE c.tenant_id=? AND c.knowledge_base_id=? AND c.document_id=? AND d.status='active'`,
			strconv.FormatUint(scope.TenantID, 10), current.kb, current.doc,
			strconv.FormatUint(scope.TenantID, 10), current.kb, current.doc)
		if e != nil {
			return e
		}
		for rows.Next() {
			var parent ref
			if e = rows.Scan(&parent.kb, &parent.doc); e != nil {
				rows.Close()
				return e
			}
			queue = append(queue, parent)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
	}
	return nil
}

// Put refreshes current native evidence, then atomically rechecks and commits
// declaration/parents/current/critical edges/linkage/audit. No relation tag intent
// is fabricated; lifecycle state changes use the existing Machine/Outbox path.
func (s *Supplements) Put(ctx context.Context, c SupplementCommand) (SupplementReceipt, error) {
	if s == nil || s.db == nil || s.registry == nil || c.Scope.TenantID == 0 || !validID(c.Scope.KnowledgeBaseID) || !validID(c.Scope.DocumentID) || !validID(c.ActorID) || !validID(c.OperationID) || c.ExpectedRevision < 0 || len(c.Parents) > 8 {
		return SupplementReceipt{}, ErrEvidence
	}
	if len(c.ParentKnowledgeBaseIDs) == 0 {
		c.ParentKnowledgeBaseIDs = make([]string, len(c.Parents))
		for i := range c.ParentKnowledgeBaseIDs {
			c.ParentKnowledgeBaseIDs[i] = c.Scope.KnowledgeBaseID
		}
	}
	if len(c.ParentKnowledgeBaseIDs) != len(c.Parents) {
		return SupplementReceipt{}, ErrEvidence
	}
	type parentRef struct {
		knowledgeBaseID string
		documentID      string
	}
	refs := make([]parentRef, len(c.Parents))
	for i, id := range c.Parents {
		if !validID(id) || !validID(c.ParentKnowledgeBaseIDs[i]) || (id == c.Scope.DocumentID && c.ParentKnowledgeBaseIDs[i] == c.Scope.KnowledgeBaseID) {
			return SupplementReceipt{}, ErrEvidence
		}
		refs[i] = parentRef{c.ParentKnowledgeBaseIDs[i], id}
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].knowledgeBaseID != refs[j].knowledgeBaseID {
			return refs[i].knowledgeBaseID < refs[j].knowledgeBaseID
		}
		return refs[i].documentID < refs[j].documentID
	})
	c.Parents = make([]string, len(refs))
	c.ParentKnowledgeBaseIDs = make([]string, len(refs))
	for i, ref := range refs {
		if i > 0 && refs[i-1] == ref {
			return SupplementReceipt{}, ErrEvidence
		}
		c.ParentKnowledgeBaseIDs[i], c.Parents[i] = ref.knowledgeBaseID, ref.documentID
	}
	payload, _ := json.Marshal(struct {
		Scope                  dependencies.Scope
		Actor                  string
		Expected               int64
		Parents                []string
		ParentKnowledgeBaseIDs []string
	}{c.Scope, c.ActorID, c.ExpectedRevision, c.Parents, c.ParentKnowledgeBaseIDs})
	hash := sha(payload)
	var receipt SupplementReceipt
	var replayed bool
	e := s.db.View(ctx, func(tx *sql.Tx) error {
		var e error
		receipt, replayed, e = supplementReplay(ctx, tx, c, hash)
		return e
	})
	if e != nil {
		return SupplementReceipt{}, e
	}
	if replayed {
		return receipt, nil
	}
	type documentRef struct {
		knowledgeBaseID string
		documentID      string
	}
	docs := map[string]documentRef{
		supplementDocumentKey(c.Scope.KnowledgeBaseID, c.Scope.DocumentID): {c.Scope.KnowledgeBaseID, c.Scope.DocumentID},
	}
	e = s.db.View(ctx, func(tx *sql.Tx) error {
		for _, ref := range refs {
			key := supplementDocumentKey(ref.knowledgeBaseID, ref.documentID)
			docs[key] = documentRef{ref.knowledgeBaseID, ref.documentID}
			a, e := dependencies.LoadTx(ctx, tx, dependencies.Scope{TenantID: c.Scope.TenantID, KnowledgeBaseID: ref.knowledgeBaseID, DocumentID: ref.documentID})
			if e != nil {
				return e
			}
			for _, source := range a.Sources {
				docs[supplementDocumentKey(source.KnowledgeBaseID, source.DocumentID)] = documentRef{source.KnowledgeBaseID, source.DocumentID}
			}
		}
		return nil
	})
	if e != nil {
		return SupplementReceipt{}, ErrEvidence
	}
	keys := make([]string, 0, len(docs))
	for key := range docs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	versions := map[string]Version{}
	for _, key := range keys {
		ref := docs[key]
		v, e := s.registry.Refresh(ctx, provenance.Scope{TenantID: c.Scope.TenantID, KnowledgeBaseID: ref.knowledgeBaseID}, ref.documentID)
		if e != nil {
			return SupplementReceipt{}, ErrEvidence
		}
		versions[key] = v
	}
	e = s.db.Write(ctx, func(tx *sql.Tx) error {
		var e error
		receipt, replayed, e = supplementReplay(ctx, tx, c, hash)
		if e != nil || replayed {
			return e
		}
		var currentRevision int64
		e = tx.QueryRowContext(ctx, "SELECT revision FROM ext_supplement_current WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?", scopeArgs(supplementScope(c.Scope), c.Scope.DocumentID)...).Scan(&currentRevision)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if currentRevision != c.ExpectedRevision {
			return ErrConflict
		}
		for key, observed := range versions {
			ref := docs[key]
			v, e := currentVersion(ctx, tx, provenance.Scope{TenantID: c.Scope.TenantID, KnowledgeBaseID: ref.knowledgeBaseID}, ref.documentID)
			if e != nil || v.ID != observed.ID {
				return ErrConflict
			}
		}
		childState, childEvent, e := store.ReadLifecycleCurrentTx(ctx, tx, c.Scope.TenantID, c.Scope.KnowledgeBaseID, c.Scope.DocumentID)
		if e != nil {
			return ErrEvidence
		}
		if len(refs) > 0 && (childState != "active" && childState != "pending" && childState != "scheduled") {
			return ErrEvidence
		}
		parentEvents := map[string]string{}
		now := s.now().UTC()
		for _, ref := range refs {
			key := supplementDocumentKey(ref.knowledgeBaseID, ref.documentID)
			state, event, e := store.ReadLifecycleCurrentTx(ctx, tx, c.Scope.TenantID, ref.knowledgeBaseID, ref.documentID)
			if e != nil || state != "active" {
				return ErrEvidence
			}
			parentEvents[key] = event
			parentScope := dependencies.Scope{TenantID: c.Scope.TenantID, KnowledgeBaseID: ref.knowledgeBaseID, DocumentID: ref.documentID}
			if e = effectiveNow(ctx, tx, parentScope, now, true); e != nil {
				return ErrEvidence
			}
			a, e := dependencies.LoadTx(ctx, tx, parentScope)
			if e != nil || a.Invalid(now) {
				return ErrEvidence
			}
			if e = validateSupplementSources(a, versions); e != nil {
				return e
			}
		}
		if len(refs) > 0 {
			if e = effectiveNow(ctx, tx, c.Scope, now, false); e != nil {
				return ErrEvidence
			}
		}
		if e = supplementCycle(ctx, tx, c.Scope, c.Parents, c.ParentKnowledgeBaseIDs); e != nil {
			return e
		}
		id, e := newID()
		if e != nil {
			return e
		}
		status := "active"
		if len(refs) == 0 {
			status = "revoked"
		}
		receipt = SupplementReceipt{id, currentRevision + 1, status}
		stamp := now.Format(time.RFC3339Nano)
		childKey := supplementDocumentKey(c.Scope.KnowledgeBaseID, c.Scope.DocumentID)
		_, e = tx.ExecContext(ctx, "INSERT INTO ext_supplement_declarations VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", id, strconv.FormatUint(c.Scope.TenantID, 10), c.Scope.KnowledgeBaseID, c.Scope.DocumentID, receipt.Revision, versions[childKey].ID, childEvent, c.ActorID, c.OperationID, hash, status, stamp)
		if e != nil {
			return e
		}
		for _, ref := range refs {
			key := supplementDocumentKey(ref.knowledgeBaseID, ref.documentID)
			if ref.knowledgeBaseID == c.Scope.KnowledgeBaseID {
				if _, e = tx.ExecContext(ctx, "INSERT INTO ext_supplement_parents VALUES(?,?,?,?,?,?,?)", id, strconv.FormatUint(c.Scope.TenantID, 10), c.Scope.KnowledgeBaseID, c.Scope.DocumentID, ref.documentID, versions[key].ID, parentEvents[key]); e != nil {
					return e
				}
				edgeID, e := newID()
				if e != nil {
					return e
				}
				if _, e = tx.ExecContext(ctx, "INSERT INTO ext_lineage_edge VALUES(?,?,?,?,?,'supplements','governance_critical',?) ON CONFLICT(tenant_id,knowledge_base_id,from_node_id,to_node_id,edge_type) DO NOTHING", edgeID, strconv.FormatUint(c.Scope.TenantID, 10), c.Scope.KnowledgeBaseID, versions[childKey].ID, versions[key].ID, stamp); e != nil {
					return e
				}
				if e = tx.QueryRowContext(ctx, "SELECT edge_id FROM ext_lineage_edge WHERE tenant_id=? AND knowledge_base_id=? AND from_node_id=? AND to_node_id=? AND edge_type='supplements'", scopeArgs(supplementScope(c.Scope), versions[childKey].ID, versions[key].ID)...).Scan(&edgeID); e != nil {
					return e
				}
				if _, e = tx.ExecContext(ctx, "INSERT INTO ext_supplement_edge_facts VALUES(?,?,?,?,?,?)", id, strconv.FormatUint(c.Scope.TenantID, 10), c.Scope.KnowledgeBaseID, c.Scope.DocumentID, ref.documentID, edgeID); e != nil {
					return e
				}
				continue
			}
			if _, e = tx.ExecContext(ctx, "INSERT INTO ext_supplement_cross_parents VALUES(?,?,?,?,?,?,?,?)", id, strconv.FormatUint(c.Scope.TenantID, 10), c.Scope.KnowledgeBaseID, c.Scope.DocumentID, ref.knowledgeBaseID, ref.documentID, versions[key].ID, parentEvents[key]); e != nil {
				return e
			}
			crossEdgeID, e := newID()
			if e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, "INSERT INTO ext_supplement_cross_edges VALUES(?,?,?,?,?,?,'supplements','governance_critical',?) ON CONFLICT(tenant_id,source_knowledge_base_id,source_node_id,target_knowledge_base_id,target_node_id,edge_type) DO NOTHING", crossEdgeID, strconv.FormatUint(c.Scope.TenantID, 10), c.Scope.KnowledgeBaseID, versions[childKey].ID, ref.knowledgeBaseID, versions[key].ID, stamp); e != nil {
				return e
			}
			if e = tx.QueryRowContext(ctx, "SELECT edge_id FROM ext_supplement_cross_edges WHERE tenant_id=? AND source_knowledge_base_id=? AND source_node_id=? AND target_knowledge_base_id=? AND target_node_id=? AND edge_type='supplements'", strconv.FormatUint(c.Scope.TenantID, 10), c.Scope.KnowledgeBaseID, versions[childKey].ID, ref.knowledgeBaseID, versions[key].ID).Scan(&crossEdgeID); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, "INSERT INTO ext_supplement_cross_edge_facts VALUES(?,?,?,?,?,?,?)", id, strconv.FormatUint(c.Scope.TenantID, 10), c.Scope.KnowledgeBaseID, c.Scope.DocumentID, ref.knowledgeBaseID, ref.documentID, crossEdgeID); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO ext_supplement_current VALUES(?,?,?,?,?) ON CONFLICT(tenant_id,knowledge_base_id,document_id) DO UPDATE SET revision=excluded.revision,declaration_id=excluded.declaration_id", strconv.FormatUint(c.Scope.TenantID, 10), c.Scope.KnowledgeBaseID, c.Scope.DocumentID, receipt.Revision, id); e != nil {
			return e
		}
		// Validate the new root as a whole: adding one child must not turn an
		// otherwise bounded parent graph into a depth-11 declaration.
		if _, e = dependencies.LoadTx(ctx, tx, c.Scope); e != nil {
			return ErrEvidence
		}
		_, e = tx.ExecContext(ctx, "INSERT INTO ext_supplement_audit(tenant_id,knowledge_base_id,document_id,declaration_id,actor_id,status,occurred_at) VALUES(?,?,?,?,?,?,?)", strconv.FormatUint(c.Scope.TenantID, 10), c.Scope.KnowledgeBaseID, c.Scope.DocumentID, id, c.ActorID, status, stamp)
		return e
	})
	if e != nil {
		if errors.Is(e, ErrConflict) || errors.Is(e, ErrEvidence) {
			return SupplementReceipt{}, e
		}
		return SupplementReceipt{}, ErrUnavailable
	}
	return receipt, nil
}
