package handler

import (
	"context"
	"crypto/subtle"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// ReclaimDeletedAttachment is called only by the authorized local attachment worker.
func (h *KnowledgeHandler) ReclaimDeletedAttachment(c *gin.Context) {
	ctx := c.Request.Context()
	_, machine := types.TenantAPIKeyScopeFromContext(ctx)
	host, _, remoteErr := net.SplitHostPort(c.Request.RemoteAddr)
	key := os.Getenv("AGENTRAG_MANAGEMENT_KEY")
	caller := types.CallerFromContext(ctx)
	configuredTenant, _ := strconv.ParseUint(os.Getenv("AGENTRAG_TENANT_ID"), 10, 64)
	if !machine || remoteErr != nil || !net.ParseIP(host).IsLoopback() || len(key) < 32 ||
		subtle.ConstantTimeCompare([]byte(c.GetHeader("X-AgentRAG-Management-Key")), []byte(key)) != 1 ||
		configuredTenant == 0 || caller.TenantID != configuredTenant || !strings.EqualFold(strings.TrimSpace(os.Getenv("DB_DRIVER")), "sqlite") {
		_ = c.Error(errors.NewForbiddenError("attachment worker authorization required"))
		return
	}
	var body struct {
		KB        string `json:"knowledge_base_id"`
		Hash      string `json:"file_hash"`
		Resources string `json:"resources"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4<<20)
	if err := c.ShouldBindJSON(&body); err != nil || len(body.Hash) != 32 || body.KB == "" {
		_ = c.Error(errors.NewBadRequestError("invalid attachment reclaim binding"))
		return
	}
	allowed := false
	for _, kb := range strings.Split(os.Getenv("AGENTRAG_KB_IDS"), ",") {
		allowed = allowed || strings.TrimSpace(kb) == body.KB
	}
	if !allowed {
		_ = c.Error(errors.NewForbiddenError("attachment knowledge base outside worker scope"))
		return
	}
	if err := requireTenantAPIKeyKnowledgeBases(ctx, body.KB); err != nil {
		_ = c.Error(err)
		return
	}
	worker, ok := h.kgService.(interface {
		ReclaimDeletedAttachment(context.Context, string, string, string, string) (string, error)
	})
	if !ok {
		_ = c.Error(errors.NewServiceUnavailableError("attachment reclaim unavailable"))
		return
	}
	status, err := worker.ReclaimDeletedAttachment(types.WithExecutionTenant(ctx, configuredTenant), body.KB, c.Param("id"), body.Hash, body.Resources)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"status": status}})
}
