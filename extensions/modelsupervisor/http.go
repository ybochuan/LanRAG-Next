package modelsupervisor

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SupervisorVersionHeader carries the wire contract version. A client that asks
// for a different major version is rejected rather than guessed at.
const SupervisorVersionHeader = "X-AgentRAG-Model-Supervisor-Version"

// WireVersion is the only contract version this server speaks.
const WireVersion = "1"

// maxBodyBytes bounds every request body. The payloads here are tiny; anything
// larger is a mistake or an attack.
const maxBodyBytes = 1 << 16

// Server exposes the manager over loopback HTTP. Every dependency is a field so
// the transport can be tested without a real GPU, WSL, or llama-server.
type Server struct {
	Manager   *Manager
	Prober    *Prober
	Token     string
	Resources func() Resources
	// Catalog lists the models and runtime present on disk. A nil hook makes the
	// endpoint answer 503, which keeps the transport testable without a
	// filesystem and keeps "not wired" distinct from "wired but empty".
	Catalog func() (Catalog, error)
	Now     func() time.Time
	// AllowedOrigins lists the console origins permitted to call this API from a
	// browser. The supervisor lives on a different port than the console, so the
	// browser treats every call as cross-origin; an empty list disables CORS
	// entirely, which is the safe default for a headless deployment.
	AllowedOrigins []string
}

// NewServer validates the dependencies the handlers assume.
func NewServer(manager *Manager, prober *Prober, token string) (*Server, error) {
	if manager == nil {
		return nil, errors.New("manager required")
	}
	if prober == nil {
		return nil, errors.New("prober required")
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("api token required: the supervisor refuses to serve unauthenticated")
	}
	return &Server{
		Manager:   manager,
		Prober:    prober,
		Token:     token,
		Resources: func() Resources { return QueryResources(nil) },
		Now:       time.Now,
	}, nil
}

// Handler builds the route table. Only /healthz is unauthenticated.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /extensions/models/list", s.guard(s.handleList))
	mux.HandleFunc("GET /extensions/models/host-resources", s.guard(s.handleResources))
	mux.HandleFunc("GET /extensions/models/catalog", s.guard(s.handleCatalog))
	mux.HandleFunc("GET /extensions/models/probe", s.guard(s.handleProbe))
	mux.HandleFunc("GET /extensions/models/{id}/probe", s.guard(s.handleProbeOne))
	mux.HandleFunc("POST /extensions/models/start", s.guard(s.handleStart))
	mux.HandleFunc("POST /extensions/models/stop", s.guard(s.handleStop))
	mux.HandleFunc("GET /extensions/models/{id}/logs", s.guard(s.handleLogs))
	return s.cors(mux)
}

// cors answers the browser's preflight and stamps the response headers. It runs
// ahead of the credential guard so that a preflight, which never carries the
// token, is not answered with 401 and reported as a network error.
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := origin != "" && s.originAllowed(origin)
		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers",
				"Content-Type, X-API-Key, Authorization, "+SupervisorVersionHeader)
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			if !allowed {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) originAllowed(origin string) bool {
	for _, candidate := range s.AllowedOrigins {
		if candidate == origin {
			return true
		}
	}
	return false
}

func (s *Server) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if version := r.Header.Get(SupervisorVersionHeader); version != "" && version != WireVersion {
			writeError(w, http.StatusBadRequest, "unsupported_version",
				fmt.Sprintf("unsupported %s %q; this server speaks %q", SupervisorVersionHeader, version, WireVersion))
			return
		}
		if subtle.ConstantTimeCompare([]byte(credential(r)), []byte(s.Token)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid credential")
			return
		}
		next(w, r)
	}
}

// credential accepts the Core's X-API-Key convention and the Bearer convention,
// so an operator does not have to remember which service wants which.
func credential(r *http.Request) string {
	if key := r.Header.Get("X-API-Key"); key != "" {
		return key
	}
	if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	}
	return ""
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": WireVersion})
}

func (s *Server) handleList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"instances": s.Manager.List()})
}

func (s *Server) handleResources(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Resources())
}

func (s *Server) handleCatalog(w http.ResponseWriter, _ *http.Request) {
	if s.Catalog == nil {
		writeError(w, http.StatusServiceUnavailable, "catalog_unavailable", "model catalog is not configured")
		return
	}
	catalog, err := s.Catalog()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "catalog_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, catalog)
}

