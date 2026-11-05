ALTER TABLE ext_provenance_inputs ADD COLUMN retrieved_chunk_id TEXT NOT NULL DEFAULT '';

CREATE INDEX ext_provenance_runs_by_answer ON ext_provenance_runs (
    tenant_id, knowledge_base_id,
    json_extract(payload_json, '$.identity.user_id'),
    json_extract(payload_json, '$.identity.session_id'),
    json_extract(payload_json, '$.identity.attempt_id')
);

CREATE TABLE ext_feedback_qa_bindings (
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL,
    feedback_id TEXT NOT NULL, run_id TEXT NOT NULL,
    retrieved_chunk_ids TEXT NOT NULL CHECK(json_valid(retrieved_chunk_ids) AND json_type(retrieved_chunk_ids)='array' AND json_array_length(retrieved_chunk_ids)>0),
    PRIMARY KEY(tenant_id,knowledge_base_id,feedback_id),
    UNIQUE(tenant_id,knowledge_base_id,run_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,feedback_id) REFERENCES ext_feedback_events(tenant_id,knowledge_base_id,event_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,run_id) REFERENCES ext_provenance_runs(tenant_id,knowledge_base_id,run_id)
);
