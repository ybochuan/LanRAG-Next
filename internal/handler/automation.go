package handler

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	stderrors "errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// AutomationHandler serves the caller's automations and notifications.
type AutomationHandler struct {
	automations    *service.AutomationService
	notifications  *service.NotificationService
	knowledgeBases interfaces.KnowledgeBaseService
	// Cleanup reminders must reach every tenant sharing the knowledge base.
	// Both dependencies are optional: a nil service degrades to the KB owner.
	shares interfaces.KBShareService
	orgs   interfaces.OrganizationService
}

// NewAutomationHandler builds the handler.
func NewAutomationHandler(
	automations *service.AutomationService, notifications *service.NotificationService, knowledgeBases interfaces.KnowledgeBaseService,
	shares interfaces.KBShareService, orgs interfaces.OrganizationService,
) *AutomationHandler {
	return &AutomationHandler{automations: automations, notifications: notifications, knowledgeBases: knowledgeBases, shares: shares, orgs: orgs}
}

func (h *AutomationHandler) fail(c *gin.Context, err error) {
	var appErr *errors.AppError
	if stderrors.As(err, &appErr) {
		_ = c.Error(appErr)
		return
	}
	logger.ErrorWithFields(c.Request.Context(), err, nil)
	_ = c.Error(errors.NewInternalServerError("automation request failed"))
}

// CreateAutomationRequest is the body of POST /automations; see
// types.AutomationPatch for the fields.
type CreateAutomationRequest struct {
	// What the automation is called
	Name string `json:"name" binding:"required" extensions:"x-agent"`
	// What it is for
	Description string `json:"description" extensions:"x-agent"`
	// Optional chat model from model.list; empty follows the assistant default
	ModelID string `json:"model_id" extensions:"x-agent"`
	// The task, sent as the user message of each run. May name now,
	// last_run_at, last_success_at, run_id or automation_name inside double
	// curly braces to have it filled in
	Instruction string `json:"instruction" binding:"required" extensions:"x-agent,x-agent-max-bytes=32000"`
	// When it runs
	Trigger types.AutomationTrigger `json:"trigger" binding:"required" extensions:"x-agent"`
	// per_run (default) starts a new conversation each run; persistent keeps one
	SessionMode string `json:"session_mode" enums:"per_run,persistent" extensions:"x-agent"`
	// Whether each per_run conversation gets the last successful answer
	CarryLastResult bool `json:"carry_last_result" extensions:"x-agent"`
	// What happens to runs missed while the service was down; default skip
	MisfirePolicy string `json:"misfire_policy" enums:"skip,run_once" extensions:"x-agent"`
	// What happens when a run is due while the previous one is still going; default skip
	OverlapPolicy string `json:"overlap_policy" enums:"skip,queue" extensions:"x-agent"`
	// How far runs may change data without asking; default confirm
	AccessLevel types.AssistantAccessLevel `json:"access_level" enums:"read_only,confirm,auto_write" extensions:"x-agent"`
	// How long a run waits for an approval before it fails, in minutes; default 1440
	ApprovalTimeoutMinutes int                    `json:"approval_timeout_minutes" extensions:"x-agent"`
	Limits                 types.AutomationLimits `json:"limits"`
	// Whether a successful run notifies the owner; failures always do
	NotifyOnSuccess bool `json:"notify_on_success" extensions:"x-agent"`
}

func (r CreateAutomationRequest) patch() *types.AutomationPatch {
	return &types.AutomationPatch{
		Name: &r.Name, Description: &r.Description, ModelID: &r.ModelID, Instruction: &r.Instruction,
		Trigger: &r.Trigger, SessionMode: &r.SessionMode, CarryLastResult: &r.CarryLastResult,
		MisfirePolicy: &r.MisfirePolicy, OverlapPolicy: &r.OverlapPolicy, AccessLevel: &r.AccessLevel,
		ApprovalTimeoutMinutes: &r.ApprovalTimeoutMinutes, Limits: &r.Limits, NotifyOnSuccess: &r.NotifyOnSuccess,
	}
}

