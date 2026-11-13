package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// SuggestionPolicy is the scoring configuration for one knowledge base.
type SuggestionPolicy struct {
	Revision          int64   `json:"revision"`
	SimilarityWeight  float64 `json:"similarity_weight"`
	DeclarationWeight float64 `json:"declaration_weight"`
	StructureWeight   float64 `json:"structure_weight"`
	Threshold         float64 `json:"threshold"`
}

// SuggestionEvidence preserves measured signals and stable explanation codes.
type SuggestionEvidence struct {
	Similarity         float64  `json:"similarity"`
	Declaration        float64  `json:"declaration"`
	Structure          float64  `json:"structure"`
	Reasons            []string `json:"reasons"`
	TitleSimilarity    float64  `json:"title_similarity"`
	SemanticSimilarity *float64 `json:"semantic_similarity,omitempty"`
	SemanticModel      string   `json:"semantic_model,omitempty"`
}

// FamilySuggestion is a reviewable, version-pinned hypothesis within one library.
type FamilySuggestion struct {
	ID               string             `json:"id"`
	BusinessFamilyID string             `json:"business_family_id"`
	Kind             string             `json:"kind"`
	DocumentID       string             `json:"document_id"`
	TargetDocumentID string             `json:"target_document_id"`
	EdgeType         string             `json:"edge_type,omitempty"`
	SourceVersionID  string             `json:"source_version_id"`
	TargetVersionID  string             `json:"target_version_id"`
	Evidence         SuggestionEvidence `json:"evidence"`
	Confidence       float64            `json:"confidence"`
	PolicyRevision   int64              `json:"policy_revision"`
	Status           string             `json:"status"`
	Revision         int64              `json:"revision"`
	AppliedID        string             `json:"applied_id,omitempty"`
}

// FamilySuggestionInbox contains the scoring policy and pending suggestions.
type FamilySuggestionInbox struct {
	Policy     SuggestionPolicy   `json:"policy"`
	Items      []FamilySuggestion `json:"items"`
	HasNext    bool               `json:"has_next"`
	ScanStatus string             `json:"scan_status"`
}

func suggestionUnitValue(n float64) bool { return n >= 0 && n <= 1 }

func (p SuggestionPolicy) valid() bool {
	return suggestionUnitValue(p.SimilarityWeight) && suggestionUnitValue(p.DeclarationWeight) &&
		suggestionUnitValue(p.StructureWeight) && p.Threshold > 0 && p.Threshold <= 1 &&
		math.Abs(p.SimilarityWeight+p.DeclarationWeight+p.StructureWeight-1) < .000001
}

func (p SuggestionPolicy) confidence(e SuggestionEvidence) float64 {
	return min(1, p.SimilarityWeight*e.Similarity+p.DeclarationWeight*e.Declaration+p.StructureWeight*e.Structure)
}

func (s FamilySuggestion) valid() bool {
	return validID(s.BusinessFamilyID) && validID(s.DocumentID) && validID(s.TargetDocumentID) &&
		validID(s.SourceVersionID) && validID(s.TargetVersionID) && s.DocumentID != s.TargetDocumentID &&
		(s.Kind == "membership" && s.EdgeType == "" || s.Kind == "relationship" &&
			(s.EdgeType == "related_to" || s.EdgeType == "amends" || s.EdgeType == "interprets" || s.EdgeType == "supersedes")) &&
		suggestionUnitValue(s.Evidence.Similarity) && suggestionUnitValue(s.Evidence.Declaration) && suggestionUnitValue(s.Evidence.Structure) &&
		len(s.Evidence.Reasons) > 0
}

func (s FamilySuggestion) stableID() string {
	if s.Kind == "membership" {
		// A different supporting member must not revive an ignored recommendation.
		return digest([]string{s.BusinessFamilyID, s.Kind, s.DocumentID})
	}
	a, b := s.DocumentID, s.TargetDocumentID
	if s.EdgeType == "related_to" && a > b {
		a, b = b, a
	}
	return digest([]string{s.BusinessFamilyID, s.Kind, s.EdgeType, a, b})
}

func readSuggestionPolicy(ctx context.Context, tx *sql.Tx, scope provenance.Scope) (SuggestionPolicy, error) {
	var p SuggestionPolicy
	err := tx.QueryRowContext(ctx, `SELECT revision,similarity_weight,declaration_weight,structure_weight,threshold
		FROM ext_family_suggestion_policies WHERE tenant_id=? AND knowledge_base_id=?`, args(scope)...).
		Scan(&p.Revision, &p.SimilarityWeight, &p.DeclarationWeight, &p.StructureWeight, &p.Threshold)
	if errors.Is(err, sql.ErrNoRows) {
		return SuggestionPolicy{SimilarityWeight: .6, DeclarationWeight: .25, StructureWeight: .15, Threshold: .70}, nil
	}
	return p, err
}

