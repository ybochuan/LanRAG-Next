package business

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// ErrNotFound means a requested business object is absent from the authorized scope.
var ErrNotFound = errors.New("business object not found")

// WorkspaceMember describes membership independently from document lifecycle.
type WorkspaceMember struct {
	DocumentID   string     `json:"document_id"`
	State        string     `json:"state,omitempty"`
	Availability string     `json:"availability,omitempty"`
	RemovedAt    *time.Time `json:"removed_at,omitempty"`
	Reason       string     `json:"reason,omitempty"`
}

// WorkspaceRelation preserves the declared direction and its version anchors.
// Current identifies a current governance record, not a retrieval permit.
type WorkspaceRelation struct {
	ID                    string `json:"id"`
	DocumentID            string `json:"document_id"`
	TargetDocumentID      string `json:"target_document_id"`
	EdgeType              string `json:"edge_type"`
	Status                string `json:"status"`
	Revision              int64  `json:"revision"`
	Scope                 string `json:"scope"`
	Current               bool   `json:"current"`
	SourceVersionID       string `json:"source_version_id,omitempty"`
	TargetVersionID       string `json:"target_version_id,omitempty"`
	KnowledgeBaseID       string `json:"knowledge_base_id"`
	TargetKnowledgeBaseID string `json:"target_knowledge_base_id,omitempty"`
}

// WorkspaceActivity exposes an audit receipt without replaying the original payload.
type WorkspaceActivity struct {
	OperationID string    `json:"operation_id"`
	ActorID     string    `json:"actor_id"`
	Action      string    `json:"action"`
	AggregateID string    `json:"aggregate_id"`
	Status      string    `json:"status"`
	OccurredAt  time.Time `json:"occurred_at"`
}

// DocumentActivity reads a document's governance receipts in the authorized library.
func (s *Service) DocumentActivity(ctx context.Context, c Command) ([]WorkspaceActivity, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	limit, offset, err := familyPage(c)
	if err != nil || !validID(c.Payload.DocumentID) {
		return nil, ErrInvalid
	}
	out := []WorkspaceActivity{}
	err = s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, documentActivitySQL, args(c.Scope, c.Payload.DocumentID, limit, offset)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var event WorkspaceActivity
			var occurred string
			if err := rows.Scan(&event.OperationID, &event.ActorID, &event.Action, &event.AggregateID, &event.Status, &occurred); err != nil {
				return err
			}
			event.OccurredAt, err = time.Parse(time.RFC3339Nano, occurred)
			if err != nil {
				return err
			}
			out = append(out, event)
		}
		return rows.Err()
	})
	return out, err
}

