package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	qaFeedbackMaxID   = 128
	qaFeedbackMaxText = 16000
)

type qaFeedbackSessions interface {
	GetOwnedSession(context.Context, string) (*types.Session, error)
}

type qaFeedbackMessages interface {
	GetMessage(context.Context, string, string) (*types.Message, error)
	GetMessagesByRequestIDs(context.Context, string, []string) ([]*types.MessageWithSession, error)
}

type qaFeedbackKnowledge interface {
	GetKnowledgeBatch(context.Context, uint64, []string) ([]*types.Knowledge, error)
}

// QAFeedbackHandler authorizes persisted chat feedback and forwards its DTOs to the local Host.
type QAFeedbackHandler struct {
	sessions       qaFeedbackSessions
	messages       qaFeedbackMessages
	knowledgeBases middleware.KBLookup
	knowledge      qaFeedbackKnowledge
	manage         func(context.Context, extensionbridge.ManagementRequest) (json.RawMessage, error)
}

// NewQAFeedbackHandler uses the native owner and provenance readers.
func NewQAFeedbackHandler(sessions qaFeedbackSessions, messages qaFeedbackMessages, knowledgeBases middleware.KBLookup, knowledge qaFeedbackKnowledge) *QAFeedbackHandler {
	return &QAFeedbackHandler{sessions: sessions, messages: messages, knowledgeBases: knowledgeBases, knowledge: knowledge, manage: extensionbridge.ManageLifecycle}
}

type qaFeedbackSubmission struct {
	OperationID    string `json:"operation_id"`
	Rating         string `json:"rating"`
	Text           string `json:"text"`
	Reason         string `json:"reason"`
	SnapshotDigest string `json:"snapshot_digest"`
}