func (s *Service) setFamilySuggestionPolicy(ctx context.Context, c Command) (Result, error) {
	p := c.Payload.SuggestionPolicy
	if c.ID != "" || p == nil || !p.valid() {
		return Result{}, ErrInvalid
	}
	return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		current, err := readSuggestionPolicy(ctx, tx, c.Scope)
		if err != nil {
			return Result{}, err
		}
		if current.Revision != c.ExpectedRevision {
			return Result{}, ErrConflict
		}
		rev := current.Revision + 1
		_, err = tx.ExecContext(ctx, `INSERT INTO ext_family_suggestion_policies VALUES(?,?,?,?,?,?,?)
			ON CONFLICT(tenant_id,knowledge_base_id) DO UPDATE SET revision=excluded.revision,
			similarity_weight=excluded.similarity_weight,declaration_weight=excluded.declaration_weight,
			structure_weight=excluded.structure_weight,threshold=excluded.threshold`,
			args(c.Scope, rev, p.SimilarityWeight, p.DeclarationWeight, p.StructureWeight, p.Threshold)...)
		return Result{ID: c.Scope.KnowledgeBaseID, Revision: rev, Status: "configured"}, err
	})
}

const suggestionColumns = `s.suggestion_id,s.business_family_id,s.kind,s.document_id,s.target_document_id,s.edge_type,
	s.source_version_id,s.target_version_id,s.evidence_json,s.confidence,s.policy_revision,s.status,s.revision,s.applied_id`

func scanSuggestion(row interface{ Scan(...any) error }) (FamilySuggestion, error) {
	var item FamilySuggestion
	var evidence []byte
	var applied sql.NullString
	err := row.Scan(&item.ID, &item.BusinessFamilyID, &item.Kind, &item.DocumentID, &item.TargetDocumentID, &item.EdgeType,
		&item.SourceVersionID, &item.TargetVersionID, &evidence, &item.Confidence, &item.PolicyRevision, &item.Status, &item.Revision, &applied)
	if err != nil {
		return item, err
	}
	item.AppliedID = applied.String
	return item, json.Unmarshal(evidence, &item.Evidence)
}

func readSuggestion(ctx context.Context, tx *sql.Tx, scope provenance.Scope, id string) (FamilySuggestion, error) {
	item, err := scanSuggestion(tx.QueryRowContext(ctx, `SELECT `+suggestionColumns+`
		FROM ext_family_suggestions s WHERE tenant_id=? AND knowledge_base_id=? AND suggestion_id=?`, args(scope, id)...))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return item, err
}

func suggestionFamilyRevision(ctx context.Context, tx *sql.Tx, scope provenance.Scope, id string) (int64, error) {
	var revision int64
	err := tx.QueryRowContext(ctx, `SELECT revision FROM ext_business_document_families
		WHERE tenant_id=? AND knowledge_base_id=? AND business_family_id=?`, args(scope, id)...).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return revision, err
}

