package parser

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

const (
	proxyJSONLimit   = 64 << 10
	proxySourceLimit = 50 << 20
	proxyResultLimit = 64 << 20
	proxyStateLimit  = 128
)

// ServeHTTP exposes only the native single-file V1 subset under a configured scope.
func (p *ScopeProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if len(parts) < 5 || parts[0] != "parser" || parts[1] != "scopes" || parts[3] != "v1" || r.URL.RawQuery != "" || r.URL.RawPath != "" {
		proxyError(w, 404, "unknown_route")
		return
	}
	s, authErr := p.resolve(parts[2], r.Header.Get("Authorization"))
	if authErr != nil || len(r.Header.Values("Authorization")) != 1 {
		proxyError(w, 403, "scope_denied")
		return
	}
	for _, segment := range parts {
		if !safeID(segment) {
			proxyError(w, 400, "invalid_route")
			return
		}
	}
	v := parts[4:]
	switch {
	case len(v) == 1 && v[0] == "health" && r.Method == "GET":
		status, body, err := p.forward(r, "GET", "/v1/health", nil, false)
		if err != nil || status != 200 {
			proxyError(w, 503, "backend_unavailable")
			return
		}
		proxyBytes(w, status, body, "application/json")
	case len(v) == 1 && v[0] == "uploads" && r.Method == "POST":
		p.createUpload(w, r, s, parts[2])
	case len(v) == 3 && v[0] == "uploads" && v[2] == "content" && r.Method == "PUT":
		p.putUpload(w, r, s, v[1])
	case len(v) == 3 && v[0] == "uploads" && v[2] == "complete" && r.Method == "POST":
		p.completeUpload(w, r, s, v[1])
	case len(v) == 2 && v[0] == "parse" && v[1] == "jobs" && r.Method == "POST":
		p.createJob(w, r, s)
	case len(v) == 3 && v[0] == "parse" && v[1] == "jobs" && (r.Method == "GET" || r.Method == "DELETE"):
		p.readJob(w, r, s, v[2])
	case len(v) == 3 && v[0] == "files" && v[2] == "content" && r.Method == "GET":
		s.mu.Lock()
		input, allowed := s.files[v[1]]
		owner, archive := s.outputDocuments[v[1]]
		s.mu.Unlock()
		if !allowed {
			proxyError(w, 403, "scope_denied")
			return
		}
		if p.artifacts != nil && !input && !archive {
			proxyError(w, 409, "artifact_not_ready")
			return
		}
		status, body, err := p.forward(r, "GET", "/v1/files/"+v[1]+"/content", nil, true)
		if err != nil || status != 200 {
			proxyError(w, 503, "backend_unavailable")
			return
		}
		if p.artifacts != nil && archive {
			if err := p.artifacts.persist(owner, body); err != nil {
				proxyError(w, 503, "artifact_commit_failed")
				return
			}
			if p.journal != nil {
				if err := p.journal.register(r.Context(), owner, body); err != nil {
					proxyError(w, 503, "artifact_registration_failed")
					return
				}
			}
		}
		proxyBytes(w, status, body, "application/octet-stream")
	default:
		proxyError(w, 404, "unknown_route")
	}
}

func (p *ScopeProxy) forward(r *http.Request, method, path string, body []byte, bulk bool) (int, []byte, error) {
	req, err := http.NewRequestWithContext(r.Context(), method, p.upstream+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	if p.upstreamKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.upstreamKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		if method == "PUT" {
			req.Header.Set("Content-Type", "application/octet-stream")
		}
	}
	client := p.client
	if bulk {
		client = p.bulkClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	limit := int64(4 << 20)
	if bulk {
		limit = proxyResultLimit
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(b)) > limit {
		return 0, nil, errors.New("invalid backend response")
	}
	return resp.StatusCode, b, nil
}
func readProxyJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	b, err := io.ReadAll(io.LimitReader(r.Body, proxyJSONLimit+1))
	if err != nil {
		proxyError(w, 400, "invalid_request")
		return false
	}
	if len(b) > proxyJSONLimit {
		proxyError(w, 413, "request_too_large")
		return false
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		proxyError(w, 400, "invalid_request")
		return false
	}
	return true
}
func proxyError(w http.ResponseWriter, status int, code string) {
	proxyJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": "parser proxy request rejected"}})
}
func proxyJSON(w http.ResponseWriter, status int, value any) {
	b, err := json.Marshal(value)
	if err != nil {
		proxyError(w, 503, "invalid_response")
		return
	}
	proxyBytes(w, status, b, "application/json")
}
func proxyBytes(w http.ResponseWriter, status int, b []byte, kind string) {
	w.Header().Set("Content-Type", kind)
	w.WriteHeader(status)
	_, _ = w.Write(b)
}