const documentActivitySQL = `WITH scope(tenant_id,knowledge_base_id,document_id) AS (VALUES(?,?,?)),
relations AS (
 SELECT r.relation_id FROM ` + relationRecords + ` r JOIN scope s USING(tenant_id,knowledge_base_id)
 WHERE json_extract(r.details,'$.document_id')=s.document_id OR json_extract(r.details,'$.target_document_id')=s.document_id
),
revisions AS (
 SELECT r.proposal_id FROM ` + revisionRecords + ` r JOIN scope s USING(tenant_id,knowledge_base_id)
 WHERE json_extract(r.details,'$.source_document_id')=s.document_id OR json_extract(r.details,'$.target_document_id')=s.document_id
 UNION
 SELECT p.batch_id FROM ext_revision_split_parts p JOIN ext_revision_proposals r USING(tenant_id,knowledge_base_id,proposal_id)
 JOIN scope s USING(tenant_id,knowledge_base_id) WHERE r.status='published' AND r.target_document_id=s.document_id
),
suggestions AS (
 SELECT g.suggestion_id FROM ext_family_suggestions g JOIN scope s USING(tenant_id,knowledge_base_id)
 WHERE g.document_id=s.document_id OR g.target_document_id=s.document_id
),
replacements AS (
 SELECT r.replacement_id FROM ext_family_replacements r JOIN scope s USING(tenant_id,knowledge_base_id)
 WHERE r.old_document_id=s.document_id OR r.new_document_id=s.document_id
)
SELECT operation_id,actor_id,action,aggregate_id,status,occurred_at FROM (
 SELECT a.operation_id,a.actor_id,a.action,a.aggregate_id,a.status,a.occurred_at,a.sequence
 FROM ext_business_audit a JOIN scope s USING(tenant_id,knowledge_base_id)
 WHERE (a.action IN ('family_member_add','family_member_remove') AND json_extract(a.payload_json,'$.payload.document_id')=s.document_id)
 OR (a.action IN ('family_suggestion_confirm','family_suggestion_ignore') AND a.aggregate_id IN (SELECT suggestion_id FROM suggestions))
 OR ((a.action GLOB 'relation_*' OR a.action GLOB 'repeal_*' OR a.action GLOB 'clause_*') AND a.aggregate_id IN (SELECT relation_id FROM relations))
 OR ((a.action GLOB 'revision_*' OR a.action GLOB 'split_*') AND a.aggregate_id IN (SELECT proposal_id FROM revisions))
 OR (a.action GLOB 'family_replace_*' AND a.aggregate_id IN (SELECT replacement_id FROM replacements))
 OR (a.action='family_replace_history' AND EXISTS (
  SELECT 1 FROM json_each(a.payload_json,'$.payload.replacement_ids') ids
  WHERE ids.value IN (SELECT replacement_id FROM replacements)))
 UNION ALL
 SELECT d.operation_id,a.actor_id,'supplements_'||a.status,a.document_id,a.status,a.occurred_at,a.sequence
 FROM ext_supplement_audit a JOIN scope s USING(tenant_id,knowledge_base_id)
 JOIN ext_supplement_declarations d USING(tenant_id,knowledge_base_id,document_id,declaration_id)
 WHERE a.document_id=s.document_id OR EXISTS (
  SELECT 1 FROM ext_supplement_declarations prior
  JOIN ext_supplement_parents p USING(tenant_id,knowledge_base_id,document_id,declaration_id)
  WHERE prior.tenant_id=d.tenant_id AND prior.knowledge_base_id=d.knowledge_base_id AND prior.document_id=d.document_id
  AND prior.revision IN (d.revision,d.revision-1) AND p.parent_document_id=s.document_id)
) ORDER BY occurred_at DESC,sequence DESC,operation_id LIMIT ? OFFSET ?`

// FamilyWorkspace contains one tab of the workbench and the authoritative family header.
type FamilyWorkspace struct {
	Family     BusinessFamily           `json:"family"`
	Projection *FamilyProjectionSetting `json:"projection,omitempty"`
	View       string                   `json:"view"`
	HasNext    bool                     `json:"has_next"`
	Members    []WorkspaceMember        `json:"members,omitempty"`
	Relations  []WorkspaceRelation      `json:"relations,omitempty"`
	Activity   []WorkspaceActivity      `json:"activity,omitempty"`
}

