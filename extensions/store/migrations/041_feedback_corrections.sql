-- Published fact corrections own their minimum anchor independent of rolling run evidence.
-- Version IDs are retained as historical identities even when existing cleanup
-- removes source metadata. Online reads still require a current verified version.
CREATE TABLE ext_feedback_corrections (
 tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, feedback_id TEXT NOT NULL,
 document_id TEXT NOT NULL, version_id TEXT NOT NULL, chunk_id TEXT NOT NULL,
 chunk_index INTEGER NOT NULL CHECK(chunk_index>=0), content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
 correction TEXT NOT NULL CHECK(length(trim(correction))>0),
 applies_when TEXT NOT NULL CHECK(length(trim(applies_when))>0),
 approved_at TEXT NOT NULL, effective_at TEXT NOT NULL,
 superseded_by TEXT, PRIMARY KEY(tenant_id,knowledge_base_id,feedback_id),
 FOREIGN KEY(tenant_id,knowledge_base_id,feedback_id) REFERENCES ext_qa_feedback(tenant_id,knowledge_base_id,feedback_id),
 FOREIGN KEY(tenant_id,knowledge_base_id,superseded_by) REFERENCES ext_feedback_corrections(tenant_id,knowledge_base_id,feedback_id)
);
CREATE INDEX ext_feedback_corrections_version ON ext_feedback_corrections(tenant_id,knowledge_base_id,document_id,version_id,chunk_id);

CREATE TABLE ext_feedback_correction_replacements (
 tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, feedback_id TEXT NOT NULL,
 previous_id TEXT NOT NULL CHECK(previous_id<>feedback_id), previous_revision INTEGER NOT NULL CHECK(previous_revision>0),
 PRIMARY KEY(tenant_id,knowledge_base_id,feedback_id),
 FOREIGN KEY(tenant_id,knowledge_base_id,feedback_id) REFERENCES ext_qa_feedback(tenant_id,knowledge_base_id,feedback_id),
 FOREIGN KEY(tenant_id,knowledge_base_id,previous_id) REFERENCES ext_feedback_corrections(tenant_id,knowledge_base_id,feedback_id)
);

-- A coalesced work item per source version records publication changes and retries.
CREATE TABLE ext_feedback_correction_projections (
 tenant_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL, document_id TEXT NOT NULL, version_id TEXT NOT NULL,
 revision INTEGER NOT NULL DEFAULT 1, rendered_revision INTEGER NOT NULL DEFAULT 0,
 last_failure_at TEXT,
 PRIMARY KEY(tenant_id,knowledge_base_id,document_id,version_id)
);
CREATE TRIGGER ext_feedback_correction_project_insert AFTER INSERT ON ext_feedback_corrections
BEGIN
 INSERT INTO ext_feedback_correction_projections(tenant_id,knowledge_base_id,document_id,version_id)
 VALUES(NEW.tenant_id,NEW.knowledge_base_id,NEW.document_id,NEW.version_id)
 ON CONFLICT(tenant_id,knowledge_base_id,document_id,version_id) DO UPDATE SET revision=revision+1;
END;
CREATE TRIGGER ext_feedback_correction_project_status AFTER UPDATE OF status ON ext_feedback_staging
WHEN NEW.status<>OLD.status
BEGIN
 UPDATE ext_feedback_correction_projections SET revision=revision+1
 WHERE (tenant_id,knowledge_base_id,document_id,version_id) IN (
  SELECT tenant_id,knowledge_base_id,document_id,version_id FROM ext_feedback_corrections
  WHERE (tenant_id,knowledge_base_id,feedback_id)=(NEW.tenant_id,NEW.knowledge_base_id,NEW.feedback_id));
END;

-- Existing rows qualify only if still active and the exact current pin can be
-- re-established from the independently verified source snapshot. The approved
-- timestamp was recorded in the existing event at reviewer approval.
INSERT INTO ext_feedback_corrections
 (tenant_id,knowledge_base_id,feedback_id,document_id,version_id,chunk_id,chunk_index,
  content_hash,correction,applies_when,approved_at,effective_at)