type qaFeedbackReview struct {
	OperationID      string `json:"operation_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	Action           string `json:"action"`
}

type qaFeedbackPayload struct {
	SessionID string `json:"session_id"`
	MessageID string `json:"message_id"`
	Text      string `json:"text"`
	Reason    string `json:"reason"`
	Context   struct {
		Rating         string `json:"rating"`
		CommentPresent bool   `json:"comment_present,omitempty"`
		qaFeedbackSnapshot
	} `json:"feedback_context"`
	CitedChunkIDs []string `json:"cited_chunk_ids,omitempty"`
}

// OwnerFeedback previews or submits the same server-derived bounded snapshot.
func (h *QAFeedbackHandler) OwnerFeedback(c *gin.Context) {
	caller, ok := qaFeedbackCaller(c, types.TenantRoleViewer)
	if !ok {
		return
	}
	sessionID, messageID := c.Param("session_id"), c.Param("id")
	if !qaFeedbackID(sessionID) || !qaFeedbackID(messageID) {
		lifecycleError(c, http.StatusBadRequest)
		return
	}
	var in qaFeedbackSubmission
	if c.Request.Method == http.MethodPost && (!decodeQAFeedback(c, &in) || !in.valid()) {
		lifecycleError(c, http.StatusBadRequest)
		return
	}
	ctx := types.WithExecutionTenant(c.Request.Context(), caller.TenantID)
	session, err := h.sessions.GetOwnedSession(ctx, sessionID)
	if err != nil && !errors.Is(err, apperrors.ErrSessionNotFound) && !errors.Is(err, gorm.ErrRecordNotFound) {
		lifecycleError(c, http.StatusServiceUnavailable)
		return
	}
	if err != nil || session == nil || session.ID != sessionID || session.TenantID != caller.TenantID || session.UserID != caller.UserID {
		lifecycleError(c, http.StatusNotFound)
		return
	}
	message, err := h.messages.GetMessage(ctx, sessionID, messageID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		lifecycleError(c, http.StatusServiceUnavailable)
		return
	}
	if err != nil || message == nil || message.ID != messageID || message.SessionID != sessionID || message.Role != "assistant" ||
		!message.IsCompleted || message.RunState == types.MessageRunStateAwaitingInput || strings.TrimSpace(message.Content) == "" {
		lifecycleError(c, http.StatusNotFound)
		return
	}
	snapshot, kbID, citationIDs, err := h.feedbackSnapshot(ctx, caller.TenantID, message)
	if err != nil {
		lifecycleError(c, http.StatusServiceUnavailable)
		return
	}
	digest := qaFeedbackSnapshotDigest(caller.TenantID, sessionID, messageID, kbID, citationIDs, snapshot)
	if c.Request.Method == http.MethodGet {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": struct {
			qaFeedbackSnapshot
			SnapshotDigest string `json:"snapshot_digest"`
		}{qaFeedbackSnapshot: snapshot, SnapshotDigest: digest}})
		return
	}
	if in.SnapshotDigest != digest {
		lifecycleError(c, http.StatusConflict)
		return
	}
	payload := qaFeedbackPayload{SessionID: sessionID, MessageID: messageID, Reason: in.Reason, CitedChunkIDs: citationIDs}
	payload.Context.Rating = in.Rating
	payload.Context.qaFeedbackSnapshot = snapshot
	if snapshot.SnapshotRestricted {
		payload.Context.CommentPresent = strings.TrimSpace(in.Text) != ""
	} else {
		payload.Text = in.Text
	}
	req := qaFeedbackRequest(caller, "qa_feedback_submit")
	req.OperationID = in.OperationID
	req.KnowledgeBaseID = kbID
	h.forwardQAFeedback(c, req, struct {
		Payload qaFeedbackPayload `json:"payload"`
	}{Payload: payload})
}

// InboxFeedback lists tenant-wide unscoped submissions or records an administrative review.
func (h *QAFeedbackHandler) InboxFeedback(c *gin.Context) {
	caller, ok := qaFeedbackCaller(c, types.TenantRoleAdmin)
	if !ok {
		return
	}
	type inboxBusiness struct {
		ID      string `json:"id,omitempty"`
		Payload *struct {
			Decision string `json:"decision"`
		} `json:"payload,omitempty"`
	}
	if c.Request.Method == http.MethodGet {
		req := qaFeedbackRequest(caller, "qa_feedback_list")
		id, state := c.Query("id"), c.Query("state")
		page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
		pageSize, pageSizeErr := strconv.Atoi(c.DefaultQuery("page_size", "20"))
		if pageErr != nil || pageSizeErr != nil || page < 1 || page > 1000000 || pageSize < 1 || pageSize > 100 ||
			(id != "" && !qaFeedbackID(id)) || (state != "" && (len(state) > 32 || !qaFeedbackID(state))) {
			lifecycleError(c, http.StatusBadRequest)
			return
		}
		req.Page, req.PageSize, req.State = page, pageSize, state
		h.forwardQAFeedback(c, req, inboxBusiness{ID: id})
		return
	}
	id := c.Param("id")
	var in qaFeedbackReview
	if !qaFeedbackID(id) || !decodeQAFeedback(c, &in) || !qaFeedbackID(in.OperationID) || in.ExpectedRevision <= 0 {
		lifecycleError(c, http.StatusBadRequest)
		return
	}
	switch in.Action {
	case "feedback_record_only", "feedback_needs_evidence", "feedback_reject":
	default:
		lifecycleError(c, http.StatusBadRequest)
		return
	}
	req := qaFeedbackRequest(caller, "qa_feedback_review")
	req.OperationID, req.ExpectedRevision = in.OperationID, in.ExpectedRevision
	h.forwardQAFeedback(c, req, inboxBusiness{ID: id, Payload: &struct {
		Decision string `json:"decision"`
	}{Decision: in.Action}})
}

func qaFeedbackCaller(c *gin.Context, required types.TenantRole) (types.Caller, bool) {
	c.Header("Cache-Control", "no-store")
	ctx := c.Request.Context()
	caller := types.CallerFromContext(ctx)
	_, machine := types.TenantAPIKeyScopeFromContext(ctx)
	_, auditKey := types.AuditAPIKeyFromContext(ctx)
	if machine || auditKey {
		lifecycleError(c, http.StatusForbidden)
		return types.Caller{}, false
	}
	principal, ok := types.PrincipalFromContext(ctx)
	if types.PrincipalAuthSourceFromContext(ctx) != types.AuthSourceWeb || caller.TenantID == 0 || caller.UserID == "" ||
		!ok || principal.Type != types.PrincipalWebUser || principal.ID != caller.UserID || principal.ActingVia != "" {
		lifecycleError(c, http.StatusUnauthorized)
		return types.Caller{}, false
	}
	if !caller.Role.HasPermission(required) {
		lifecycleError(c, http.StatusForbidden)
		return types.Caller{}, false
	}
	return caller, true
}

func (in qaFeedbackSubmission) valid() bool {
	if !qaFeedbackID(in.OperationID) || len(in.Text) > qaFeedbackMaxText || strings.ContainsRune(in.Text, 0) ||
		(in.SnapshotDigest != "" && !qaFeedbackID(in.SnapshotDigest)) {
		return false
	}
	switch in.Rating {
	case "like", "dislike", "supplement", "correction":
	default:
		return false
	}
	switch in.Reason {
	case "", "inaccurate", "incomplete", "missing_sources", "other":
		return true
	default:
		return false
	}
}

func qaFeedbackID(id string) bool {
	if id == "" || len(id) > qaFeedbackMaxID {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.:", r)) {
			return false
		}
	}
	return true
}

func decodeQAFeedback(c *gin.Context, out any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10))
	decoder.DisallowUnknownFields()
	return decoder.Decode(out) == nil && decoder.Decode(new(any)) == io.EOF
}

func qaFeedbackRequest(caller types.Caller, action string) extensionbridge.ManagementRequest {
	return extensionbridge.ManagementRequest{
		Action: action, TenantID: caller.TenantID, ActorID: strconv.FormatUint(caller.TenantID, 10) + ":" + caller.UserID,
		CanWrite: caller.Role.HasPermission(types.TenantRoleAdmin),
	}
}

func (h *QAFeedbackHandler) forwardQAFeedback(c *gin.Context, req extensionbridge.ManagementRequest, business any) {
	var err error
	req.Business, err = json.Marshal(business)
	if err != nil {
		lifecycleError(c, http.StatusServiceUnavailable)
		return
	}
	data, err := h.manage(c.Request.Context(), req)
	if err != nil {
		lifecycleError(c, extensionbridge.ManagementStatus(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}
