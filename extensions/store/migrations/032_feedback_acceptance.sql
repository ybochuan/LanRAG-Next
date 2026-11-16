CREATE TABLE ext_feedback_sample_reviews (
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, feedback_id TEXT NOT NULL,
    decision TEXT NOT NULL CHECK(decision IN ('pending','passed','failed')),
    reviewer TEXT NOT NULL DEFAULT '', note TEXT NOT NULL DEFAULT '',
    selected_at TEXT NOT NULL, reviewed_at TEXT,
    PRIMARY KEY(tenant_id,knowledge_base_id,feedback_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,feedback_id)
        REFERENCES ext_feedback_staging(tenant_id,knowledge_base_id,feedback_id)
);
CREATE INDEX ext_feedback_sample_reviews_pending
    ON ext_feedback_sample_reviews(tenant_id,knowledge_base_id,selected_at)
    WHERE decision='pending';

CREATE TABLE ext_feedback_topic_recoveries (
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, topic TEXT NOT NULL,
    recovery_id TEXT NOT NULL, reviewer TEXT NOT NULL, note TEXT NOT NULL DEFAULT '',
    recovered_at TEXT NOT NULL,
    PRIMARY KEY(tenant_id,knowledge_base_id,topic,recovery_id)
);
CREATE INDEX ext_feedback_topic_recoveries_latest
    ON ext_feedback_topic_recoveries(tenant_id,knowledge_base_id,topic,recovered_at);
