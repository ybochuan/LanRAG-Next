package handler

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/types"
)

// feedbackAlertNotification is the Host's minimal feedback-review notice. Core
// derives the title, body and link from the KB, so the caller never supplies
// notification text and the user's private feedback comment cannot leak here.
type feedbackAlertNotification struct {
	FeedbackID      string `json:"feedback_id"        binding:"required,max=64"`
	KnowledgeBaseID string `json:"knowledge_base_id"  binding:"required,max=64"`
	TenantID        uint64 `json:"tenant_id"          binding:"required"`
}

// NotifyFeedbackAlert asks Core to show a pending-review feedback in the
// owner's native notification bell. Only the KB owner is notified; the link
// opens the review tab of the feedback-documents workbench.
func (h *AutomationHandler) NotifyFeedbackAlert(c *gin.Context) {
	key := os.Getenv("AGENTRAG_MANAGEMENT_KEY")
	if len(key) < 32 || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-AgentRAG-Management-Key")), []byte(key)) != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "unauthorized"})
		return
	}
	var req feedbackAlertNotification
	dec := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&req) != nil || dec.Decode(new(any)) != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid_request"})
		return
	}
	if req.FeedbackID == "" || req.KnowledgeBaseID == "" || req.TenantID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid_request"})
		return
	}
	ctx := c.Request.Context()
	kb, err := h.knowledgeBases.GetKnowledgeBaseByIDOnly(ctx, req.KnowledgeBaseID)
	if err != nil || kb == nil || kb.TenantID != req.TenantID || kb.CreatorID == "" {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "knowledge_base_not_found"})
		return
	}
	name := kb.Name
	if name == "" {
		name = kb.ID
	}
	h.notifications.Notify(ctx, &types.Notification{
		TenantID:   req.TenantID,
		UserID:     kb.CreatorID,
		Kind:       "feedback.review_pending",
		Title:      "新的问答反馈待审核",
		Body:       "知识库「" + name + "」收到一条新的问答反馈，请前往反馈资料审核。",
		Link:       "/platform/documents?view=feedback-materials&feedbackTab=review&kb=" + url.QueryEscape(kb.ID),
		SourceType: "feedback_alert",
		SourceID:   req.FeedbackID,
	})
	c.JSON(http.StatusOK, gin.H{"success": true})
}
