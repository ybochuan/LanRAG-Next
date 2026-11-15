package business

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

func TestFamilyAvailabilityReconcilesNativeDeletion(t *testing.T) {
	s, _, create := familyFixture(t)
	ctx := t.Context()
	family, err := s.Execute(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	add := cmd("family_member_add", "availability-member")
	add.ID, add.ExpectedRevision, add.Payload.DocumentID = family.ID, family.Revision, "2026"
	if family, err = s.Execute(ctx, add); err != nil {
		t.Fatal(err)
	}

	deleted := false
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/knowledge/batch" || r.Header.Get("X-API-Key") != "fixture-api-key" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		data := []map[string]any{}
		if !deleted {
			for _, id := range r.URL.Query()["ids"] {
				data = append(data, map[string]any{
					"id": id, "tenant_id": 7, "knowledge_base_id": "kb",
					"parse_status": "completed", "enable_status": "enabled",
				})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
	}))
	defer native.Close()
	client, err := weknora.NewClient(native.URL, "fixture-api-key", 7)
	if err != nil {
		t.Fatal(err)
	}
	s.WithRemote(NewNativePublisher(client))

	query := Command{Scope: create.Scope, ID: family.ID}
	members, err := s.FamilyMembers(ctx, query)
	if err != nil || len(members) != 1 || members[0].Availability != documentAvailabilityActive {
		t.Fatalf("active member availability: %+v %v", members, err)
	}
	deleted = true
	members, err = s.FamilyMembers(ctx, query)
	if err != nil || len(members) != 1 || members[0].Availability != documentAvailabilityMissing {
		t.Fatalf("deleted member availability: %+v %v", members, err)
	}
	view, err := s.FamilyWorkspace(ctx, query, "members")
	if err != nil || len(view.Members) != 1 || view.Members[0].Availability != documentAvailabilityMissing {
		t.Fatalf("workbench did not reconcile native deletion: %+v %v", view, err)
	}
}