// FamilySuggestions returns a same-scope inbox. Older policy revisions await rescoring.
func (s *Service) FamilySuggestions(ctx context.Context, c Command) (FamilySuggestionInbox, error) {
	out := FamilySuggestionInbox{Items: []FamilySuggestion{}}
	if s == nil || s.db == nil {
		return out, ErrUnavailable
	}
	limit, offset, err := familyPage(c)
	if err != nil || c.ID == "" || c.Payload.DocumentID != "" && !validID(c.Payload.DocumentID) {
		return out, ErrInvalid
	}
	err = s.db.View(ctx, func(tx *sql.Tx) error {
		if _, err := suggestionFamilyRevision(ctx, tx, c.Scope, c.ID); err != nil {
			return err
		}
		p, err := readSuggestionPolicy(ctx, tx, c.Scope)
		if err != nil {
			return err
		}
		out.Policy = p
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT status FROM ext_family_suggestion_scans
			WHERE tenant_id=? AND knowledge_base_id=? AND business_family_id=?),'idle')`, args(c.Scope, c.ID)...).Scan(&out.ScanStatus); err != nil {
			return err
		}
		asOf := s.now().UTC().Format(time.RFC3339Nano)
		out.Items, out.HasNext, err = workspacePage(ctx, tx, `SELECT `+suggestionColumns+`
			FROM ext_family_suggestions s
			JOIN ext_document_version_family f ON f.tenant_id=s.tenant_id AND f.knowledge_base_id=s.knowledge_base_id
				AND f.document_id=s.document_id AND f.current_version_node_id=s.source_version_id
			JOIN ext_document_version_family g ON g.tenant_id=s.tenant_id AND g.knowledge_base_id=s.knowledge_base_id
				AND g.document_id=s.target_document_id AND g.current_version_node_id=s.target_version_id
			JOIN ext_lifecycle_current c ON c.tenant_id=s.tenant_id AND c.knowledge_base_id=s.knowledge_base_id AND c.document_id=s.document_id
			`+store.LifecycleEventJoinSQL+`
			LEFT JOIN ext_document_annotations ann ON ann.tenant_id=c.tenant_id AND ann.knowledge_base_id=c.knowledge_base_id AND ann.document_id=c.document_id
			JOIN ext_lifecycle_current d ON d.tenant_id=s.tenant_id AND d.knowledge_base_id=s.knowledge_base_id AND d.document_id=s.target_document_id
			LEFT JOIN ext_business_document_family_members a ON a.tenant_id=s.tenant_id AND a.knowledge_base_id=s.knowledge_base_id AND a.document_id=s.document_id
			LEFT JOIN ext_business_document_family_members b ON b.tenant_id=s.tenant_id AND b.knowledge_base_id=s.knowledge_base_id AND b.document_id=s.target_document_id
			WHERE s.tenant_id=? AND s.knowledge_base_id=? AND s.business_family_id=?
				AND s.status='pending' AND s.policy_revision=? AND c.state='active' AND d.state='active'
				AND `+store.LifecycleReadableSQL+` AND NOT `+store.PublicationTargetPendingSQL+`
				AND (ann.effective_from IS NULL OR ann.effective_from='' OR ann.effective_from<=?)
				AND (ann.effective_until IS NULL OR ann.effective_until='' OR ann.effective_until>=?)
				AND (?='' OR s.document_id=? OR s.target_document_id=?)
				AND CASE WHEN s.kind='membership' THEN a.document_id IS NULL AND b.business_family_id=s.business_family_id
					WHEN s.edge_type='supersedes' THEN a.business_family_id=s.business_family_id AND b.business_family_id=s.business_family_id
					ELSE a.business_family_id=s.business_family_id OR b.business_family_id=s.business_family_id END
			ORDER BY s.confidence DESC,s.suggestion_id LIMIT ? OFFSET ?`, args(c.Scope, c.ID, p.Revision, asOf, asOf, c.Payload.DocumentID, c.Payload.DocumentID, c.Payload.DocumentID, limit+1, offset), limit,
			func(rows *sql.Rows) (FamilySuggestion, error) { return scanSuggestion(rows) })
		return err
	})
	return out, err
}

func suggestionMembersCurrent(ctx context.Context, tx *sql.Tx, scope provenance.Scope, item FamilySuggestion) error {
	var sourceFamily, targetFamily string
	read := `SELECT COALESCE((SELECT business_family_id FROM ext_business_document_family_members
		WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?),'')`
	for _, end := range []struct {
		doc string
		out *string
	}{{item.DocumentID, &sourceFamily}, {item.TargetDocumentID, &targetFamily}} {
		if err := tx.QueryRowContext(ctx, read, args(scope, end.doc)...).Scan(end.out); err != nil {
			return err
		}
	}
	sourceMember, targetMember := sourceFamily == item.BusinessFamilyID, targetFamily == item.BusinessFamilyID
	switch {
	case item.Kind == "membership" && sourceFamily == "" && targetMember:
		return nil
	case item.EdgeType == "supersedes" && sourceMember && targetMember:
		return nil
	case item.Kind == "relationship" && item.EdgeType != "supersedes" && (sourceMember || targetMember):
		return nil
	default:
		return ErrConflict
	}
}

// recordFamilySuggestions persists hypotheses. Confirmation owns governance writes.
func (s *Service) recordFamilySuggestions(ctx context.Context, scope provenance.Scope, items []FamilySuggestion) error {
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		p, err := readSuggestionPolicy(ctx, tx, scope)
		if err != nil {
			return err
		}
		for _, item := range items {
			if !item.valid() {
				return ErrInvalid
			}
			confidence := p.confidence(item.Evidence)
			if confidence < p.Threshold {
				continue
			}
			if err := s.suggestionCurrent(ctx, tx, scope, item); errors.Is(err, ErrConflict) {
				continue
			} else if err != nil {
				return err
			}
			evidence, err := json.Marshal(item.Evidence)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO ext_family_suggestions VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'pending',1,NULL)
				ON CONFLICT(tenant_id,knowledge_base_id,suggestion_id) DO UPDATE SET
				document_id=excluded.document_id,target_document_id=excluded.target_document_id,
				source_version_id=excluded.source_version_id,target_version_id=excluded.target_version_id,evidence_json=excluded.evidence_json,
				confidence=excluded.confidence,policy_revision=excluded.policy_revision,revision=ext_family_suggestions.revision+1
				WHERE ext_family_suggestions.status='pending' AND
				(ext_family_suggestions.policy_revision<>excluded.policy_revision OR
				ext_family_suggestions.source_version_id<>excluded.source_version_id OR
				ext_family_suggestions.target_version_id<>excluded.target_version_id OR
				ext_family_suggestions.evidence_json<>excluded.evidence_json)`,
				args(scope, item.stableID(), item.BusinessFamilyID, item.Kind, item.DocumentID, item.TargetDocumentID, item.EdgeType,
					item.SourceVersionID, item.TargetVersionID, string(evidence), confidence, p.Revision)...)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) suggestionCurrent(ctx context.Context, tx *sql.Tx, scope provenance.Scope, item FamilySuggestion) error {
	if err := suggestionMembersCurrent(ctx, tx, scope, item); err != nil {
		return err
	}
	if err := s.current(ctx, tx, scope, item.DocumentID, item.SourceVersionID); err != nil {
		return err
	}
	return s.current(ctx, tx, scope, item.TargetDocumentID, item.TargetVersionID)
}
