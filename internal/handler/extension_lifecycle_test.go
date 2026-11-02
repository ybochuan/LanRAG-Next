package handler

import (
	"context"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

type lifecycleKBStub struct {
	interfaces.KnowledgeBaseService
}

func (lifecycleKBStub) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{ID: "kb", TenantID: 7, CreatorID: "creator"}, nil
}

type lifecycleDocStub struct{ interfaces.KnowledgeService }

func (lifecycleDocStub) GetKnowledgeByID(context.Context, string) (*types.Knowledge, error) {
	return &types.Knowledge{ID: "doc", TenantID: 7, KnowledgeBaseID: "other-kb"}, nil
}

func TestExtensionLifecycleHTTPAuthority(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, method, path, body string
		caller                   types.Caller
		want                     int
	}{
		{"no login", "GET", "/api/v1/knowledge-bases/kb/lifecycle", "", types.Caller{}, 401},
		{"viewer cannot write", "PUT", "/api/v1/knowledge-bases/kb/lifecycle/rules", `{"expected_revision":0,"rules":{}}`, types.Caller{TenantID: 7, UserID: "creator", Role: types.TenantRoleViewer}, 403},
		{"viewer cannot declare", "PUT", "/api/v1/knowledge-bases/kb/lifecycle/documents/doc/supplements", `{"expected_revision":0,"parents":[],"operation_id":"op"}`, types.Caller{TenantID: 7, UserID: "creator", Role: types.TenantRoleViewer}, 403},
		{"foreign document", "GET", "/api/v1/knowledge-bases/kb/lifecycle/documents/doc", "", types.Caller{TenantID: 7, UserID: "creator", Role: types.TenantRoleAdmin}, 404},
		{"forged authority", "PUT", "/api/v1/knowledge-bases/kb/lifecycle/rules", `{"expected_revision":0,"rules":{},"can_write":true}`, types.Caller{TenantID: 7, UserID: "creator", Role: types.TenantRoleAdmin}, 400},
		{"business viewer cannot write", "PUT", "/api/v1/knowledge-bases/kb/lifecycle/business/guidance_seed", `{"expected_revision":0,"operation_id":"seed","business":{"payload":{"text":"test","topic":"test"}}}`, types.Caller{TenantID: 7, UserID: "creator", Role: types.TenantRoleViewer}, 403},
		{"business GET cannot publish", "GET", "/api/v1/knowledge-bases/kb/lifecycle/business/revision_publish", "", types.Caller{TenantID: 7, UserID: "creator", Role: types.TenantRoleAdmin}, 400},
		{"business cannot forge actor", "PUT", "/api/v1/knowledge-bases/kb/lifecycle/business/guidance_seed", `{"expected_revision":0,"operation_id":"seed","actor_id":"other","business":{"payload":{"text":"test","topic":"test"}}}`, types.Caller{TenantID: 7, UserID: "creator", Role: types.TenantRoleAdmin}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(middleware.ErrorHandler(), func(c *gin.Context) {
				ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, tc.caller.TenantID)
				c.Request = c.Request.WithContext(types.WithCaller(ctx, tc.caller))
				c.Next()
			})
			h := &KnowledgeBaseHandler{service: lifecycleKBStub{}, knowledgeService: lifecycleDocStub{}}
			r.GET("/api/v1/knowledge-bases/:id/lifecycle", h.ExtensionLifecycle)
			r.GET("/api/v1/knowledge-bases/:id/lifecycle/documents/:document_id", h.ExtensionLifecycle)
			r.PUT("/api/v1/knowledge-bases/:id/lifecycle/rules", h.ExtensionLifecycle)
			r.PUT("/api/v1/knowledge-bases/:id/lifecycle/documents/:document_id/supplements", h.ExtensionLifecycle)
			r.GET("/api/v1/knowledge-bases/:id/lifecycle/business/:business_action", h.ExtensionLifecycle)
			r.PUT("/api/v1/knowledge-bases/:id/lifecycle/business/:business_action", h.ExtensionLifecycle)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d", w.Code, tc.want)
			}
		})
	}
}

