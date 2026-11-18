package host

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/cleanup"
)

// cleanupNotifier forwards cleanup reminders to the native notification bell.
// The cleanup sweep runs inside a SQLite write transaction, so Signal only
// enqueues; delivery happens on a background worker outside that transaction.
type cleanupNotifier struct {
	coreURL string
	key     string
	queue   chan cleanup.Notice
	client  *http.Client
}

func newCleanupNotifier(coreURL, key string) *cleanupNotifier {
	if coreURL == "" || len(key) < 32 {
		return nil
	}
	return &cleanupNotifier{coreURL: coreURL, key: key, queue: make(chan cleanup.Notice, 256), client: &http.Client{Timeout: 3 * time.Second}}
}

func (n *cleanupNotifier) Signal(notice cleanup.Notice) {
	if n == nil {
		return
	}
	select {
	case n.queue <- notice:
	default:
	}
}

func (n *cleanupNotifier) Run(ctx context.Context, report func(string)) {
	if n == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case notice := <-n.queue:
			n.deliver(ctx, notice, report)
		}
	}
}

func (n *cleanupNotifier) deliver(ctx context.Context, notice cleanup.Notice, report func(string)) {
	body, err := json.Marshal(map[string]any{
		"title":             notice.Title,
		"content":           notice.Body,
		"link":              notice.Link,
		"source_id":         notice.SourceID,
		"knowledge_base_id": notice.KnowledgeBaseID,
		"tenant_id":         notice.TenantID,
	})
	if err != nil {
		return
	}
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.coreURL+"/api/v1/notifications/internal/cleanup-alert", bytes.NewReader(body))
		if err != nil {
			return
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
			return
		}
	}
	if report != nil {
		report("cleanup_notification_failed")
	}
}
