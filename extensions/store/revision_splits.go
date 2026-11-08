package store

// SplitTargetPendingSQL identifies a bound but unreleased split target.
// The alias c is the scoped lifecycle-current row under consideration.
const SplitTargetPendingSQL = `EXISTS(
	SELECT 1 FROM ext_revision_proposals r JOIN ext_revision_split_parts p USING(tenant_id,knowledge_base_id,proposal_id)
	WHERE r.tenant_id=c.tenant_id AND r.knowledge_base_id=c.knowledge_base_id AND r.target_document_id=c.document_id
	AND (r.status NOT IN ('published','superseded') OR p.target_version_id IS NULL)
)`

// PublishedSplitProposalSQL returns the proposal receipt for the bound target
// at the scoped lifecycle-current row c, or NULL before release.
const PublishedSplitProposalSQL = `(
	SELECT r.proposal_id FROM ext_revision_proposals r JOIN ext_revision_split_parts p USING(tenant_id,knowledge_base_id,proposal_id)
	WHERE r.tenant_id=c.tenant_id AND r.knowledge_base_id=c.knowledge_base_id AND r.target_document_id=c.document_id
	AND r.status IN ('published','superseded') AND p.target_version_id IS NOT NULL
)`
