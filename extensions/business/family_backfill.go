package business

import (
	"context"
	"database/sql"
	"encoding/hex"
	"slices"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

// LegacyFamilyLink ties a published legacy proposal to its exact critical edge.
type LegacyFamilyLink struct {
	ProposalID       string `json:"proposal_id"`
	Status           string `json:"status"`
	EdgeID           string `json:"edge_id"`
	OldDocumentID    string `json:"old_document_id"`
	OldVersionNodeID string `json:"old_version_node_id"`
	NewDocumentID    string `json:"new_document_id"`
	NewVersionNodeID string `json:"new_version_node_id"`
}

// LegacyFamilyIssue identifies evidence that prevents automatic application.
type LegacyFamilyIssue struct {
	Code       string `json:"code"`
	ProposalID string `json:"proposal_id,omitempty"`
}

// LegacyFamilyPlan is a read-only preview, including the evidence for confirmation.
type LegacyFamilyPlan struct {
	LegacyFamilyID           string              `json:"legacy_family_id"`
	Status                   string              `json:"status"`
	Fingerprint              string              `json:"fingerprint,omitempty"`
	MappedBusinessFamilyID   string              `json:"mapped_business_family_id,omitempty"`
	HeadDocumentID           string              `json:"head_document_id,omitempty"`
	HeadVersionNodeID        string              `json:"head_version_node_id,omitempty"`
	DocumentIDs              []string            `json:"document_ids"`
	Links                    []LegacyFamilyLink  `json:"links"`
	Issues                   []LegacyFamilyIssue `json:"issues"`
	ExcludedSplitProposalIDs []string            `json:"excluded_split_proposal_ids"`
}

// FamilyBackfillPlans reads one scoped page of historical publication candidates.
func (s *Service) FamilyBackfillPlans(ctx context.Context, c Command) ([]LegacyFamilyPlan, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	limit, offset, err := familyPage(c)
	if err != nil {
		return nil, err
	}
	plans := []LegacyFamilyPlan{}
	err = s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT DISTINCT family_id FROM ext_revision_proposals
			WHERE tenant_id=? AND knowledge_base_id=? AND status IN ('published','superseded') AND (?='' OR family_id=?)
			ORDER BY family_id LIMIT ? OFFSET ?`, args(c.Scope, c.ID, c.ID, limit, offset)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, id := range ids {
			plan, err := legacyFamilyPlanTx(ctx, tx, c, id)
			if err != nil {
				return err
			}
			err = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT business_family_id FROM ext_business_family_legacy_mappings
				WHERE tenant_id=? AND knowledge_base_id=? AND legacy_family_id=?),'')`,
				args(c.Scope, id)...).Scan(&plan.MappedBusinessFamilyID)
			if err != nil {
				return err
			}
			if plan.MappedBusinessFamilyID != "" {
				plan.Status, plan.Fingerprint = "applied", ""
			}
			plans = append(plans, plan)
		}
		return nil
	})
	return plans, err
}