// ListAutomations godoc
// @Summary      列出我的自动化
// @Description  列出当前用户在本空间创建的自动化（定时任务），最新的在前
// @Tags         自动化
// @Produce      json
// @Success      200  {object}  handler.DataResponse{data=[]types.Automation}  "自动化列表"
// @Security     Bearer
// @x-agent      {"id":"automation.list","risk":"read","max_items":100}
// @x-agent-hint "Lists the user's automations: scheduled tasks the WeKnora assistant runs on their behalf, with next and last run."
// @Router       /automations [get]
func (h *AutomationHandler) ListAutomations(c *gin.Context) {
	list, err := h.automations.List(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}

// CreateAutomation godoc
// @Summary      创建自动化
// @Description  创建一个按计划由 WeKnora 助手以当前用户身份运行的任务
// @Tags         自动化
// @Accept       json
// @Produce      json
// @Param        request  body      CreateAutomationRequest  true  "自动化"
// @Success      201      {object}  handler.DataResponse{data=types.Automation}  "创建的自动化"
// @Failure      400      {object}  errors.AppError  "参数不合法"
// @Security     Bearer
// @x-agent      {"id":"automation.create","role":"viewer","risk":"write","label_param":"name","always_confirm":true}
// @x-agent-hint "Creates an automation the WeKnora assistant runs as the user. If work requires other agents, name them and their assignments in the instruction so the assistant can delegate. Turn the user's wording into a cron expression (five fields, minute first) and an IANA timezone, and check it with automation.validate_trigger first, telling the user the next run times it returns. Runs are unattended: write the instruction as a complete task, since nobody can answer questions during a run. access_level defaults to confirm, where each change waits for the user's approval; only set auto_write when the user asks for it."
// @Router       /automations [post]
func (h *AutomationHandler) CreateAutomation(c *gin.Context) {
	var req CreateAutomationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(errors.NewBadRequestError("invalid automation").WithDetails(err.Error()))
		return
	}
	a, err := h.automations.Create(c.Request.Context(), req.patch())
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": a})
}

// ValidateTriggerRequest is the body of POST /automations/validate-trigger.
type ValidateTriggerRequest struct {
	// The trigger to check
	Trigger types.AutomationTrigger `json:"trigger" binding:"required" extensions:"x-agent"`
}

// ValidateTriggerResult is the data of POST /automations/validate-trigger.
type ValidateTriggerResult struct {
	// The next times the trigger fires, in UTC
	NextRuns []time.Time `json:"next_runs" extensions:"x-agent"`
}

// ValidateTrigger godoc
// @Summary      校验触发器
// @Description  校验 cron 或一次性触发器，返回未来 3 次触发时间
// @Tags         自动化
// @Accept       json
// @Produce      json
// @Param        request  body      ValidateTriggerRequest  true  "触发器"
// @Success      200      {object}  handler.DataResponse{data=ValidateTriggerResult}  "未来触发时间"
// @Failure      400      {object}  errors.AppError  "触发器不合法"
// @Security     Bearer
// @x-agent      {"id":"automation.validate_trigger","risk":"probe"}
// @x-agent-hint "Checks a trigger and returns its next three run times in UTC; convert them to the trigger's timezone for the user."
// @Router       /automations/validate-trigger [post]
func (h *AutomationHandler) ValidateTrigger(c *gin.Context) {
	var req ValidateTriggerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(errors.NewBadRequestError("invalid trigger").WithDetails(err.Error()))
		return
	}
	times, err := h.automations.ValidateTrigger(req.Trigger, 3)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": ValidateTriggerResult{NextRuns: times}})
}

// GetAutomationBySession godoc
// @Summary      按会话查找自动化
// @Description  返回这次会话所属的自动化，供会话页跳回
// @Tags         自动化
// @Produce      json
// @Param        session_id  path      string  true  "会话 ID"
// @Success      200         {object}  handler.DataResponse{data=types.Automation}  "自动化"
// @Failure      404         {object}  errors.AppError  "不存在"
// @Security     Bearer
// @Router       /automations/by-session/{session_id} [get]
func (h *AutomationHandler) GetAutomationBySession(c *gin.Context) {
	a, err := h.automations.FindBySession(c.Request.Context(), c.Param("session_id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": a})
}

