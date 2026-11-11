CREATE TABLE ext_feedback_review_context (
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, feedback_id TEXT NOT NULL,
    submitted_context TEXT NOT NULL CHECK(json_valid(submitted_context)),
    reviewed_guidance TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(tenant_id,knowledge_base_id,feedback_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,feedback_id)
        REFERENCES ext_feedback_qa_bindings(tenant_id,knowledge_base_id,feedback_id)
);
