-- Issued additions consume budget even if delivery becomes unknown.
CREATE TABLE ext_guidance_calls (
 tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, run_id TEXT NOT NULL,
 call_id TEXT NOT NULL, user_id TEXT NOT NULL, session_id TEXT NOT NULL,
 units INTEGER NOT NULL CHECK(units BETWEEN 0 AND 800),
 selection_json TEXT NOT NULL CHECK(json_valid(selection_json)),
 PRIMARY KEY(tenant_id,knowledge_base_id,run_id,call_id),
 FOREIGN KEY(tenant_id,knowledge_base_id,run_id) REFERENCES ext_provenance_runs(tenant_id,knowledge_base_id,run_id)
);
CREATE INDEX ext_guidance_session_budget ON ext_guidance_calls(tenant_id,user_id,session_id);
CREATE TABLE ext_guidance_call_items (
 tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, run_id TEXT NOT NULL, call_id TEXT NOT NULL,
 guidance_id TEXT NOT NULL, guidance_revision INTEGER NOT NULL CHECK(guidance_revision>0),
 PRIMARY KEY(tenant_id,knowledge_base_id,run_id,call_id,guidance_id),
 FOREIGN KEY(tenant_id,knowledge_base_id,run_id,call_id) REFERENCES ext_guidance_calls(tenant_id,knowledge_base_id,run_id,call_id),
 FOREIGN KEY(tenant_id,knowledge_base_id,guidance_id) REFERENCES ext_feedback_guidances(tenant_id,knowledge_base_id,guidance_id)
);
CREATE TABLE ext_guidance_step_inputs (
 tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, run_id TEXT NOT NULL, step_id TEXT NOT NULL,
 call_id TEXT NOT NULL, content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
 PRIMARY KEY(tenant_id,knowledge_base_id,run_id,step_id),
 FOREIGN KEY(tenant_id,knowledge_base_id,run_id,step_id) REFERENCES ext_provenance_steps(tenant_id,knowledge_base_id,run_id,step_id),
 FOREIGN KEY(tenant_id,knowledge_base_id,run_id,call_id) REFERENCES ext_guidance_calls(tenant_id,knowledge_base_id,run_id,call_id)
);