func (s *Server) handleProbe(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	instances := s.Manager.List()
	results := make([]map[string]any, 0, len(instances))
	ports := make([]int, 0, len(instances))
	for _, view := range instances {
		ports = append(ports, view.Port)
	}
	observed := ObservePorts(ctx, ports, nil)

	for _, view := range instances {
		spec := Spec{ID: view.ID, Model: view.Model, TaskType: view.TaskType, Port: view.Port}
		result := s.Prober.Check(ctx, spec)
		results = append(results, map[string]any{"id": view.ID, "probe": result})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"instances":      results,
		"observed_ports": observed,
	})
}

func (s *Server) handleProbeOne(w http.ResponseWriter, r *http.Request) {
	view, err := s.Manager.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), codeFor(err), err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	spec := Spec{ID: view.ID, Model: view.Model, TaskType: view.TaskType, Port: view.Port}
	writeJSON(w, http.StatusOK, map[string]any{"id": view.ID, "probe": s.Prober.Check(ctx, spec)})
}

type startRequest struct {
	Spec
	// Force starts an instance the capacity precheck refused. It is recorded in
	// the response so an override is never silent.
	Force bool `json:"force"`
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	var request startRequest
	if err := decodeBody(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	request.Spec = request.Spec.Normalize()
	if err := request.Spec.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	_, size, err := s.Manager.ModelPath(request.Spec)
	if err != nil {
		writeError(w, statusFor(err), codeFor(err), err.Error())
		return
	}
	// A CPU-only instance never touches the card, so a VRAM budget says nothing
	// about whether it fits. Gating it on free VRAM would refuse a start that
	// cannot possibly exhaust the GPU — the guard must measure the resource the
	// instance actually consumes.
	if request.Spec.usesGPU() {
		estimate := EstimateGB(size, request.Spec.CtxSize)
		resources := s.Resources()
		if ok, reason := resources.CheckCapacity(estimate); !ok && !request.Force {
			writeJSON(w, http.StatusPreconditionRequired, map[string]any{
				"error":        ErrCapacityExceeded.Error() + ": " + reason,
				"code":         "capacity_exceeded",
				"estimated_gb": estimate,
				"free_gb":      resources.FreeGB,
				"reserve_gb":   resources.ReserveGB,
			})
			return
		}
	}
	view, err := s.Manager.Start(request.Spec)
	if err != nil {
		writeError(w, statusFor(err), codeFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"instance": view, "forced": request.Force})
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID string `json:"id"`
	}
	if err := decodeBody(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if strings.TrimSpace(request.ID) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "id is required")
		return
	}
	if err := s.Manager.Stop(request.ID); err != nil {
		writeError(w, statusFor(err), codeFor(err), err.Error())
		return
	}
	view, err := s.Manager.Get(request.ID)
	if err != nil {
		writeError(w, statusFor(err), codeFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instance": view})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	since := uint64(0)
	if raw := r.URL.Query().Get("since"); raw != "" {
		parsed, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "since must be a non-negative integer")
			return
		}
		since = parsed
	}
	lines, next, err := s.Manager.Logs(id, since)
	if err != nil {
		writeError(w, statusFor(err), codeFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": lines, "next": next})
}

// statusFor maps domain errors to HTTP statuses in one place, so a new error
// cannot silently become a 500.
func statusFor(err error) int {
	switch {
	case errors.Is(err, ErrUnknownInstance):
		return http.StatusNotFound
	case errors.Is(err, ErrStateConflict), errors.Is(err, ErrDuplicateID),
		errors.Is(err, ErrDuplicatePort), errors.Is(err, ErrPortInUse):
		return http.StatusConflict
	case errors.Is(err, ErrResourceDenied), errors.Is(err, ErrCapacityExceeded):
		return http.StatusPreconditionRequired
	case errors.Is(err, ErrUnsupported):
		return http.StatusServiceUnavailable
	default:
		return http.StatusBadRequest
	}
}

func codeFor(err error) string {
	switch {
	case errors.Is(err, ErrUnknownInstance):
		return "unknown_instance"
	case errors.Is(err, ErrStateConflict):
		return "state_conflict"
	case errors.Is(err, ErrDuplicateID):
		return "duplicate_id"
	case errors.Is(err, ErrDuplicatePort):
		return "duplicate_port"
	case errors.Is(err, ErrPortInUse):
		return "port_in_use"
	case errors.Is(err, ErrResourceDenied), errors.Is(err, ErrCapacityExceeded):
		return "capacity_exceeded"
	case errors.Is(err, ErrUnsupported):
		return "unsupported_platform"
	default:
		return "invalid_request"
	}
}

func decodeBody(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": message, "code": code})
}
