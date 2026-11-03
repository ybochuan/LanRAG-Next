package management

import (
	"encoding/json"
	"testing"
)

func TestBusinessManagementUsesExistingAuthorityAndReceipts(t *testing.T) {
	s := fixture(t)
	key := s.key
	body := `{"action":"business_guidance_seed","tenant_id":7,"knowledge_base_id":"kb","actor_id":"7:owner","operation_id":"seed","business":{"payload":{"text":"Check effective date","topic":"date"}}}`
	if w := call(s, body, key); w.Code != 403 {
		t.Fatalf("readonly business mutation=%d %s", w.Code, w.Body.String())
	}
	var request map[string]any
	if e := json.Unmarshal([]byte(body), &request); e != nil {
		t.Fatal(e)
	}
	request["can_write"] = true
	encoded, _ := json.Marshal(request)
	w := call(s, string(encoded), key)
	if w.Code != 200 {
		t.Fatalf("business not wired: %d %s", w.Code, w.Body.String())
	}
	again := call(s, string(encoded), key)
	if again.Code != 200 || again.Body.String() != w.Body.String() {
		t.Fatal("unstable receipt")
	}
	request["tenant_id"] = 8
	encoded, _ = json.Marshal(request)
	if call(s, string(encoded), key).Code != 403 {
		t.Fatal("foreign tenant accepted")
	}
	if call(s, `{"action":"business_guidance_list","tenant_id":7,"knowledge_base_id":"kb","business":{}}`, "bad").Code != 401 {
		t.Fatal("credential bypass")
	}
}

func TestRelationManagementListIsScopedAndReadonly(t *testing.T) {
	s := fixture(t)
	w := call(s, `{"action":"business_relation_list","tenant_id":7,"knowledge_base_id":"kb","can_write":false}`, s.key)
	if w.Code != 200 {
		t.Fatalf("readonly relation list: %d %s", w.Code, w.Body.String())
	}
	if w = call(s, `{"action":"business_relation_list","tenant_id":8,"knowledge_base_id":"kb"}`, s.key); w.Code != 403 {
		t.Fatal("foreign scope read", w.Code)
	}
}
