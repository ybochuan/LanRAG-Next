package parser

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// ErrRenditionEvidence identifies an absent or unverifiable durable parse result.
var ErrRenditionEvidence = errors.New("parser rendition evidence unavailable")

// RenditionArtifact contains a verified result and the fingerprint of its parser input.
type RenditionArtifact struct {
	SourceSHA256 string
	SHA256       string
	Body         []byte
}

// ReadRenditionTx checks the committed job, scoped input/output ownership and bytes.
// It never infers a source from the native document ID alone.
func ReadRenditionTx(ctx context.Context, tx *sql.Tx, tenant uint64, kb, doc, identity string) (RenditionArtifact, error) {
	var out RenditionArtifact
	var source, jobKey, version string
	var snapshot, receipt []byte
	err := tx.QueryRowContext(ctx, `SELECT a.source_file_id,a.job_key,a.mineru_version,a.sha256,a.middle_json,d.snapshot,j.receipt
		FROM ext_parser_artifacts a JOIN ext_parser_documents d USING(identity_key,tenant_id,knowledge_base_id,doc_id)
		JOIN ext_parser_jobs j USING(identity_key,job_key)
		WHERE a.identity_key=? AND a.tenant_id=? AND a.knowledge_base_id=? AND a.doc_id=?
		AND a.artifact_format='middle_json' AND j.status BETWEEN 200 AND 299`, identity, tenant, kb, doc).
		Scan(&source, &jobKey, &version, &out.SHA256, &out.Body, &snapshot, &receipt)
	if errors.Is(err, sql.ErrNoRows) {
		return RenditionArtifact{}, ErrRenditionEvidence
	}
	if err != nil {
		return RenditionArtifact{}, err
	}
	var state documentSnapshot
	var job struct {
		ID string `json:"job_id"`
	}
	if len(out.Body) == 0 || len(out.Body) > middleJSONLimit || hexHash(out.Body) != out.SHA256 ||
		json.Unmarshal(snapshot, &state) != nil || state.Version != 1 ||
		json.Unmarshal(receipt, &job) != nil || !safeID(job.ID) || !state.Jobs[job.ID] || state.JobKeys[job.ID] != jobKey ||
		!state.Files[source] || state.Documents[source] != doc {
		return RenditionArtifact{}, ErrRenditionEvidence
	}
	out.SourceSHA256 = state.Hashes[source]
	if decoded, err := hex.DecodeString(out.SourceSHA256); err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != out.SourceSHA256 {
		return RenditionArtifact{}, ErrRenditionEvidence
	}
	verified := false
	for _, upload := range state.Uploads {
		if upload.BackendFile != source || !upload.Verified {
			continue
		}
		if upload.Hash != out.SourceSHA256 {
			return RenditionArtifact{}, ErrRenditionEvidence
		}
		verified = true
	}
	if !verified {
		return RenditionArtifact{}, ErrRenditionEvidence
	}
	owner := ScopeIdentity{TenantID: tenant, KnowledgeBaseID: kb}
	for id, ref := range state.OutputDocuments {
		isInput, known := state.Files[id]
		if known && !isInput && ref.ScopeIdentity == owner && ref.DocumentID == doc && ref.SourceFileID == source &&
			ref.MinerUVersion == version && state.OutputJobKeys[id] == jobKey {
			return out, nil
		}
	}
	return RenditionArtifact{}, ErrRenditionEvidence
}

func hexHash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