func legacyFamilyPlanTx(ctx context.Context, tx *sql.Tx, c Command, id string) (LegacyFamilyPlan, error) {
	p := LegacyFamilyPlan{LegacyFamilyID: id, Status: "needs_review", DocumentIDs: []string{}, Links: []LegacyFamilyLink{},
		Issues: []LegacyFamilyIssue{}, ExcludedSplitProposalIDs: []string{}}
	rows, err := tx.QueryContext(ctx, `SELECT r.proposal_id,r.publication_key,r.status,r.source_document_id,COALESCE(old.version_node_id,''),
		r.target_document_id,COALESCE(e.from_node_id,''),COALESCE(e.edge_id,''),m.proposal_id IS NOT NULL
		FROM ext_revision_proposals r LEFT JOIN ext_revision_split_parts m USING(tenant_id,knowledge_base_id,proposal_id)
		LEFT JOIN ext_document_version old ON old.tenant_id=r.tenant_id AND old.knowledge_base_id=r.knowledge_base_id
			AND old.version_node_id=r.source_version_id AND old.document_id=r.source_document_id
		LEFT JOIN ext_lineage_edge e ON m.proposal_id IS NULL AND e.tenant_id=r.tenant_id AND e.knowledge_base_id=r.knowledge_base_id
			AND e.to_node_id=r.source_version_id AND e.edge_type='supersedes' AND e.edge_class='governance_critical'
			AND EXISTS (SELECT 1 FROM ext_document_version v WHERE v.tenant_id=e.tenant_id AND v.knowledge_base_id=e.knowledge_base_id
				AND v.version_node_id=e.from_node_id AND v.document_id=r.target_document_id)
		WHERE r.tenant_id=? AND r.knowledge_base_id=? AND r.family_id=? AND r.status IN ('published','superseded')
		ORDER BY r.proposal_id,e.edge_id LIMIT 257`, args(c.Scope, id)...)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	count := 0
	for rows.Next() {
		count++
		if count > 256 {
			p.Issues = append(p.Issues, LegacyFamilyIssue{Code: "evidence_limit"})
			break
		}
		var link LegacyFamilyLink
		var key string
		var split bool
		if err := rows.Scan(&link.ProposalID, &key, &link.Status, &link.OldDocumentID, &link.OldVersionNodeID,
			&link.NewDocumentID, &link.NewVersionNodeID, &link.EdgeID, &split); err != nil {
			return p, err
		}
		if split {
			p.ExcludedSplitProposalIDs = append(p.ExcludedSplitProposalIDs, link.ProposalID)
			continue
		}
		switch {
		case seen[link.ProposalID]:
			p.Issues = append(p.Issues, LegacyFamilyIssue{Code: "ambiguous_supersedes", ProposalID: link.ProposalID})
		case link.OldVersionNodeID == "" || link.EdgeID == "":
			p.Issues = append(p.Issues, LegacyFamilyIssue{Code: "missing_supersedes", ProposalID: link.ProposalID})
		case link.EdgeID != textHash(key+"supersedes"):
			p.Issues = append(p.Issues, LegacyFamilyIssue{Code: "unmatched_publication_receipt", ProposalID: link.ProposalID})
		default:
			p.Links = append(p.Links, link)
		}
		seen[link.ProposalID] = true
	}
	if err := rows.Err(); err != nil {
		return p, err
	}
	if len(p.Issues) > 0 {
		return p, nil
	}
	if len(p.Links) == 0 {
		if len(p.ExcludedSplitProposalIDs) > 0 {
			p.Status = "split_only"
		}
		return p, nil
	}
	p.validateLinearChain()
	return p, nil
}

func (p *LegacyFamilyPlan) validateLinearChain() {
	next := make(map[string]LegacyFamilyLink, len(p.Links))
	prev := make(map[string]LegacyFamilyLink, len(p.Links))
	for _, link := range p.Links {
		if next[link.OldDocumentID].EdgeID != "" || prev[link.NewDocumentID].EdgeID != "" || link.OldDocumentID == link.NewDocumentID {
			p.Issues = append(p.Issues, LegacyFamilyIssue{Code: "nonlinear_chain", ProposalID: link.ProposalID})
			return
		}
		next[link.OldDocumentID], prev[link.NewDocumentID] = link, link
	}
	var root string
	for doc := range next {
		if prev[doc].EdgeID == "" {
			if root != "" {
				p.Issues = append(p.Issues, LegacyFamilyIssue{Code: "disconnected_chain"})
				return
			}
			root = doc
		}
	}
	if root == "" {
		p.Issues = append(p.Issues, LegacyFamilyIssue{Code: "cyclic_chain"})
		return
	}
	doc := root
	ids := []string{root}
	for len(ids) <= len(p.Links) {
		link, ok := next[doc]
		if !ok {
			break
		}
		doc = link.NewDocumentID
		ids = append(ids, doc)
	}
	if len(ids) != len(p.Links)+1 || next[doc].EdgeID != "" {
		p.Issues = append(p.Issues, LegacyFamilyIssue{Code: "disconnected_chain"})
		return
	}
	if prev[doc].Status != "published" {
		p.Issues = append(p.Issues, LegacyFamilyIssue{Code: "no_current_publication", ProposalID: prev[doc].ProposalID})
		return
	}
	slices.Sort(ids)
	p.Status, p.DocumentIDs = "ready", ids
	p.HeadDocumentID, p.HeadVersionNodeID = doc, prev[doc].NewVersionNodeID
	p.Fingerprint = digest(*p)
}

