package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"

	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

// NativePublicationTargetPendingSQL identifies a bound but unreleased native sidecar.
// The alias c is the scoped lifecycle-current row under consideration.
const NativePublicationTargetPendingSQL = `EXISTS(
	SELECT 1 FROM ext_revision_proposals r LEFT JOIN ext_revision_split_parts p USING(tenant_id,knowledge_base_id,proposal_id)
	WHERE r.tenant_id=c.tenant_id AND r.knowledge_base_id=c.knowledge_base_id AND r.target_document_id=c.document_id
	AND (r.status NOT IN ('published','superseded') OR (p.proposal_id IS NOT NULL AND p.target_version_id IS NULL))
)`

// PublicationTargetPendingSQL also holds the existing new member of a scheduled cutover.
const PublicationTargetPendingSQL = `(` + NativePublicationTargetPendingSQL + ` OR EXISTS(
	SELECT 1 FROM ext_family_replacements f WHERE f.tenant_id=c.tenant_id AND f.knowledge_base_id=c.knowledge_base_id
	AND f.new_document_id=c.document_id AND f.status='scheduled'
))`

// FamilyReplacementDueSQL identifies the old member of a scheduled family
// replacement after its effective time has arrived. It takes the caller's
// clock as its single parameter (Unix milliseconds) so governance code can
// share the service test clock instead of relying on the database clock.
const FamilyReplacementDueSQL = `EXISTS(
	SELECT 1 FROM ext_family_replacements f WHERE f.tenant_id=c.tenant_id AND f.knowledge_base_id=c.knowledge_base_id
	AND f.old_document_id=c.document_id AND f.status='scheduled'
	AND f.effective_at_ms<=?
)`

// PublishedPublicationChannelSQL returns the native channel for the bound target
// at the scoped lifecycle-current row c, or NULL before release.
const PublishedPublicationChannelSQL = `(
	SELECT (CASE WHEN p.proposal_id IS NULL THEN '` + weknora.RevisionPublicationChannelPrefix + `' ELSE '` + weknora.SplitPublicationChannelPrefix + `' END) || r.proposal_id
	FROM ext_revision_proposals r LEFT JOIN ext_revision_split_parts p USING(tenant_id,knowledge_base_id,proposal_id)
	WHERE r.tenant_id=c.tenant_id AND r.knowledge_base_id=c.knowledge_base_id AND r.target_document_id=c.document_id
	AND r.status IN ('published','superseded') AND (p.proposal_id IS NULL OR p.target_version_id IS NOT NULL)
)`

// LegacyPublicationUnreleasedSQL is the release check for a proposal already
// matched by the pre-draft publication title. The aliases are r (proposal) and
// c (lifecycle current).
const LegacyPublicationUnreleasedSQL = `(r.target_document_id<>c.document_id OR r.status NOT IN ('published','superseded'))`

// LegacyPublicationHeldTx fences the old api upload after a lost create ACK,
// even when its target ID has not yet been recorded.
func LegacyPublicationHeldTx(ctx context.Context, tx *sql.Tx, doc weknora.AttachmentDocument) (bool, error) {
	key, marked := strings.CutPrefix(doc.Title, weknora.RevisionPublicationTitlePrefix)
	if doc.Channel != "api" || !marked || len(key) != 64 {
		return false, nil
	}
	var held bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM ext_lifecycle_current c JOIN ext_revision_proposals r USING(tenant_id,knowledge_base_id)
		WHERE c.tenant_id=? AND c.knowledge_base_id=? AND c.document_id=? AND r.publication_key=?
		AND `+LegacyPublicationUnreleasedSQL+`)`,
		strconv.FormatUint(doc.TenantID, 10), doc.KnowledgeBaseID, doc.ID, key).Scan(&held)
	return held, err
}
