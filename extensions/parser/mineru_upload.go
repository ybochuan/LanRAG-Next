package parser

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type uploadRequest struct {
	Filename string `json:"filename"`
	Bytes    int64  `json:"bytes"`
	MimeType string `json:"mime_type"`
	Purpose  string `json:"purpose"`
	SHA256   string `json:"sha256sum"`
}
type uploadReply struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	UploadURL    string `json:"upload_url"`
	UploadMethod string `json:"upload_method"`
	File         *struct {
		ID string `json:"id"`
	} `json:"file"`
}

func (p *ScopeProxy) createUpload(w http.ResponseWriter, r *http.Request, s *scopeState, route string) {
	var in uploadRequest
	if !readProxyJSON(w, r, &in) {
		return
	}
	decoded, err := hex.DecodeString(in.SHA256)
	if err != nil || len(decoded) != 32 || in.Bytes <= 0 || in.Bytes > proxySourceLimit || in.Filename == "" || len(in.Filename) > 256 || in.Purpose != "parse" {
		proxyError(w, 400, "invalid_upload")
		return
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		proxyError(w, 503, "unavailable")
		return
	}
	id := "pu-" + hex.EncodeToString(nonce[:])
	u := &sourceUpload{size: in.Bytes, hash: hex.EncodeToString(decoded), busy: true, declaredPDF: strings.HasSuffix(strings.ToLower(in.Filename), ".pdf") || strings.EqualFold(strings.TrimSpace(strings.Split(in.MimeType, ";")[0]), "application/pdf")}
	u.filename = in.Filename
	s.mu.Lock()
	if len(s.uploads) >= proxyStateLimit {
		s.mu.Unlock()
		proxyError(w, 503, "capacity_reached")
		return
	}
	s.uploads[id] = u
	s.mu.Unlock()
	b, _ := json.Marshal(in)
	status, body, err := p.forward(r, "POST", "/v1/uploads", b, false)
	var result uploadReply
	valid := err == nil && status >= 200 && status < 300 && json.Unmarshal(body, &result) == nil
	if valid && result.Status == "pending" {
		valid = safeID(result.ID) && result.UploadMethod == "PUT" && result.UploadURL == p.upstream+"/v1/uploads/"+result.ID+"/content"
	} else if valid && result.Status == "completed" {
		valid = result.File != nil && safeID(result.File.ID)
	} else {
		valid = false
	}
	s.mu.Lock()
	if !valid {
		delete(s.uploads, id)
		s.mu.Unlock()
		proxyError(w, 503, "invalid_backend_upload")
		return
	}
	u.backendID = result.ID
	u.uploadURL = result.UploadURL
	if result.File != nil {
		u.backendFile = result.File.ID
	}
	u.busy = false
	s.mu.Unlock()
	proxyJSON(w, 200, map[string]any{"id": id, "status": "pending", "upload_method": "PUT", "upload_url": p.origin + "/parser/scopes/" + route + "/v1/uploads/" + id + "/content"})
}
func takeUpload(w http.ResponseWriter, s *scopeState, id string) (*sourceUpload, bool) {
	s.mu.Lock()
	u := s.uploads[id]
	if u == nil {
		s.mu.Unlock()
		proxyError(w, 403, "scope_denied")
		return nil, false
	}
	if u.busy {
		s.mu.Unlock()
		proxyError(w, 409, "upload_busy")
		return nil, false
	}
	u.busy = true
	s.mu.Unlock()
	return u, true
}
func releaseUpload(s *scopeState, u *sourceUpload) { s.mu.Lock(); u.busy = false; s.mu.Unlock() }
func (p *ScopeProxy) putUpload(w http.ResponseWriter, r *http.Request, s *scopeState, id string) {
	u, ok := takeUpload(w, s, id)
	if !ok {
		return
	}
	defer releaseUpload(s, u)
	b, err := io.ReadAll(io.LimitReader(r.Body, u.size+1))
	sum := sha256.Sum256(b)
	if err != nil || int64(len(b)) != u.size || hex.EncodeToString(sum[:]) != u.hash {
		proxyError(w, 400, "invalid_source_bytes")
		return
	}
	isPDF := u.declaredPDF || bytes.Contains(b[:min(len(b), 1024)], []byte("%PDF-"))
	if s.nativeDocument != "" && !isPDF {
		proxyError(w, 422, "native_pdf_required")
		return
	}
	if isPDF {
		if p.attachments != nil && s.nativeDocument != "" {
			name := u.filename
			if name == "" {
				name = "source.pdf"
			}
			if _, err := p.attachments.Process(r.Context(), s.identity.TenantID, s.identity.KnowledgeBaseID, s.nativeDocument, name, b); err != nil {
				proxyError(w, 503, "attachment_import_failed")
				return
			}
		}
		report, err := PreflightPDF(r.Context(), b)
		if err != nil {
			proxyError(w, 422, "pdf_preflight_failed")
			return
		}
		if _, err := DecidePDF(report); err != nil {
			proxyError(w, 422, "pdf_requires_review")
			return
		}
		u.preflight = &report
	}
	if u.backendFile == "" {
		status, _, err := p.forward(r, "PUT", "/v1/uploads/"+u.backendID+"/content", b, true)
		if err != nil || status < 200 || status >= 300 {
			proxyError(w, 503, "upload_failed")
			return
		}
	}
	u.verified = true
	w.WriteHeader(204)
}
func (p *ScopeProxy) completeUpload(w http.ResponseWriter, r *http.Request, s *scopeState, id string) {
	var in struct {
		SHA256 string `json:"sha256sum"`
	}
	if !readProxyJSON(w, r, &in) {
		return
	}
	u, ok := takeUpload(w, s, id)
	if !ok {
		return
	}
	defer releaseUpload(s, u)
	if !u.verified || in.SHA256 != u.hash {
		proxyError(w, 409, "source_not_verified")
		return
	}
	if u.backendFile == "" {
		b, _ := json.Marshal(in)
		status, body, err := p.forward(r, "POST", "/v1/uploads/"+u.backendID+"/complete", b, false)
		var result uploadReply
		if err != nil || status < 200 || status >= 300 || json.Unmarshal(body, &result) != nil || result.Status != "completed" || result.File == nil || !safeID(result.File.ID) {
			proxyError(w, 503, "complete_failed")
			return
		}
		u.backendFile = result.File.ID
	}
	s.mu.Lock()
	s.files[u.backendFile] = true
	s.hashes[u.backendFile] = u.hash
	if s.nativeDocument != "" {
		s.documents[u.backendFile] = s.nativeDocument
	}
	if u.preflight != nil {
		s.pdfs[u.backendFile] = *u.preflight
	}
	s.mu.Unlock()
	proxyJSON(w, 200, map[string]any{"id": id, "status": "completed", "file": map[string]string{"id": u.backendFile}})
}
