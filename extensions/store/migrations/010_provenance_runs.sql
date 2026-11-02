-- Synchronous server-only run facts. No native business tables are changed.
CREATE TABLE ext_provenance_runs (
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, run_id TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK(revision>0),
    state TEXT NOT NULL CHECK(state IN ('open','sealed','unattributed','aborted')),
    capability_hash TEXT NOT NULL CHECK(length(capability_hash)=64),
    payload_json TEXT NOT NULL CHECK(json_valid(payload_json)),
    PRIMARY KEY(tenant_id,knowledge_base_id,run_id)
);
CREATE TABLE ext_provenance_steps (
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, run_id TEXT NOT NULL, step_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK(kind IN ('model','tool','write')),
    state TEXT NOT NULL CHECK(state IN ('open','success','unknown','error')),
    input_hash TEXT NOT NULL DEFAULT '', output_hash TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(tenant_id,knowledge_base_id,run_id,step_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,run_id) REFERENCES ext_provenance_runs(tenant_id,knowledge_base_id,run_id)
);
CREATE TABLE ext_provenance_inputs (
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, run_id TEXT NOT NULL, step_id TEXT NOT NULL,
    operation_id TEXT NOT NULL, evidence_hash TEXT NOT NULL CHECK(length(evidence_hash)=64),
    refs_json TEXT NOT NULL CHECK(json_valid(refs_json)),
    PRIMARY KEY(tenant_id,knowledge_base_id,run_id,operation_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,run_id,step_id) REFERENCES ext_provenance_steps(tenant_id,knowledge_base_id,run_id,step_id)
);
CREATE TABLE ext_provenance_input_intents (
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, run_id TEXT NOT NULL, step_id TEXT NOT NULL,
    operation_id TEXT NOT NULL, payload_hash TEXT NOT NULL CHECK(length(payload_hash)=64),
    state TEXT NOT NULL CHECK(state IN ('pending','verified','rejected')),
    PRIMARY KEY(tenant_id,knowledge_base_id,run_id,operation_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,run_id,step_id) REFERENCES ext_provenance_steps(tenant_id,knowledge_base_id,run_id,step_id)
);
CREATE TABLE ext_provenance_producers (
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, run_id TEXT NOT NULL, step_id TEXT NOT NULL,
    producer_id TEXT NOT NULL, state TEXT NOT NULL CHECK(state IN ('reserved','committed')),
    payload_json TEXT NOT NULL CHECK(json_valid(payload_json)),
    PRIMARY KEY(tenant_id,knowledge_base_id,producer_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,run_id,step_id) REFERENCES ext_provenance_steps(tenant_id,knowledge_base_id,run_id,step_id)
);
CREATE TABLE ext_provenance_run_receipts (
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, operation_id TEXT NOT NULL,
    run_id TEXT NOT NULL, kind TEXT NOT NULL, payload_hash TEXT NOT NULL CHECK(length(payload_hash)=64),
    result_json TEXT NOT NULL CHECK(json_valid(result_json)),
    PRIMARY KEY(tenant_id,knowledge_base_id,operation_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,run_id) REFERENCES ext_provenance_runs(tenant_id,knowledge_base_id,run_id)
);
CREATE TABLE ext_provenance_run_audit (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, operation_id TEXT NOT NULL,
    run_id TEXT NOT NULL, revision INTEGER NOT NULL CHECK(revision>0), state TEXT NOT NULL, occurred_at TEXT NOT NULL,
    UNIQUE(tenant_id,knowledge_base_id,operation_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,operation_id) REFERENCES ext_provenance_run_receipts(tenant_id,knowledge_base_id,operation_id),
    FOREIGN KEY(tenant_id,knowledge_base_id,run_id) REFERENCES ext_provenance_runs(tenant_id,knowledge_base_id,run_id)
);