func TestExtensionLifecycleWriteAuthority(t *testing.T) {
	kb := &types.KnowledgeBase{TenantID: 7, CreatorID: "creator"}
	for _, tc := range []struct {
		name       string
		caller     types.Caller
		permission types.OrgMemberRole
		want       bool
	}{
		{"viewer creator", types.Caller{TenantID: 7, UserID: "creator", Role: types.TenantRoleViewer}, types.OrgRoleAdmin, false},
		{"other contributor", types.Caller{TenantID: 7, UserID: "other", Role: types.TenantRoleContributor}, types.OrgRoleAdmin, false},
		{"creator", types.Caller{TenantID: 7, UserID: "creator", Role: types.TenantRoleContributor}, types.OrgRoleAdmin, true},
		{"admin", types.Caller{TenantID: 7, UserID: "admin", Role: types.TenantRoleAdmin}, types.OrgRoleAdmin, true},
		{"shared read", types.Caller{TenantID: 8, UserID: "admin", Role: types.TenantRoleAdmin}, types.OrgRoleViewer, false},
		{"shared edit", types.Caller{TenantID: 8, UserID: "admin", Role: types.TenantRoleAdmin}, types.OrgRoleEditor, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := lifecycleCanWrite(tc.caller, kb, tc.permission); got != tc.want {
				t.Fatalf("write=%v", got)
			}
		})
	}
}

type supplementDocStub struct{ interfaces.KnowledgeService }

func (supplementDocStub) GetKnowledgeByID(_ context.Context, id string) (*types.Knowledge, error) {
	kb := "kb"
	if id == "foreign" {
		kb = "other-kb"
	}
	return &types.Knowledge{ID: id, TenantID: 7, KnowledgeBaseID: kb}, nil
}

func TestSupplementFacadeChecksEveryParentAndRejectsForgedActor(t *testing.T) {
	t.Setenv("AGENTRAG_MANAGEMENT_KEY", "")
	for _, tc := range []struct {
		body string
		want int
	}{
		{`{"expected_revision":0,"operation_id":"op","parents":["foreign"]}`, 404},
		{`{"expected_revision":0,"operation_id":"op","parents":["parent"],"actor_id":"forged"}`, 400},
		{`{"expected_revision":0,"operation_id":"op","parents":["parent"],"annotations":{}}`, 400},
		{`{"expected_revision":0,"operation_id":"op","parents":["parent"]}`, 503},
		{`{"expected_revision":0,"operation_id":"op","parents":[]}`, 503},
		{`{"expected_revision":0,"operation_id":"op","parents":null}`, 400},
		{`{"operation_id":"op","parents":[]}`, 400},
		{`{"expected_revision":0,"operation_id":"op","parents":[],"tenant_id":8}`, 400},
		{`{"expected_revision":0,"operation_id":"` + strings.Repeat("x", 65<<10) + `","parents":[]}`, 400},
	} {
		r := gin.New()
		r.Use(middleware.ErrorHandler(), func(c *gin.Context) {
			caller := types.Caller{TenantID: 7, UserID: "creator", Role: types.TenantRoleAdmin}
			ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, uint64(7))
			c.Request = c.Request.WithContext(types.WithCaller(ctx, caller))
			c.Next()
		})
		h := &KnowledgeBaseHandler{service: lifecycleKBStub{}, knowledgeService: supplementDocStub{}}
		r.PUT("/api/v1/knowledge-bases/:id/lifecycle/documents/:document_id/supplements", h.ExtensionLifecycle)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("PUT", "/api/v1/knowledge-bases/kb/lifecycle/documents/child/supplements", strings.NewReader(tc.body)))
		if w.Code != tc.want {
			t.Fatalf("status=%d want=%d body=%s", w.Code, tc.want, tc.body)
		}
	}
}