// FamilyWorkspace reads one page within one scoped SQLite snapshot.
func (s *Service) FamilyWorkspace(ctx context.Context, c Command, view string) (FamilyWorkspace, error) {
	if s == nil || s.db == nil {
		return FamilyWorkspace{}, ErrUnavailable
	}
	limit, offset, err := familyPage(c)
	if err != nil || c.ID == "" {
		return FamilyWorkspace{}, ErrInvalid
	}
	if view == "" {
		view = "members"
	}
	out := FamilyWorkspace{View: view}
	if projection, e := s.FamilyProjection(ctx, c.Scope); e == nil {
		out.Projection = &projection
	}
	if err := s.refreshFamilyAvailability(ctx, c.Scope, c.ID); err != nil {
		return FamilyWorkspace{}, err
	}
	err = s.db.View(ctx, func(tx *sql.Tx) error {
		f := &out.Family
		err := tx.QueryRowContext(ctx, `SELECT f.business_family_id,f.label,f.revision,COALESCE(h.document_id,''),COALESCE(h.version_node_id,''),COALESCE(a.status,'')
			FROM ext_business_document_families f LEFT JOIN ext_business_family_published_heads h USING(tenant_id,knowledge_base_id,business_family_id)
			LEFT JOIN ext_document_availability a ON a.tenant_id=h.tenant_id AND a.knowledge_base_id=h.knowledge_base_id AND a.document_id=h.document_id
			WHERE f.tenant_id=? AND f.knowledge_base_id=? AND f.business_family_id=?`, args(c.Scope, c.ID)...).
			Scan(&f.ID, &f.Label, &f.Revision, &f.PublishedDocumentID, &f.PublishedVersionNodeID, &f.PublishedHeadAvailability)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		params := args(c.Scope, c.ID, limit+1, offset)
		switch view {
		case "members", "removed":
			status := "active"
			if view == "removed" {
				status = "removed"
			}
			out.Members, out.HasNext, err = workspacePage(ctx, tx, workspaceMembersSQL, args(c.Scope, c.ID, status, limit+1, offset), limit,
				func(rows *sql.Rows) (WorkspaceMember, error) {
					var item WorkspaceMember
					var stamp sql.NullString
					err := rows.Scan(&item.DocumentID, &item.State, &item.Availability, &stamp, &item.Reason)
					if err == nil && stamp.Valid {
						at, parseErr := time.Parse(time.RFC3339Nano, stamp.String)
						item.RemovedAt, err = &at, parseErr
					}
					return item, err
				})
		case "relations", "history":
			out.Relations, out.HasNext, err = workspacePage(ctx, tx, workspaceRelationsSQL,
				args(c.Scope, c.ID, view == "relations", limit+1, offset), limit, func(rows *sql.Rows) (WorkspaceRelation, error) {
					var item WorkspaceRelation
					err := rows.Scan(&item.ID, &item.DocumentID, &item.TargetDocumentID, &item.EdgeType, &item.Status, &item.Revision,
						&item.Scope, &item.Current, &item.SourceVersionID, &item.TargetVersionID, &item.KnowledgeBaseID, &item.TargetKnowledgeBaseID)
					return item, err
				})
		case "activity":
			out.Activity, out.HasNext, err = workspacePage(ctx, tx, workspaceActivitySQL, params, limit,
				func(rows *sql.Rows) (WorkspaceActivity, error) {
					var item WorkspaceActivity
					var stamp string
					if err := rows.Scan(&item.OperationID, &item.ActorID, &item.Action, &item.AggregateID, &item.Status, &stamp); err != nil {
						return item, err
					}
					at, err := time.Parse(time.RFC3339Nano, stamp)
					item.OccurredAt = at.UTC()
					return item, err
				})
		default:
			return ErrInvalid
		}
		return err
	})
	return out, err
}

func workspacePage[T any](ctx context.Context, tx *sql.Tx, query string, params []any, limit int, scan func(*sql.Rows) (T, error)) ([]T, bool, error) {
	rows, err := tx.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := make([]T, 0, limit)
	for rows.Next() {
		if len(items) == limit {
			return items, true, nil
		}
		item, err := scan(rows)
		if err != nil {
			return nil, false, err
		}
		items = append(items, item)
	}
	return items, false, rows.Err()
}

const workspaceMembersSQL = `SELECT m.document_id,
	CASE WHEN c.state IS NOT NULL AND ` + store.LifecycleCurrentVerifiedSQL + ` THEN c.state ELSE '' END,
	COALESCE(a.status,''),m.removed_at,COALESCE(m.reason,'')
	FROM ext_business_family_member_history m
	LEFT JOIN ext_lifecycle_current c USING(tenant_id,knowledge_base_id,document_id)
	` + store.LifecycleEventJoinSQL + `
	LEFT JOIN ext_document_availability a USING(tenant_id,knowledge_base_id,document_id)
	WHERE m.tenant_id=? AND m.knowledge_base_id=? AND m.business_family_id=? AND m.status=?
	ORDER BY m.removed_at DESC,m.document_id LIMIT ? OFFSET ?`

