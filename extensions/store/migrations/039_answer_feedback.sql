-- Keep review history independent of rolling run and source snapshot evidence.
CREATE TABLE ext_qa_feedback (
 tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, feedback_id TEXT NOT NULL,
 submitted_at TEXT NOT NULL, rating TEXT NOT NULL CHECK(rating IN ('like','dislike','supplement','correction')),
 reason TEXT NOT NULL, text TEXT NOT NULL,
 submitted_context TEXT NOT NULL CHECK(json_valid(submitted_context) AND json_type(submitted_context)='object'),
 citation_chunks TEXT NOT NULL CHECK(json_valid(citation_chunks) AND json_type(citation_chunks)='array'),
 feedback_anchor TEXT CHECK(feedback_anchor IS NULL OR (knowledge_base_id<>'' AND json_valid(feedback_anchor) AND json_type(feedback_anchor)='object')),
 applies_when TEXT NOT NULL DEFAULT '', reviewed_guidance TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL CHECK(status IN ('received','pending_review','record_only','needs_evidence','rejected','approved')),
 revision INTEGER NOT NULL CHECK(revision>0),
 PRIMARY KEY(tenant_id,knowledge_base_id,feedback_id),
 CHECK(json_extract(submitted_context,'$.snapshot_restricted') IS NOT 1 OR
   (knowledge_base_id='' AND text='' AND json_extract(submitted_context,'$.question') IS NULL
    AND json_extract(submitted_context,'$.answer_excerpt') IS NULL AND json_array_length(citation_chunks)=0))
);

CREATE INDEX ext_qa_feedback_recent ON ext_qa_feedback (tenant_id,knowledge_base_id,submitted_at DESC,feedback_id DESC);
CREATE INDEX ext_provenance_runs_by_owner_answer ON ext_provenance_runs (
 tenant_id, json_extract(payload_json,'$.identity.user_id'),
 json_extract(payload_json,'$.identity.session_id'), json_extract(payload_json,'$.identity.attempt_id')
);