func (s *Service) applyLegacyFamily(ctx context.Context, c Command) (Result, error) {
	body := c.Payload
	decoded, decodeErr := hex.DecodeString(body.BackfillFingerprint)
	if !validID(c.ID) || !validID(body.LegacyFamilyID) || c.ExpectedRevision < 1 ||
		!body.SecondConfirmation || decodeErr != nil || len(decoded) != 32 {
		return Result{}, ErrInvalid
	}
	var plan LegacyFamilyPlan
	if err := s.db.View(ctx, func(tx *sql.Tx) error {
		var err error
		plan, err = legacyFamilyPlanTx(ctx, tx, c, body.LegacyFamilyID)
		return err
	}); err != nil {
		return Result{}, err
	}
	if plan.Status != "ready" || plan.Fingerprint != body.BackfillFingerprint {
		return Result{}, ErrConflict
	}
	var documents []weknora.AttachmentDocument
	if s.registry != nil {
		var err error
		documents, err = s.nativeFamilyPrimaryDocuments(ctx, c.Scope, plan.DocumentIDs)
		if err != nil {
			return s.replayAfterObservationError(ctx, c, err)
		}
		if len(documents) != len(plan.DocumentIDs) {
			return Result{}, ErrConflict
		}
	}
	byDocument := make(map[string]weknora.AttachmentDocument, len(documents))
	for _, document := range documents {
		byDocument[document.ID] = document
	}
	return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		plan, err := legacyFamilyPlanTx(ctx, tx, c, body.LegacyFamilyID)
		if err != nil {
			return Result{}, err
		}
		if plan.Status != "ready" || plan.Fingerprint != body.BackfillFingerprint {
			return Result{}, ErrConflict
		}
		now := s.now().UTC()
		stamp := now.Format(time.RFC3339Nano)
		changed, err := tx.ExecContext(ctx, `UPDATE ext_business_document_families AS f SET revision=revision+1,updated_at=?
			WHERE tenant_id=? AND knowledge_base_id=? AND business_family_id=? AND revision=?
			AND NOT EXISTS (SELECT 1 FROM ext_business_family_published_heads h WHERE h.tenant_id=f.tenant_id
				AND h.knowledge_base_id=f.knowledge_base_id AND h.business_family_id=f.business_family_id)
			AND NOT EXISTS (SELECT 1 FROM ext_family_replacements r WHERE r.tenant_id=f.tenant_id
				AND r.knowledge_base_id=f.knowledge_base_id AND r.business_family_id=f.business_family_id AND r.status='scheduled')`,
			append([]any{stamp}, args(c.Scope, c.ID, c.ExpectedRevision)...)...)
		if err := requireFamilyChange(changed, err); err != nil {
			return Result{}, err
		}
		for _, doc := range plan.DocumentIDs {
			if err := requirePrimaryFamilyDocumentTx(ctx, tx, c.Scope, doc); err != nil {
				return Result{}, err
			}
			if document, ok := byDocument[doc]; ok {
				var version string
				if err := tx.QueryRowContext(ctx, `SELECT current_version_node_id FROM ext_document_version_family
					WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?`,
					args(c.Scope, doc)...).Scan(&version); err != nil {
					return Result{}, err
				}
				if err := requireFamilyHeaderTx(ctx, tx, c.Scope, document, version); err != nil {
					return Result{}, err
				}
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO ext_business_document_family_members
				(tenant_id,knowledge_base_id,document_id,business_family_id)
				SELECT tenant_id,knowledge_base_id,document_id,? FROM ext_document_version_family
				WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?
				ON CONFLICT(tenant_id,knowledge_base_id,document_id) DO NOTHING`, append([]any{c.ID}, args(c.Scope, doc)...)...); err != nil {
				return Result{}, err
			}
			var belongs bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ext_business_document_family_members
				WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND business_family_id=?)`,
				args(c.Scope, doc, c.ID)...).Scan(&belongs); err != nil {
				return Result{}, err
			}
			if !belongs {
				return Result{}, ErrConflict
			}
			if err := recordFamilyMembership(ctx, tx, c.Scope, c.ID, doc); err != nil {
				return Result{}, err
			}
		}
		changed, err = tx.ExecContext(ctx, `INSERT INTO ext_business_family_legacy_mappings
			(tenant_id,knowledge_base_id,legacy_family_id,business_family_id,head_document_id,head_version_node_id,fingerprint,operation_id,applied_at)
			VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(tenant_id,knowledge_base_id,legacy_family_id) DO NOTHING`,
			args(c.Scope, body.LegacyFamilyID, c.ID, plan.HeadDocumentID, plan.HeadVersionNodeID, plan.Fingerprint, c.OperationID, stamp)...)
		if err := requireFamilyChange(changed, err); err != nil {
			return Result{}, err
		}
		if err := enqueueFamilyProjectionIfEnabled(ctx, tx, c, now, plan.DocumentIDs); err != nil {
			return Result{}, err
		}
		return Result{ID: c.ID, Revision: c.ExpectedRevision + 1, Status: "applied"}, nil
	})
}