SELECT q.tenant_id,q.knowledge_base_id,q.feedback_id,v.document_id,v.version_node_id,
 json_extract(q.feedback_anchor,'$.chunk_id'),json_extract(q.feedback_anchor,'$.chunk_index'),
 json_extract(q.feedback_anchor,'$.content_hash'),q.reviewed_guidance,q.applies_when,ev.occurred_at,g.created_at
FROM ext_qa_feedback q
JOIN ext_feedback_staging s USING(tenant_id,knowledge_base_id,feedback_id)
JOIN ext_feedback_guidances g ON (g.tenant_id,g.knowledge_base_id,g.feedback_id)=(q.tenant_id,q.knowledge_base_id,q.feedback_id)
 AND g.guidance_id=q.feedback_id AND g.status='active'
JOIN ext_feedback_events ev ON (ev.tenant_id,ev.knowledge_base_id,ev.event_id)=(q.tenant_id,q.knowledge_base_id,q.feedback_id)
JOIN ext_document_version_family f ON (f.tenant_id,f.knowledge_base_id,f.document_id)=(q.tenant_id,q.knowledge_base_id,json_extract(q.feedback_anchor,'$.document_id'))
JOIN ext_document_version v ON (v.tenant_id,v.knowledge_base_id,v.document_id,v.version_node_id)=(f.tenant_id,f.knowledge_base_id,f.document_id,f.current_version_node_id)
 AND v.version_node_id=json_extract(q.feedback_anchor,'$.version_id')
JOIN ext_lifecycle_current c ON (c.tenant_id,c.knowledge_base_id,c.document_id)=(f.tenant_id,f.knowledge_base_id,f.document_id)
JOIN ext_lifecycle_events e ON (e.tenant_id,e.knowledge_base_id,e.document_id,e.event_id)=(c.tenant_id,c.knowledge_base_id,c.document_id,c.last_event_id)
LEFT JOIN ext_document_annotations a ON (a.tenant_id,a.knowledge_base_id,a.document_id)=(c.tenant_id,c.knowledge_base_id,c.document_id)
JOIN ext_source_snapshot_evidence ss ON (ss.tenant_id,ss.knowledge_base_id,ss.version_node_id)=(v.tenant_id,v.knowledge_base_id,v.version_node_id)
WHERE s.status='active' AND q.status='approved' AND q.rating IN ('correction','supplement','dislike')
 AND c.state='active' AND e.to_state=c.state
 AND NOT EXISTS (SELECT 1 FROM ext_lifecycle_events newer WHERE (newer.tenant_id,newer.knowledge_base_id,newer.document_id)=(c.tenant_id,c.knowledge_base_id,c.document_id) AND newer.sequence>e.sequence)
 AND NOT EXISTS (SELECT 1 FROM ext_document_availability n WHERE (n.tenant_id,n.knowledge_base_id,n.document_id)=(c.tenant_id,c.knowledge_base_id,c.document_id) AND n.status<>'active')
 AND (a.effective_from IS NULL OR julianday(a.effective_from)<=julianday('now'))
 AND (a.effective_until IS NULL OR julianday(a.effective_until)>=julianday('now'))
 AND length(trim(q.reviewed_guidance))>0 AND length(trim(q.applies_when))>0
 AND length(json_extract(q.feedback_anchor,'$.chunk_id'))>0
 AND length(json_extract(q.feedback_anchor,'$.content_hash'))=64
 AND json_type(q.feedback_anchor,'$.chunk_index')='integer' AND json_extract(q.feedback_anchor,'$.chunk_index')>=0
 AND EXISTS (SELECT 1 FROM json_each(ss.snapshot_json,'$.chunks') leaf
 WHERE json_extract(leaf.value,'$.id')=json_extract(q.feedback_anchor,'$.chunk_id')
   AND json_extract(leaf.value,'$.content_hash')=json_extract(q.feedback_anchor,'$.content_hash')
   AND json_extract(leaf.value,'$.chunk_index')=json_extract(q.feedback_anchor,'$.chunk_index'));
