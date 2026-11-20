package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const qaFeedbackOwnerPath = "/api/v1/messages/session/message/feedback"
const qaFeedbackInboxPath = "/api/v1/messages/feedback"

type qaFeedbackTestStore struct {
	session      types.Session
	answer       types.Message
	questions    []*types.MessageWithSession
	kb           *types.KnowledgeBase
	messageReads int
}

func (s *qaFeedbackTestStore) GetOwnedSession(context.Context, string) (*types.Session, error) {
	return &s.session, nil
}

func (s *qaFeedbackTestStore) GetMessage(_ context.Context, sessionID, messageID string) (*types.Message, error) {
	s.messageReads++
	if sessionID != s.answer.SessionID || messageID != s.answer.ID {
		return nil, nil
	}
	return &s.answer, nil
}

func (s *qaFeedbackTestStore) GetMessagesByRequestIDs(_ context.Context, sessionID string, requestIDs []string) ([]*types.MessageWithSession, error) {
	if sessionID != s.answer.SessionID || len(requestIDs) != 1 || requestIDs[0] != s.answer.RequestID {
		return nil, nil
	}
	return s.questions, nil
}

func (s *qaFeedbackTestStore) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

func qaFeedbackTestServer(t *testing.T) (*gin.Engine, *qaFeedbackTestStore, *[]extensionbridge.ManagementRequest) {
	t.Helper()
	now := time.Unix(200, 0)
	store := &qaFeedbackTestStore{
		session: types.Session{ID: "session", TenantID: 7, UserID: "user"},
		answer:  types.Message{ID: "message", SessionID: "session", RequestID: "request", Role: "assistant", IsCompleted: true, Content: "saved answer", CreatedAt: now},
		questions: []*types.MessageWithSession{
			{Message: types.Message{SessionID: "session", RequestID: "unrelated", Role: "user", Content: "other turn", CreatedAt: now.Add(-2 * time.Second)}},
			{Message: types.Message{SessionID: "session", RequestID: "request", Role: "user", Content: "saved question", CreatedAt: now.Add(-time.Second)}},
			{Message: types.Message{SessionID: "session", RequestID: "request", Role: "user", Content: "later steer", CreatedAt: now.Add(time.Second)}},
		},
		kb: &types.KnowledgeBase{ID: "kb", TenantID: 7},
	}
	requests := new([]extensionbridge.ManagementRequest)
	h := NewQAFeedbackHandler(store, store, store, nil)
	h.manage = func(_ context.Context, req extensionbridge.ManagementRequest) (json.RawMessage, error) {
		*requests = append(*requests, req)
		if req.Action == "qa_feedback_list" {
			return json.RawMessage(`[]`), nil
		}
		return json.RawMessage(`{"id":"feedback-id","revision":1,"status":"received"}`), nil
	}
	r := gin.New()
	r.GET("/api/v1/messages/:session_id/:id/feedback", h.OwnerFeedback)
	r.POST("/api/v1/messages/:session_id/:id/feedback", h.OwnerFeedback)
	r.GET(qaFeedbackInboxPath, h.InboxFeedback)
	r.PUT(qaFeedbackInboxPath+"/:id", h.InboxFeedback)
	return r, store, requests
}

func qaFeedbackTestAuth(role types.TenantRole, source, principalType string) context.Context {
	ctx := types.WithCaller(context.Background(), types.Caller{UserID: "user", TenantID: 7, Role: role})
	ctx = types.WithPrincipal(ctx, types.Principal{Type: principalType, ID: "user"})
	return types.WithPrincipalAuthSource(ctx, source)
}

func qaFeedbackTestCall(r *gin.Engine, ctx context.Context, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx))
	return w
}

func TestQAFeedbackOwnerAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name      string
		source    string
		principal string
		owner     string
		want      int
	}{
		{"anonymous", "", "", "user", http.StatusUnauthorized},
		{"embed token", types.AuthSourceEmbed, types.PrincipalEmbedSession, "user", http.StatusUnauthorized},
		{"tenant API key", types.AuthSourceAPITenantKey, types.PrincipalAPITenant, "user", http.StatusForbidden},
		{"other owner even as tenant admin", types.AuthSourceWeb, types.PrincipalWebUser, "other", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, store, requests := qaFeedbackTestServer(t)
			store.session.UserID = tc.owner
			ctx := qaFeedbackTestAuth(types.TenantRoleAdmin, tc.source, tc.principal)
			if tc.principal == types.PrincipalAPITenant {
				ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KeyID: 1})
			}
			w := qaFeedbackTestCall(r, ctx, http.MethodPost, qaFeedbackOwnerPath, `{"operation_id":"op","rating":"like"}`)
			require.Equal(t, tc.want, w.Code, w.Body.String())
			require.Zero(t, store.messageReads)
			require.Empty(t, *requests)
		})
	}
}

func TestQAFeedbackPreviewAndSubmit(t *testing.T) {
	r, store, requests := qaFeedbackTestServer(t)
	store.answer.IsFallback = true
	ctx := qaFeedbackTestAuth(types.TenantRoleViewer, types.AuthSourceWeb, types.PrincipalWebUser)
	preview := qaFeedbackTestCall(r, ctx, http.MethodGet, qaFeedbackOwnerPath, "")
	require.Equal(t, http.StatusOK, preview.Code, preview.Body.String())
	var result struct {
		Data struct {
			qaFeedbackSnapshot
			Digest string `json:"snapshot_digest"`
		}
	}
	require.NoError(t, json.Unmarshal(preview.Body.Bytes(), &result))
	require.Equal(t, "saved question", result.Data.Question)
	require.Equal(t, "saved answer", result.Data.AnswerExcerpt)
	require.Len(t, result.Data.Digest, 64)
	require.Empty(t, *requests)
	body := `{"operation_id":"op","rating":"like","snapshot_digest":"` + result.Data.Digest + `"}`
	for i := 0; i < 2; i++ {
		w := qaFeedbackTestCall(r, ctx, http.MethodPost, qaFeedbackOwnerPath, body)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	require.Len(t, *requests, 2)
	first := (*requests)[0]
	require.Equal(t, first, (*requests)[1])
	require.Equal(t, "qa_feedback_submit", first.Action)
	require.Equal(t, "7:user", first.ActorID)
	require.Empty(t, first.KnowledgeBaseID)
	require.JSONEq(t, `{"payload":{"session_id":"session","message_id":"message","text":"","reason":"","feedback_context":{"rating":"like","question":"saved question","answer_excerpt":"saved answer"}}}`, string(first.Business))

	store.answer.IsCompleted = false
	w := qaFeedbackTestCall(r, ctx, http.MethodGet, qaFeedbackOwnerPath, "")
	require.Equal(t, http.StatusNotFound, w.Code)
	store.answer.IsCompleted = true
	store.answer.RunState = types.MessageRunStateAwaitingInput
	w = qaFeedbackTestCall(r, ctx, http.MethodGet, qaFeedbackOwnerPath, "")
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestQAFeedbackRestrictedPreviewCannotExpand(t *testing.T) {
	r, store, requests := qaFeedbackTestServer(t)
	store.answer.KnowledgeReferences = types.References{{ID: "chunk", KnowledgeID: "doc", KnowledgeBaseID: "kb", MatchType: types.MatchTypeEmbedding}}
	store.kb.TenantID = 8
	ctx := qaFeedbackTestAuth(types.TenantRoleViewer, types.AuthSourceWeb, types.PrincipalWebUser)
	preview := qaFeedbackTestCall(r, ctx, http.MethodGet, qaFeedbackOwnerPath, "")
	require.Equal(t, http.StatusOK, preview.Code, preview.Body.String())
	var result struct {
		Data struct {
			qaFeedbackSnapshot
			Digest string `json:"snapshot_digest"`
		}
	}
	require.NoError(t, json.Unmarshal(preview.Body.Bytes(), &result))
	require.True(t, result.Data.SnapshotRestricted)
	require.Empty(t, result.Data.Question)
	require.Empty(t, result.Data.AnswerExcerpt)
	body := `{"operation_id":"op","rating":"correction","text":"private comment","reason":"inaccurate","snapshot_digest":"` + result.Data.Digest + `"}`
	w := qaFeedbackTestCall(r, ctx, http.MethodPost, qaFeedbackOwnerPath, body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, *requests, 1)
	require.Empty(t, (*requests)[0].KnowledgeBaseID)
	require.JSONEq(t, `{"payload":{"session_id":"session","message_id":"message","text":"","reason":"inaccurate","feedback_context":{"rating":"correction","comment_present":true,"snapshot_restricted":true}}}`, string((*requests)[0].Business))

	store.kb.TenantID = 7
	w = qaFeedbackTestCall(r, ctx, http.MethodPost, qaFeedbackOwnerPath, body)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Len(t, *requests, 1)

	w = qaFeedbackTestCall(r, ctx, http.MethodPost, qaFeedbackOwnerPath, `{"operation_id":"scoped","rating":"like"}`)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Len(t, *requests, 1)
	w = qaFeedbackTestCall(r, ctx, http.MethodGet, qaFeedbackOwnerPath, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	w = qaFeedbackTestCall(r, ctx, http.MethodPost, qaFeedbackOwnerPath, `{"operation_id":"scoped","rating":"like","snapshot_digest":"`+result.Data.Digest+`"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "kb", (*requests)[1].KnowledgeBaseID)
	require.Contains(t, string((*requests)[1].Business), `"cited_chunk_ids":["chunk"]`)
}

func TestQAFeedbackInboxAuthorizationAndScope(t *testing.T) {
	r, _, requests := qaFeedbackTestServer(t)
	review := `{"operation_id":"review","expected_revision":2,"action":"feedback_needs_evidence"}`
	for _, role := range []types.TenantRole{types.TenantRoleViewer, types.TenantRoleContributor} {
		ctx := qaFeedbackTestAuth(role, types.AuthSourceWeb, types.PrincipalWebUser)
		require.Equal(t, http.StatusForbidden, qaFeedbackTestCall(r, ctx, http.MethodGet, qaFeedbackInboxPath, "").Code)
		require.Equal(t, http.StatusForbidden, qaFeedbackTestCall(r, ctx, http.MethodPut, qaFeedbackInboxPath+"/feedback-id", review).Code)
	}
	require.Empty(t, *requests)
	admin := qaFeedbackTestAuth(types.TenantRoleAdmin, types.AuthSourceWeb, types.PrincipalWebUser)
	w := qaFeedbackTestCall(r, admin, http.MethodGet, qaFeedbackInboxPath+"?id=feedback-id&state=received", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.JSONEq(t, `{"success":true,"data":[]}`, w.Body.String())
	w = qaFeedbackTestCall(r, admin, http.MethodPut, qaFeedbackInboxPath+"/feedback-id", review)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	list, put := (*requests)[0], (*requests)[1]
	require.Equal(t, "qa_feedback_list", list.Action)
	require.Equal(t, 1, list.Page)
	require.Equal(t, 20, list.PageSize)
	require.Equal(t, "received", list.State)
	require.JSONEq(t, `{"id":"feedback-id"}`, string(list.Business))
	require.Equal(t, "qa_feedback_review", put.Action)
	require.EqualValues(t, 2, put.ExpectedRevision)
	require.Equal(t, "review", put.OperationID)
	require.JSONEq(t, `{"id":"feedback-id","payload":{"decision":"feedback_needs_evidence"}}`, string(put.Business))
	for _, req := range *requests {
		require.EqualValues(t, 7, req.TenantID)
		require.Equal(t, "7:user", req.ActorID)
		require.Empty(t, req.KnowledgeBaseID)
		require.True(t, req.CanWrite)
	}
}
