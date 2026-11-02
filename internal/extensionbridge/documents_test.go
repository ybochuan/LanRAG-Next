package extensionbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAllowedDocumentsUsesCompleteActivePartition(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			RequestID string   `json:"request_id"`
			SessionID string   `json:"session_id"`
			TenantID  uint64   `json:"tenant_id"`
			KBs       []string `json:"knowledge_base_ids"`
			IDs       []string `json:"knowledge_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.SessionID == "" {
			t.Error("missing mandatory Host correlation session")
			w.WriteHeader(400)
			return
		}
		if r.Header.Get("X-AgentRAG-Bridge-Version") != "1" {
			t.Error("missing protocol version")
		}
		excluded := []string{}
		allowed := []string{}
		for _, id := range request.IDs {
			if id == "active" {
				allowed = append(allowed, id)
			} else {
				excluded = append(excluded, id)
			}
		}
		_ = json.NewEncoder(w).Encode(Decision{RequestID: request.RequestID, TenantID: request.TenantID, KnowledgeBaseIDs: request.KBs, KnowledgeIDs: allowed, ExcludedDocumentIDs: excluded, DenyAll: len(allowed) == 0})
	}))
	defer server.Close()
	routeTo(t, server.URL)
	allowed, err := AllowedDocuments(context.Background(), 7, "kb", []string{"active", "stale", "archived", "superseded", "failed", "unknown", "active"})
	if err != nil || len(allowed) != 1 || !allowed["active"] {
		t.Fatalf("partition not enforced: %v %v", allowed, err)
	}
}

func TestAllowedDocumentsUnavailableAndMalformedFailClosed(t *testing.T) {
	for _, response := range []string{"503", "malformed", "disconnected"} {
		t.Run(response, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if response == "503" {
					w.WriteHeader(503)
					return
				}
				_, _ = w.Write([]byte(`{"knowledge_ids":["active"]}`))
			}))
			routeTo(t, server.URL)
			if response == "disconnected" {
				server.Close()
			} else {
				defer server.Close()
			}
			if allowed, err := AllowedDocuments(context.Background(), 7, "kb", []string{"active"}); err == nil || len(allowed) > 0 {
				t.Fatal("unavailable/malformed policy permitted content")
			}
		})
	}
}
