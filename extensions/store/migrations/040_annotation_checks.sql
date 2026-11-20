-- Keep recent reviewer diagnostics separate from the annotation and its source evidence.
CREATE TABLE ext_annotation_checks (
 sequence INTEGER PRIMARY KEY,
 tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, run_id TEXT NOT NULL, step_id TEXT NOT NULL,
 call_id TEXT NOT NULL CHECK(length(call_id)=64), checked_at TEXT NOT NULL,
 matches_json TEXT NOT NULL CHECK(json_valid(matches_json) AND json_type(matches_json)='object'),
 delivery TEXT NOT NULL DEFAULT '' CHECK(delivery IN ('','applied','unavailable')),
 UNIQUE(tenant_id,knowledge_base_id,run_id,step_id),
 FOREIGN KEY(tenant_id,knowledge_base_id,run_id,step_id) REFERENCES ext_provenance_steps(tenant_id,knowledge_base_id,run_id,step_id) ON DELETE CASCADE
);
CREATE INDEX ext_annotation_checks_scope ON ext_annotation_checks(tenant_id,knowledge_base_id,sequence DESC);
