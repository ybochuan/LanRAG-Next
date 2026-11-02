CREATE TABLE ext_parser_documents (
 identity_key TEXT PRIMARY KEY,
 tenant_id INTEGER NOT NULL,
 knowledge_base_id TEXT NOT NULL,
 doc_id TEXT NOT NULL,
 snapshot BLOB NOT NULL,
 updated_at TEXT NOT NULL,
 UNIQUE(identity_key,tenant_id,knowledge_base_id,doc_id)
);
CREATE TABLE ext_parser_jobs (
 identity_key TEXT NOT NULL,
 job_key TEXT NOT NULL,
 status INTEGER NOT NULL CHECK(status=0 OR status BETWEEN 200 AND 299),
 receipt BLOB,
 PRIMARY KEY(identity_key, job_key),
 FOREIGN KEY(identity_key) REFERENCES ext_parser_documents(identity_key)
);
CREATE TABLE ext_parser_artifacts (
 identity_key TEXT PRIMARY KEY,
 job_key TEXT NOT NULL,
 tenant_id INTEGER NOT NULL,
 knowledge_base_id TEXT NOT NULL,
 doc_id TEXT NOT NULL,
 source_file_id TEXT NOT NULL,
 artifact_format TEXT NOT NULL CHECK(artifact_format='middle_json'),
 artifact_schema_version TEXT NOT NULL,
 mineru_version TEXT NOT NULL,
 sha256 TEXT NOT NULL,
 middle_json BLOB NOT NULL,
 committed_at TEXT NOT NULL,
 FOREIGN KEY(identity_key, job_key) REFERENCES ext_parser_jobs(identity_key, job_key),
 FOREIGN KEY(identity_key,tenant_id,knowledge_base_id,doc_id) REFERENCES ext_parser_documents(identity_key,tenant_id,knowledge_base_id,doc_id)
);
