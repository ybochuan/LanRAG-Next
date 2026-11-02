package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

type jobRequest struct {
	Files []struct {
		Source struct {
			Type   string `json:"type"`
			FileID string `json:"file_id"`
		} `json:"source"`
	} `json:"files"`
	OutputFormats []string `json:"output_formats"`
	Tier          string   `json:"tier,omitempty"`
	OCRMode       string   `json:"ocr_mode,omitempty"`
}

func (p *ScopeProxy) createJob(w http.ResponseWriter, r *http.Request, s *scopeState) {
	var in jobRequest
	if !readProxyJSON(w, r, &in) {
		return
	}
	validTier := in.Tier == "" || in.Tier == "flash" || in.Tier == "basic" || in.Tier == "standard" || in.Tier == "advanced"
	if !validTier || len(in.Files) != 1 || in.Files[0].Source.Type != "file_id" || !safeID(in.Files[0].Source.FileID) || len(in.OutputFormats) != 1 || in.OutputFormats[0] != "zip" || (in.OCRMode != "" && in.OCRMode != "auto" && in.OCRMode != "txt" && in.OCRMode != "ocr") {
		proxyError(w, 400, "invalid_job")
		return
	}
	s.mu.Lock()
	if !s.files[in.Files[0].Source.FileID] {
		s.mu.Unlock()
		proxyError(w, 403, "scope_denied")
		return
	}
	if report, pdf := s.pdfs[in.Files[0].Source.FileID]; pdf {
		decision, err := DecidePDF(report)
		if err != nil {
			s.mu.Unlock()
			proxyError(w, 422, "pdf_requires_review")
			return
		}
		in.Tier, in.OCRMode = string(decision.Tier), decision.OCRMode
	}
	document := s.documents[in.Files[0].Source.FileID]
	if p.artifacts != nil && document == "" {
		s.mu.Unlock()
		proxyError(w, 409, "document_binding_required")
		return
	}
	// Deduplicate the effective policy, not caller auto/ocr/tier preferences.
	b, _ := json.Marshal(in)
	canonical := in
	// Do not mutate the shared Files slice: the forwarded request still needs
	// the backend's current file ID, whereas deduplication needs proven bytes.
	canonical.Files = append(canonical.Files[:0:0], in.Files...)
	if hash := s.hashes[in.Files[0].Source.FileID]; hash != "" {
		canonical.Files[0].Source.FileID = hash
	}
	identityBytes, _ := json.Marshal([]any{1, p.upstream, s.identity, document, canonical})
	key := sha256.Sum256(identityBytes)
	if old := s.requests[key]; old != nil {
		busy, status, body := old.busy, old.status, old.body
		s.mu.Unlock()
		if busy {
			proxyError(w, 409, "job_busy")
		} else if status == 0 {
			proxyError(w, 503, "job_outcome_unknown")
		} else {
			proxyBytes(w, status, body, "application/json")
		}
		return
	}
	if len(s.requests) >= proxyStateLimit {
		s.mu.Unlock()
		proxyError(w, 503, "capacity_reached")
		return
	}
	receipt := &jobReceipt{busy: true}
	s.requests[key] = receipt
	s.mu.Unlock()
	if p.journal != nil {
		if err := p.journal.claim(r.Context(), s, key); err != nil {
			s.mu.Lock()
			receipt.busy = false
			s.mu.Unlock()
			proxyError(w, 503, "job_intent_unavailable")
			return
		}
	}
	status, body, err := p.forward(r, "POST", "/v1/parse/jobs", b, false)
	var result map[string]any
	valid := err == nil && status >= 200 && status < 300 && json.Unmarshal(body, &result) == nil
	id, _ := result["job_id"].(string)
	valid = valid && safeID(id)
	if valid {
		maskJobErrors(result)
		body, _ = json.Marshal(result)
	}
	s.mu.Lock()
	receipt.busy = false
	if valid {
		receipt.status = status
		receipt.body = body
		s.jobs[id] = true
		s.jobKeys[id] = hex.EncodeToString(key[:])
		if document != "" {
			s.jobDocuments[id] = artifactRef{ScopeIdentity: s.identity, DocumentID: document, SourceFileID: in.Files[0].Source.FileID, JobKey: s.jobKeys[id]}
		}
		recordOutputFiles(s, result)
	}
	s.mu.Unlock()
	if !valid {
		proxyError(w, 503, "job_outcome_unknown")
		return
	}
	proxyBytes(w, status, body, "application/json")
}
func (p *ScopeProxy) readJob(w http.ResponseWriter, r *http.Request, s *scopeState, id string) {
	s.mu.Lock()
	allowed := s.jobs[id]
	s.mu.Unlock()
	if !allowed {
		proxyError(w, 403, "scope_denied")
		return
	}
	status, body, err := p.forward(r, r.Method, "/v1/parse/jobs/"+id, nil, false)
	if err != nil || status < 200 || status >= 300 {
		proxyError(w, 503, "backend_unavailable")
		return
	}
	if r.Method == "GET" || len(body) > 0 {
		var result map[string]any
		if json.Unmarshal(body, &result) != nil || result["job_id"] != id {
			proxyError(w, 503, "invalid_backend_job")
			return
		}
		maskJobErrors(result)
		body, _ = json.Marshal(result)
		s.mu.Lock()
		recordOutputFiles(s, result)
		s.mu.Unlock()
	}
	proxyBytes(w, status, body, "application/json")
}

// Async failures are HTTP-success envelopes too. Never cache or return their
// untrusted exception text/parameters; retain statuses and a fixed public code.
func maskJobErrors(value any) {
	switch node := value.(type) {
	case map[string]any:
		for key, child := range node {
			if key == "error" && child != nil {
				node[key] = map[string]string{"code": "parse_failed", "message": "parser backend reported failure"}
			} else {
				maskJobErrors(child)
			}
		}
	case []any:
		for _, child := range node {
			maskJobErrors(child)
		}
	}
}

// Caller holds s.mu; only a previously owned job may grant output capabilities.
func recordOutputFiles(s *scopeState, job map[string]any) {
	files, _ := job["files"].([]any)
	for _, raw := range files {
		f, _ := raw.(map[string]any)
		outputs, _ := f["output_files"].(map[string]any)
		for format, rawRef := range outputs {
			ref, _ := rawRef.(map[string]any)
			id, _ := ref["file_id"].(string)
			if safeID(id) {
				if _, known := s.files[id]; !known {
					s.files[id] = false
				}
				jobID, _ := job["job_id"].(string)
				owner, bound := s.jobDocuments[jobID]
				if bound && format == "zip" && job["status"] == "completed" && f["status"] == "completed" {
					parse, _ := f["parse"].(map[string]any)
					owner.MinerUVersion, _ = parse["parser_version"].(string)
					s.outputDocuments[id] = owner
				}
			}
		}
	}
}