const workspaceScope = `WITH scope(tenant_id,knowledge_base_id,business_family_id) AS (VALUES(?,?,?)),
	members AS (SELECT document_id,status FROM ext_business_family_member_history JOIN scope USING(tenant_id,knowledge_base_id,business_family_id)),
	records AS (
		SELECT r.relation_id,r.status,r.revision,
			json_extract(r.details,'$.document_id') AS document_id,
			json_extract(r.details,'$.target_document_id') AS target_document_id,
			json_extract(r.details,'$.edge_type') AS edge_type,
			COALESCE(json_extract(r.details,'$.source_version_id'),'') AS source_version_id,
			COALESCE(json_extract(r.details,'$.target_version_id'),'') AS target_version_id,
			CASE WHEN json_type(r.details,'$.pins_current') IS NOT NULL THEN
				r.status IN ('active','awaiting_resolution','resolved') AND json_extract(r.details,'$.pins_current')=1
				ELSE r.status IN ('active','applied','linked') END AS declared_current,
			r.knowledge_base_id,
			COALESCE(json_extract(r.details,'$.target_knowledge_base_id'),r.knowledge_base_id) AS target_knowledge_base_id
		FROM ` + relationRecords + ` r JOIN scope USING(tenant_id,knowledge_base_id)
		UNION ALL
		SELECT r.relation_id,r.status,r.revision,
			json_extract(r.details,'$.document_id'),json_extract(r.details,'$.target_document_id'),json_extract(r.details,'$.edge_type'),
			COALESCE(json_extract(r.details,'$.source_version_id'),''),COALESCE(json_extract(r.details,'$.target_version_id'),''),
			CASE WHEN json_type(r.details,'$.pins_current') IS NOT NULL THEN
				r.status IN ('active','awaiting_resolution','resolved') AND json_extract(r.details,'$.pins_current')=1
				ELSE r.status IN ('active','applied','linked') END,
			r.knowledge_base_id,COALESCE(json_extract(r.details,'$.target_knowledge_base_id'),r.knowledge_base_id)
		FROM ` + relationRecords + ` r JOIN scope s ON r.tenant_id=s.tenant_id AND r.target_knowledge_base_id=s.knowledge_base_id AND r.knowledge_base_id<>s.knowledge_base_id
		UNION ALL
		SELECT 'supplement:'||d.declaration_id||':'||p.parent_document_id,
			CASE WHEN c.declaration_id=d.declaration_id THEN d.status WHEN latest.status='revoked' THEN 'revoked' ELSE 'historical' END,
			d.revision,d.document_id,p.parent_document_id,'supplements',d.child_version_node_id,p.parent_version_node_id,
			(c.declaration_id=d.declaration_id AND d.status='active'),d.knowledge_base_id,d.knowledge_base_id
		FROM ext_supplement_declarations d JOIN scope USING(tenant_id,knowledge_base_id)
		JOIN ext_supplement_parents p USING(tenant_id,knowledge_base_id,document_id,declaration_id)
		LEFT JOIN ext_supplement_current c USING(tenant_id,knowledge_base_id,document_id)
		LEFT JOIN ext_supplement_declarations latest ON latest.tenant_id=d.tenant_id AND latest.knowledge_base_id=d.knowledge_base_id AND latest.document_id=d.document_id AND latest.declaration_id=c.declaration_id
		UNION ALL
		SELECT 'supplement:'||cf.cross_edge_id,
			CASE WHEN c.declaration_id=d.declaration_id THEN d.status WHEN latest.status='revoked' THEN 'revoked' ELSE 'historical' END,
			d.revision,d.document_id,cp.parent_document_id,'supplements',d.child_version_node_id,cp.parent_version_node_id,
			(c.declaration_id=d.declaration_id AND d.status='active'),d.knowledge_base_id,cp.parent_knowledge_base_id
		FROM ext_supplement_cross_parents cp
		JOIN scope s ON cp.tenant_id=s.tenant_id AND cp.knowledge_base_id=s.knowledge_base_id
		JOIN ext_supplement_declarations d ON d.tenant_id=cp.tenant_id AND d.knowledge_base_id=cp.knowledge_base_id AND d.document_id=cp.document_id AND d.declaration_id=cp.declaration_id
		JOIN ext_supplement_cross_edge_facts cf ON cf.declaration_id=cp.declaration_id AND cf.parent_knowledge_base_id=cp.parent_knowledge_base_id AND cf.parent_document_id=cp.parent_document_id
		LEFT JOIN ext_supplement_current c ON c.tenant_id=d.tenant_id AND c.knowledge_base_id=d.knowledge_base_id AND c.document_id=d.document_id
		LEFT JOIN ext_supplement_declarations latest ON latest.tenant_id=d.tenant_id AND latest.knowledge_base_id=d.knowledge_base_id AND latest.document_id=d.document_id AND latest.declaration_id=c.declaration_id
		UNION ALL
		SELECT 'supplement:'||cf.cross_edge_id,
			CASE WHEN c.declaration_id=d.declaration_id THEN d.status WHEN latest.status='revoked' THEN 'revoked' ELSE 'historical' END,
			d.revision,d.document_id,cp.parent_document_id,'supplements',d.child_version_node_id,cp.parent_version_node_id,
			(c.declaration_id=d.declaration_id AND d.status='active'),d.knowledge_base_id,cp.parent_knowledge_base_id
		FROM ext_supplement_cross_parents cp
		JOIN scope s ON cp.tenant_id=s.tenant_id AND cp.parent_knowledge_base_id=s.knowledge_base_id
		JOIN ext_supplement_declarations d ON d.tenant_id=cp.tenant_id AND d.knowledge_base_id=cp.knowledge_base_id AND d.document_id=cp.document_id AND d.declaration_id=cp.declaration_id
		JOIN ext_supplement_cross_edge_facts cf ON cf.declaration_id=cp.declaration_id AND cf.parent_knowledge_base_id=cp.parent_knowledge_base_id AND cf.parent_document_id=cp.parent_document_id
		LEFT JOIN ext_supplement_current c ON c.tenant_id=d.tenant_id AND c.knowledge_base_id=d.knowledge_base_id AND c.document_id=d.document_id
		LEFT JOIN ext_supplement_declarations latest ON latest.tenant_id=d.tenant_id AND latest.knowledge_base_id=d.knowledge_base_id AND latest.document_id=d.document_id AND latest.declaration_id=c.declaration_id
		UNION ALL
		SELECT e.edge_id,'applied',1,n.native_id,m.native_id,e.edge_type,e.from_node_id,e.to_node_id,1,scope.knowledge_base_id,scope.knowledge_base_id
		FROM (SELECT edge_id,tenant_id,knowledge_base_id,edge_type,from_node_id,to_node_id FROM ext_lineage_edge
			WHERE edge_type IN ('supersedes','merged_into')
			UNION ALL SELECT edge_id,tenant_id,knowledge_base_id,edge_type,from_node_id,to_node_id FROM ext_revision_split_ancestry) e
		JOIN scope USING(tenant_id,knowledge_base_id)
		JOIN ext_lineage_node n ON n.tenant_id=e.tenant_id AND n.knowledge_base_id=e.knowledge_base_id AND n.node_id=e.from_node_id
		JOIN ext_lineage_node m ON m.tenant_id=e.tenant_id AND m.knowledge_base_id=e.knowledge_base_id AND m.node_id=e.to_node_id
	), incident AS (
		SELECT r.*,CASE WHEN a.status='active' AND b.status='active' THEN 'internal' ELSE 'external' END AS relation_scope,
			COALESCE(declared_current AND (a.status='active' OR b.status='active') AND
				(edge_type IN ('supersedes','merged_into','split_from','repeals','attachment_of') OR
				(source_version_id=f.current_version_node_id AND target_version_id=g.current_version_node_id)),0) AS current
		FROM records r CROSS JOIN scope
		LEFT JOIN members a ON a.document_id=r.document_id
		LEFT JOIN members b ON b.document_id=r.target_document_id
		LEFT JOIN ext_document_version_family f ON f.tenant_id=scope.tenant_id AND f.knowledge_base_id=r.knowledge_base_id AND f.document_id=r.document_id
		LEFT JOIN ext_document_version_family g ON g.tenant_id=scope.tenant_id AND g.knowledge_base_id=r.target_knowledge_base_id AND g.document_id=r.target_document_id
		WHERE a.document_id IS NOT NULL OR b.document_id IS NOT NULL
	)`