// GetAutomation godoc
// @Summary      获取自动化
// @Tags         自动化
// @Produce      json
// @Param        id   path      string  true  "自动化 ID"  extensions(x-agent=automation_id)
// @Success      200  {object}  handler.DataResponse{data=types.Automation}  "自动化"
// @Failure      404  {object}  errors.AppError  "不存在"
// @Security     Bearer
// @x-agent      {"id":"automation.get","risk":"read"}
// @x-agent-hint "Returns one automation, including its instruction."
// @Router       /automations/{id} [get]
func (h *AutomationHandler) GetAutomation(c *gin.Context) {
	a, err := h.automations.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": a})
}

// UpdateAutomation godoc
// @Summary      修改自动化
// @Description  只修改请求中出现的字段，其余保持不变
// @Tags         自动化
// @Accept       json
// @Produce      json
// @Param        id       path      string                 true  "自动化 ID"  extensions(x-agent=automation_id)
// @Param        request  body      types.AutomationPatch  true  "要修改的字段"
// @Success      200      {object}  handler.DataResponse{data=types.Automation}  "修改后的自动化"
// @Failure      400      {object}  errors.AppError  "参数不合法"
// @Security     Bearer
// @x-agent      {"id":"automation.update","role":"viewer","risk":"write","target":"automation.get","label":"name","always_confirm":true}
// @x-agent-hint "Changes the listed fields of an automation; a field left out is kept. Check a new trigger with automation.validate_trigger first. Use automation.pause and automation.resume to switch it off and on."
// @Router       /automations/{id} [put]
func (h *AutomationHandler) UpdateAutomation(c *gin.Context) {
	var req types.AutomationPatch
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(errors.NewBadRequestError("invalid automation").WithDetails(err.Error()))
		return
	}
	a, err := h.automations.Update(c.Request.Context(), c.Param("id"), &req)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": a})
}

