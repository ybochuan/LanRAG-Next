package host

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/attachmentworkbench"
)

// attachmentNotifier turns the attachment work queue into native bell
// reminders. It keeps the last fingerprint in memory so an unchanged queue is
// not posted again during the same Host lifetime.
type attachmentNotifier struct {
	coreURL string
	key     string
	service *attachmentworkbench.Service
	scopes  []attachmentworkbench.Scope
	client  *http.Client
	sent    map[string]string
}

func newAttachmentNotifier(coreURL, key string, service *attachmentworkbench.Service, scopes []attachmentworkbench.Scope) *attachmentNotifier {
	if coreURL == "" || len(key) < 32 || service == nil || len(scopes) == 0 {
		return nil
	}
	return &attachmentNotifier{coreURL: coreURL, key: key, service: service, scopes: scopes,
		client: &http.Client{Timeout: 5 * time.Second}, sent: map[string]string{}}
}

func (n *attachmentNotifier) Run(ctx context.Context, report func(string)) {
	if n == nil {
		return
	}
	// Core may still be binding its port during Host startup.
	select {
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}
	n.sweep(ctx, report)
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.sweep(ctx, report)
		}
	}
}

func (n *attachmentNotifier) sweep(ctx context.Context, report func(string)) {
	for _, scope := range n.scopes {
		tasks, err := n.service.Tasks(ctx, scope)
		if err != nil {
			if ctx.Err() == nil && report != nil {
				report("attachment_task_scan_failed")
			}
			continue
		}
		if tasks.Total == 0 {
			delete(n.sent, scope.KB)
			continue
		}
		fingerprint := attachmentTaskFingerprint(tasks)
		if n.sent[scope.KB] == fingerprint {
			continue
		}
		if n.deliver(ctx, scope, tasks, fingerprint, report) {
			n.sent[scope.KB] = fingerprint
		}
	}
}

func attachmentTaskFingerprint(tasks attachmentworkbench.TaskList) string {
	parts := []string{}
	for _, doc := range tasks.Documents {
		for _, item := range doc.Items {
			parts = append(parts, doc.ParentID+":"+item.ID+":"+strings.Join(item.Reasons, ","))
		}
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:16])
}

func (n *attachmentNotifier) deliver(ctx context.Context, scope attachmentworkbench.Scope, tasks attachmentworkbench.TaskList, fingerprint string, report func(string)) bool {
	title := "文档工作台有附件待办"
	body := fmt.Sprintf("有 %d 个附件需要确认名称或正文引用，涉及 %d 份文档。点击打开文档工作台处理。", tasks.Total, len(tasks.Documents))
	if len(tasks.Documents) == 1 {
		name := tasks.Documents[0].ParentTitle
		if name == "" {
			name = tasks.Documents[0].ParentFileName
		}
		if name == "" {
			name = "该文档"
		}
		body = fmt.Sprintf("《%s》有 %d 个附件需要确认名称或正文引用。点击打开文档工作台处理。", name, tasks.Documents[0].Count)
	}
	link := "/platform/lifecycle?tab=reviews&kb=" + url.QueryEscape(scope.KB)
	if len(tasks.Documents) == 1 {
		link += "&document=" + url.QueryEscape(tasks.Documents[0].ParentID) + "&attachment=1"
	}
	payload, err := json.Marshal(map[string]any{
		"title": title, "content": body, "link": link, "kind": "lifecycle.attachment_due",
		"source_id": fingerprint, "knowledge_base_id": scope.KB, "tenant_id": scope.Tenant,
	})
	if err != nil {
		return false
	}
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return false
			case <-time.After(2 * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.coreURL+"/api/v1/notifications/internal/review-alert", bytes.NewReader(payload))
		if err != nil {
			return false
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-AgentRAG-Management-Key", n.key)
		resp, err := n.client.Do(req)
		if err != nil {
			continue
		}
		status := resp.StatusCode
		resp.Body.Close()
		if status == http.StatusOK {
			return true
		}
	}
	if report != nil {
		report("attachment_notification_failed")
	}
	return false
}
