package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/ybochuan/LanRAG-Next/extensions/contract"
)

// RetrievePolicy returns excluded IDs within the supplied authorized candidates.
// Implementations must honor context and derive decisions from their own truth;
// this boundary does not infer governance from native parse/ingest statuses.
type RetrievePolicy interface {
	ExcludedDocumentIDs(context.Context, contract.BeforeRetrieveRequest) ([]string, error)
}

// RetrieveHandler validates requests and applies either a scope-bound policy or
// the explicitly selected M0 pass-through behavior.
// Register it through NewServerWithServices to enforce the Bridge version gate.
type RetrieveHandler struct {
	tenantID    uint64
	kbs         map[string]bool
	policy      RetrievePolicy
	passThrough bool
}

// NewPassThroughRetrieveHandler implements the user-approved M0 default: validate
// a nonempty request scope, then preserve native-authorized candidates without exclusions.
// This is a transport gate, not an active-state or other governance policy.
func NewPassThroughRetrieveHandler() *RetrieveHandler {
	return &RetrieveHandler{policy: allowAllPolicy{}, passThrough: true}
}

type allowAllPolicy struct{}

func (allowAllPolicy) ExcludedDocumentIDs(ctx context.Context, _ contract.BeforeRetrieveRequest) ([]string, error) {
	return nil, ctx.Err()
}

// NewRetrieveHandler requires an explicit policy and bounded nonempty scope.
// It does not open a database, access the network, or enable any other hook.
func NewRetrieveHandler(tenantID uint64, kbs []string, policy RetrievePolicy) (*RetrieveHandler, error) {
	if policy == nil || (contract.Context{TenantID: tenantID, KnowledgeBaseIDs: kbs}).ValidateScope() != nil {
		return nil, errors.New("retrieval policy and valid tenant/KB allowlist required")
	}
	h := &RetrieveHandler{tenantID: tenantID, kbs: make(map[string]bool, len(kbs)), policy: policy}
	for _, kb := range kbs {
		h.kbs[kb] = true
	}
	return h, nil
}

// ServeHTTP validates scope and emits only a subset of the authorized candidates.
// Malformed policy output or a canceled request can never produce a permit.
func (h *RetrieveHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Context().Err() != nil {
		writeError(w, 503, "retrieval_unavailable")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, contract.MaxRequestBytes))
	if err != nil {
		var oversized *http.MaxBytesError
		if errors.As(err, &oversized) {
			writeError(w, http.StatusRequestEntityTooLarge, "bridge_body_too_large")
		} else {
			writeError(w, 400, "invalid_retrieval_request")
		}
		return
	}
	request, err := contract.DecodeBeforeRetrieve(data)
	if err != nil {
		writeError(w, 400, "invalid_retrieval_request")
		return
	}
	if h.passThrough && len(request.KnowledgeIDs) == 0 {
		// The native M0 gate checks HTTP status only. A 200/deny_all response
		// would still allow native whole-KB search; refuse at the HTTP boundary.
		writeError(w, http.StatusForbidden, "empty_retrieval_scope")
		return
	}
	if !h.passThrough {
		if request.TenantID != h.tenantID {
			writeError(w, 403, "retrieval_scope_denied")
			return
		}
		for _, kb := range request.KnowledgeBaseIDs {
			if !h.kbs[kb] {
				writeError(w, 403, "retrieval_scope_denied")
				return
			}
		}
	}
	if r.Context().Err() != nil {
		writeError(w, 503, "retrieval_unavailable")
		return
	}
	// Snapshot before passing slices to the policy. Even a policy that mutates
	// its input cannot alter the response's original authorization boundary.
	candidates := append([]string{}, request.KnowledgeIDs...)
	response := contract.BeforeRetrieveResponse{RequestID: request.RequestID, TenantID: request.TenantID,
		KnowledgeBaseIDs: append([]string{}, request.KnowledgeBaseIDs...), KnowledgeIDs: []string{}, ExcludedDocumentIDs: []string{}, DenyAll: true}
	if len(candidates) != 0 {
		ids, err := h.policy.ExcludedDocumentIDs(r.Context(), request)
		if err != nil || r.Context().Err() != nil {
			writeError(w, 503, "retrieval_unavailable")
			return
		}
		if len(ids) > len(candidates) {
			writeError(w, 503, "retrieval_unavailable")
			return
		}
		allowed := make(map[string]bool, len(candidates))
		for _, id := range candidates {
			allowed[id] = true
		}
		excluded := make(map[string]bool, len(ids))
		for _, id := range ids {
			if !allowed[id] || excluded[id] {
				writeError(w, 503, "retrieval_unavailable")
				return
			}
			excluded[id] = true
		}
		for _, id := range candidates {
			if excluded[id] {
				response.ExcludedDocumentIDs = append(response.ExcludedDocumentIDs, id)
			} else {
				response.KnowledgeIDs = append(response.KnowledgeIDs, id)
			}
		}
		response.DenyAll = len(response.KnowledgeIDs) == 0
	}
	if r.Context().Err() != nil {
		writeError(w, 503, "retrieval_unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}