// DeleteAutomation godoc
// @Summary      删除自动化
// @Description  停止进行中的运行，删除自动化及其运行记录、会话和长期授权
// @Tags         自动化
// @Produce      json
// @Param        id   path      string  true  "自动化 ID"  extensions(x-agent=automation_id)
// @Success      200  {object}  handler.SuccessResponse  "删除成功"
// @Failure      404  {object}  errors.AppError  "不存在"
// @Security     Bearer
// @x-agent      {"id":"automation.delete","role":"viewer","risk":"destructive","target":"automation.get","label":"name"}
// @x-agent-hint "Deletes an automation with its run history and conversations. Cannot be undone; automation.pause keeps it."
// @Router       /automations/{id} [delete]
func (h *AutomationHandler) DeleteAutomation(c *gin.Context) {
	if err := h.automations.Delete(c.Request.Context(), c.Param("id")); err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// PauseAutomation godoc
// @Summary      暂停自动化
// @Description  停止按计划触发；进行中的运行不受影响
// @Tags         自动化
// @Produce      json
// @Param        id   path      string  true  "自动化 ID"  extensions(x-agent=automation_id)
// @Success      200  {object}  handler.DataResponse{data=types.Automation}  "暂停后的自动化"
// @Security     Bearer
// @x-agent      {"id":"automation.pause","role":"viewer","risk":"write","target":"automation.get","label":"name"}
// @x-agent-hint "Stops an automation from running on schedule until automation.resume."
// @Router       /automations/{id}/pause [post]
func (h *AutomationHandler) PauseAutomation(c *gin.Context) {
	a, err := h.automations.Pause(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": a})
}

// ResumeAutomation godoc
// @Summary      恢复自动化
// @Description  从现在起重新按计划触发，并清零连续失败计数
// @Tags         自动化
// @Produce      json
// @Param        id   path      string  true  "自动化 ID"  extensions(x-agent=automation_id)
// @Success      200  {object}  handler.DataResponse{data=types.Automation}  "恢复后的自动化"
// @Security     Bearer
// @x-agent      {"id":"automation.resume","role":"viewer","risk":"write","target":"automation.get","label":"name","always_confirm":true}
// @x-agent-hint "Schedules a paused automation again from now; missed runs are not caught up."
// @Router       /automations/{id}/resume [post]
func (h *AutomationHandler) ResumeAutomation(c *gin.Context) {
	a, err := h.automations.Resume(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": a})
}

// RunAutomation godoc
// @Summary      立即运行自动化
// @Description  排队一次手动运行；已有运行在排队或进行中时拒绝
// @Tags         自动化
// @Produce      json
// @Param        id   path      string  true  "自动化 ID"  extensions(x-agent=automation_id)
// @Success      202  {object}  handler.DataResponse{data=types.AutomationRun}  "排队的运行"
// @Failure      409  {object}  errors.AppError  "已有运行"
// @Security     Bearer
// @x-agent      {"id":"automation.run_now","role":"viewer","risk":"write","target":"automation.get","label":"name","always_confirm":true}
// @x-agent-hint "Queues a run now, in the background; follow it with automation.list_runs."
// @Router       /automations/{id}/run [post]
func (h *AutomationHandler) RunAutomation(c *gin.Context) {
	run, err := h.automations.RunNow(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": run})
}

// ListAutomationRuns godoc
// @Summary      列出运行记录
// @Tags         自动化
// @Produce      json
// @Param        id         path      string  true   "自动化 ID"  extensions(x-agent=automation_id)
// @Param        page       query     int     false  "页码，从 1 开始"  extensions(x-agent)
// @Param        page_size  query     int     false  "每页条数，最多 100"  extensions(x-agent,x-agent-maximum=20)
// @Success      200        {object}  handler.PageResponse{data=[]types.AutomationRun}  "运行记录"
// @Security     Bearer
// @x-agent      {"id":"automation.list_runs","risk":"read"}
// @x-agent-hint "Lists an automation's runs, newest first, with status, error and the start of the answer."
// @Router       /automations/{id}/runs [get]
func (h *AutomationHandler) ListAutomationRuns(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	page = max(page, 1)
	if size <= 0 || size > 100 {
		size = 20
	}
	runs, total, err := h.automations.ListRuns(c.Request.Context(), c.Param("id"), size, (page-1)*size)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": runs, "total": total, "page": page, "page_size": size})
}

// GetAutomationRun godoc
// @Summary      获取运行记录
// @Tags         自动化
// @Produce      json
// @Param        id   path      string  true  "运行 ID"
// @Success      200  {object}  handler.DataResponse{data=types.AutomationRun}  "运行记录"
// @Security     Bearer
// @Router       /automation-runs/{id} [get]
func (h *AutomationHandler) GetAutomationRun(c *gin.Context) {
	run, err := h.automations.GetRun(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": run})
}

// CancelAutomationRun godoc
// @Summary      停止运行
// @Description  排队或等待审批的运行立即结束；执行中的运行在下一次检查时停止
// @Tags         自动化
// @Produce      json
// @Param        id   path      string  true  "运行 ID"
// @Success      200  {object}  handler.DataResponse{data=types.AutomationRun}  "运行记录"
// @Security     Bearer
// @Router       /automation-runs/{id}/cancel [post]
func (h *AutomationHandler) CancelAutomationRun(c *gin.Context) {
	run, err := h.automations.CancelRun(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": run})
}

// ListAutomationGrants godoc
// @Summary      列出自动化的长期授权
// @Description  列出以后的运行无需确认即可执行的调用，以及失效原因
// @Tags         自动化
// @Produce      json
// @Param        id   path      string  true  "自动化 ID"
// @Success      200  {object}  map[string]interface{}  "长期授权"
// @Security     Bearer
// @Router       /automations/{id}/grants [get]
func (h *AutomationHandler) ListAutomationGrants(c *gin.Context) {
	grants, err := h.automations.Grants(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": grants})
}

// ListAutomationGrantCandidates godoc
// @Summary      列出可设为长期授权的调用
// @Description  列出本自动化的运行请求过审批、尚未长期授权的调用；需每次审批的调用不在其中
// @Tags         自动化
// @Produce      json
// @Param        id   path      string  true  "自动化 ID"
// @Success      200  {object}  map[string]interface{}  "候选调用"
// @Security     Bearer
// @Router       /automations/{id}/grant-candidates [get]
func (h *AutomationHandler) ListAutomationGrantCandidates(c *gin.Context) {
	candidates, err := h.automations.GrantCandidates(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": candidates})
}

type addAutomationGrantRequest struct {
	PromptID string `json:"prompt_id" binding:"required"`
}

// AddAutomationGrant godoc
// @Summary      添加长期授权
// @Description  让以后的运行无需确认即可执行某次运行请求过审批的调用；Agent 配置或所有者角色变化后失效
// @Tags         自动化
// @Accept       json
// @Produce      json
// @Param        id       path      string  true  "自动化 ID"
// @Param        request  body      object  true  "{prompt_id}"
// @Success      200      {object}  map[string]interface{}  "授权"
// @Security     Bearer
// @Router       /automations/{id}/grants [post]
func (h *AutomationHandler) AddAutomationGrant(c *gin.Context) {
	var req addAutomationGrantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(errors.NewBadRequestError("prompt_id is required"))
		return
	}
	grant, err := h.automations.AddGrant(c.Request.Context(), c.Param("id"), req.PromptID)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": grant})
}

// ListNotifications godoc
// @Summary      列出我的通知
// @Tags         通知
// @Produce      json
// @Param        limit   query     int   false  "条数，最多 100"
// @Param        cursor  query     string  false  "上一页返回的 next_cursor"
// @Param        unread  query     bool  false  "只看未读"
// @Success      200     {object}  map[string]interface{}  "{items, pending, unread, read_count, next_cursor}"
// @Security     Bearer
// @Router       /notifications [get]
func (h *AutomationHandler) ListNotifications(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	page, err := h.notifications.List(c.Request.Context(), limit, c.Query("unread") == "true", c.Query("cursor"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": page})
}

type markNotificationsReadRequest struct {
	IDs []string `json:"ids"`
}

// MarkNotificationsRead godoc
// @Summary      标记通知已读
// @Description  ids 为空时将全部通知记录标为已读，不改变待处理事项
// @Tags         通知
// @Accept       json
// @Produce      json
// @Param        request  body      object  false  "{ids}"
// @Success      200      {object}  map[string]interface{}  "结果"
// @Security     Bearer
// @Router       /notifications/read [post]
func (h *AutomationHandler) MarkNotificationsRead(c *gin.Context) {
	var req markNotificationsReadRequest
	_ = c.ShouldBindJSON(&req)
	if err := h.notifications.MarkRead(c.Request.Context(), req.IDs); err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ClearReadNotifications removes read notification history without changing work.
// @Summary      清理已读通知
// @Tags         通知
// @Produce      json
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /notifications/read [delete]
func (h *AutomationHandler) ClearReadNotifications(c *gin.Context) {
	if err := h.notifications.ClearRead(c.Request.Context()); err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

const maxDownloadNotificationBytes = 256 << 20

var downloadNotificationIDPattern = regexp.MustCompile("^[A-Za-z0-9_-]{1,36}$")

// downloadNotificationRequest is the desktop client's record of a business
// file it saved locally. Tenant and user always come from the authenticated
// caller, never this body.
type downloadNotificationRequest struct {
	FileName    string `json:"file_name"`
	SourceType  string `json:"source_type"`
	SourceID    string `json:"source_id"`
	SourceTitle string `json:"source_title"`
	Link        string `json:"link"`
	DownloadID  string `json:"download_id"`
	FileSize    int64  `json:"file_size"`
}

// CreateDownloadNotification godoc
// @Summary      记录一次业务文件下载
// @Description  桌面端在文件保存成功后调用；同一 download_id 幂等
// @Tags         通知
// @Accept       json
// @Produce      json
// @Param        request  body      downloadNotificationRequest  true  "下载记录"
// @Success      200      {object}  map[string]interface{}  "{id, created_at, duplicate}"
// @Security     Bearer
// @Router       /notifications/download [post]
func (h *AutomationHandler) CreateDownloadNotification(c *gin.Context) {
	caller := types.CallerFromContext(c.Request.Context())
	if caller.TenantID == 0 || strings.TrimSpace(caller.UserID) == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "unauthorized"})
		return
	}
	var req downloadNotificationRequest
	dec := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&req) != nil || dec.Decode(new(any)) != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid_request"})
		return
	}
	fileName := sanitizeDownloadFileName(req.FileName)
	sourceID := strings.TrimSpace(req.SourceID)
	downloadID := strings.TrimSpace(req.DownloadID)
	link, linkOK := validDownloadNotificationLink(req.Link)
	valid := strings.TrimSpace(req.FileName) != "" &&
		(req.SourceType == "knowledge_document" || req.SourceType == "attachment" || req.SourceType == "artifact") &&
		downloadNotificationIDPattern.MatchString(sourceID) &&
		downloadNotificationIDPattern.MatchString(downloadID) &&
		len([]rune(strings.TrimSpace(req.SourceTitle))) <= 255 &&
		linkOK &&
		req.FileSize >= 0 && req.FileSize <= maxDownloadNotificationBytes
	if !valid {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid_request"})
		return
	}
	if h.notifications == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "notification_unavailable"})
		return
	}
	body := ""
	if sourceTitle := strings.TrimSpace(req.SourceTitle); sourceTitle != "" {
		body = "来源：" + sourceTitle
	}
	notification, created, err := h.notifications.NotifyDownloadOnce(c.Request.Context(), &types.Notification{
		ID:         downloadID,
		TenantID:   caller.TenantID,
		UserID:     strings.TrimSpace(caller.UserID),
		Kind:       types.NotificationDownloadCompleted,
		Title:      fileName,
		Body:       body,
		Link:       link,
		SourceType: "download",
		SourceID:   downloadID,
	})
	if err != nil {
		logger.Warnf(c.Request.Context(), "Record download notification %s: %v", downloadID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "notification_unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"id": notification.ID, "created_at": notification.CreatedAt, "duplicate": !created,
	}})
}

func validDownloadNotificationLink(raw string) (string, bool) {
	link := strings.TrimSpace(raw)
	if link == "" {
		return "", true
	}
	if len(link) > 512 {
		return "", false
	}
	parsed, err := url.Parse(link)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil {
		return "", false
	}
	if !strings.HasPrefix(parsed.Path, "/platform/") || strings.HasPrefix(parsed.Path, "/platform//") {
		return "", false
	}
	return link, true
}

// reviewAlertNotification is the narrow DTO the trusted Host uses to ask Core
// to surface a review reminder in the native notification bell. The caller
// cannot choose a user: the recipient is resolved from the KB owner.
type reviewAlertNotification struct {
	Title           string `json:"title"           binding:"required,max=250"`
	Content         string `json:"content"         binding:"max=2000"`
	Link            string `json:"link"            binding:"required,max=512"`
	SourceID        string `json:"source_id"       binding:"required,max=64"`
	KnowledgeBaseID string `json:"knowledge_base_id" binding:"required,max=64"`
	TenantID        uint64 `json:"tenant_id"       binding:"required"`
	// Kind is empty for document review alerts. The attachment work queue uses
	// lifecycle.attachment_due so the bell can label it independently.
	Kind string `json:"kind,omitempty"`
}

func (h *AutomationHandler) NotifyReviewAlert(c *gin.Context) {
	key := os.Getenv("AGENTRAG_MANAGEMENT_KEY")
	if len(key) < 32 || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-AgentRAG-Management-Key")), []byte(key)) != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "unauthorized"})
		return
	}
	var req reviewAlertNotification
	dec := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&req) != nil || dec.Decode(new(any)) != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid_request"})
		return
	}
	if !strings.HasPrefix(req.Link, "/platform/lifecycle") || req.SourceID == "" || req.KnowledgeBaseID == "" || req.TenantID == 0 || req.Title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid_request"})
		return
	}
	kind, sourceType := "lifecycle.review_due", "review_alert"
	if req.Kind != "" {
		if req.Kind != "lifecycle.attachment_due" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid_request"})
			return
		}
		kind, sourceType = req.Kind, "attachment_alert"
	}
	ctx := c.Request.Context()
	kb, err := h.knowledgeBases.GetKnowledgeBaseByIDOnly(ctx, req.KnowledgeBaseID)
	if err != nil || kb == nil || kb.TenantID != req.TenantID || kb.CreatorID == "" {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "knowledge_base_not_found"})
		return
	}
	notify := h.notifications.Notify
	if kind == "lifecycle.attachment_due" {
		notify = h.notifications.NotifyOnce
	}
	notify(ctx, &types.Notification{
		TenantID:   req.TenantID,
		UserID:     kb.CreatorID,
		Kind:       kind,
		Title:      req.Title,
		Body:       req.Content,
		Link:       req.Link,
		SourceType: sourceType,
		SourceID:   req.SourceID,
	})
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// NotifyCleanupAlert is the trusted Host's cleanup-reminder path into the
// native notification bell. It is intentionally separate from review alerts so
// the bell can label and filter the two kinds independently.
func (h *AutomationHandler) NotifyCleanupAlert(c *gin.Context) {
	key := os.Getenv("AGENTRAG_MANAGEMENT_KEY")
	if len(key) < 32 || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-AgentRAG-Management-Key")), []byte(key)) != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "unauthorized"})
		return
	}
	var req reviewAlertNotification
	dec := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&req) != nil || dec.Decode(new(any)) != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid_request"})
		return
	}
	if !strings.HasPrefix(req.Link, "/platform/documents") || req.SourceID == "" || req.KnowledgeBaseID == "" || req.TenantID == 0 || req.Title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid_request"})
		return
	}
	ctx := c.Request.Context()
	kb, err := h.knowledgeBases.GetKnowledgeBaseByIDOnly(ctx, req.KnowledgeBaseID)
	if err != nil || kb == nil || kb.TenantID != req.TenantID || kb.CreatorID == "" {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "knowledge_base_not_found"})
		return
	}
	notified := map[string]bool{}
	notify := func(tenant uint64, user string) {
		if tenant == 0 || user == "" {
			return
		}
		key := strconv.FormatUint(tenant, 10) + ":" + user
		if notified[key] {
			return
		}
		notified[key] = true
		h.notifications.Notify(ctx, &types.Notification{
			TenantID:   tenant,
			UserID:     user,
			Kind:       "lifecycle.cleanup_due",
			Title:      req.Title,
			Body:       req.Content,
			Link:       req.Link,
			SourceType: "cleanup_alert",
			SourceID:   req.SourceID,
		})
	}
	for _, recipient := range h.cleanupRecipients(ctx, req, kb, h.resolveSharedTenants) {
		notify(recipient.tenant, recipient.user)
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

type cleanupRecipient struct {
	tenant uint64
	user   string
}

// cleanupRecipients resolves the KB owner plus one representative per tenant
// sharing the KB. Missing services degrade to owner-only, never to no recipient.
func (h *AutomationHandler) cleanupRecipients(ctx context.Context, req reviewAlertNotification, kb *types.KnowledgeBase, shared func(context.Context, uint64, string) []cleanupRecipient) []cleanupRecipient {
	seen := map[string]bool{}
	out := []cleanupRecipient{}
	add := func(tenant uint64, user string) {
		if tenant == 0 || user == "" {
			return
		}
		key := strconv.FormatUint(tenant, 10) + ":" + user
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, cleanupRecipient{tenant: tenant, user: user})
	}
	add(req.TenantID, kb.CreatorID)
	if shared != nil {
		for _, recipient := range shared(ctx, req.TenantID, req.KnowledgeBaseID) {
			add(recipient.tenant, recipient.user)
		}
	}
	return out
}

// resolveSharedTenants is the production resolver; it is nil-safe and never
// blocks the owner notification when share metadata is unavailable.
func (h *AutomationHandler) resolveSharedTenants(ctx context.Context, tenant uint64, kbID string) []cleanupRecipient {
	if h.shares == nil || h.orgs == nil {
		return nil
	}
	shares, err := h.shares.ListSharesByKnowledgeBase(ctx, kbID, tenant)
	if err != nil {
		return nil
	}
	out := []cleanupRecipient{}
	for _, share := range shares {
		if share == nil || share.OrganizationID == "" {
			continue
		}
		members, err := h.orgs.ListTenantMembers(ctx, share.OrganizationID)
		if err != nil {
			continue
		}
		for _, member := range members {
			if member == nil || member.TenantID == tenant || member.RepresentativeUserID == "" {
				continue
			}
			out = append(out, cleanupRecipient{tenant: member.TenantID, user: member.RepresentativeUserID})
		}
	}
	return out
}