const workspaceRelationsSQL = workspaceScope + `
	SELECT relation_id,document_id,target_document_id,edge_type,status,revision,relation_scope,current,source_version_id,target_version_id,knowledge_base_id,target_knowledge_base_id
	FROM incident WHERE current=? ORDER BY edge_type,document_id,target_document_id,relation_id LIMIT ? OFFSET ?`

const workspaceActivitySQL = workspaceScope + `
	SELECT operation_id,actor_id,action,aggregate_id,status,occurred_at FROM (
		SELECT a.operation_id,a.actor_id,a.action,a.aggregate_id,a.status,a.occurred_at,a.sequence
		FROM ext_business_audit a JOIN scope USING(tenant_id,knowledge_base_id)
		WHERE (a.action IN ('family_create','family_member_add','family_member_remove','family_backfill_apply','family_suggestion_scan','family_replace_history') AND a.aggregate_id=scope.business_family_id)
			OR (a.action IN ('family_suggestion_confirm','family_suggestion_ignore') AND a.aggregate_id IN (
				SELECT s.suggestion_id FROM ext_family_suggestions s JOIN scope USING(tenant_id,knowledge_base_id,business_family_id)))
			OR (a.action GLOB 'family_replace_*' AND a.aggregate_id IN (
				SELECT r.replacement_id FROM ext_family_replacements r JOIN scope USING(tenant_id,knowledge_base_id,business_family_id)))
			OR ((a.action GLOB 'relation_*' OR a.action GLOB 'repeal_*' OR a.action GLOB 'clause_*') AND a.aggregate_id IN (SELECT relation_id FROM incident))
			OR (a.action GLOB 'revision_*' AND a.aggregate_id IN (
				SELECT r.proposal_id FROM ext_revision_proposals r JOIN scope USING(tenant_id,knowledge_base_id)
				WHERE r.source_document_id IN (SELECT document_id FROM members) OR r.target_document_id IN (SELECT document_id FROM members)))
			OR (a.action GLOB 'split_*' AND a.aggregate_id IN (
				SELECT b.batch_id FROM ext_revision_split_batches b JOIN scope USING(tenant_id,knowledge_base_id)
				WHERE b.source_document_id IN (SELECT document_id FROM members)))
		UNION ALL
		SELECT d.operation_id,a.actor_id,'supplements_'||a.status,a.document_id,a.status,a.occurred_at,a.sequence
		FROM ext_supplement_audit a JOIN scope USING(tenant_id,knowledge_base_id)
		JOIN ext_supplement_declarations d USING(tenant_id,knowledge_base_id,document_id,declaration_id)
		WHERE a.document_id IN (SELECT document_id FROM members)
			OR a.document_id IN (SELECT document_id FROM incident WHERE edge_type='supplements')
	) ORDER BY occurred_at DESC,sequence DESC,operation_id LIMIT ? OFFSET ?`
