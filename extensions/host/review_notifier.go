package host

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/review"
)

// reviewNotifier forwards new review alerts to Core as native notifications.
// The review delivery hook runs inside a SQLite write transaction and must
// never do network I/O, so Signal only enqueues; a background worker performs
// the HTTP call outside the transaction. Failure is logged, never returned:
// the notification is an enhancement, not a dependency of review delivery.
type reviewNotifier struct {
	coreURL string
	key     string
	queue   chan review.Alert
	client  *http.Client
}

func newReviewNotifier(coreURL, key string) *reviewNotifier {
	if coreURL == "" || len(key) < 32 {
		return nil
	}
	return &reviewNotifier{coreURL: coreURL, key: key, queue: make(chan review.Alert, 256), client: &http.Client{Timeout: 3 * time.Second}}
}

// Signal enqueues a new-alert notification without blocking the caller.
func (n *reviewNotifier) Signal(_ context.Context, a review.Alert) {
	if n == nil {
		return
	}
	select {
	case n.queue <- a:
	default:
	}
}

// Run drains the queue until the context is cancelled. It is not part of the
// outbox transaction and may be started alongside the other background tasks.
func (n *reviewNotifier) Run(ctx context.Context, report func(string)) {
	if n == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case a := <-n.queue:
			n.deliver(ctx, a, report)
		}
	}
}

func (n *reviewNotifier) deliver(ctx context.Context, a review.Alert, report func(string)) {
	body, err := json.Marshal(map[string]any{
		"title":             "文档复核到期",
		"content":           "知识库 " + a.KnowledgeBaseID + " 的文档 " + a.DocumentID + " 复核到期：" + a.ReviewDue.Format(time.RFC3339),
		"link":              "/platform/lifecycle?tab=reviews",
		"source_id":         a.AlertID,
		"knowledge_base_id": a.KnowledgeBaseID,
		"tenant_id":         a.TenantID,
	})
	if err != nil {
		return
	}
	// A startup sweep can fire before Core is ready. Retry a few times with
	// a short backoff; a final failure is only reported, never propagated.
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.coreURL+"/api/v1/notifications/internal/review-alert", bytes.NewReader(body))
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
		report("review_notification_failed")
	}
}
