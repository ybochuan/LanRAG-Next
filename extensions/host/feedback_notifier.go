package host

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// feedbackNotifier forwards newly submitted review feedback to Core as a
// native notification. SubmitQAFeedback runs inside the management request
// path, so Signal only enqueues; delivery happens on a background worker and a
// failed notification never turns a stored feedback submission into an error.
type feedbackNotifier struct {
	coreURL string
	key     string
	queue   chan feedbackNotice
	client  *http.Client
}

type feedbackNotice struct {
	FeedbackID      string
	TenantID        uint64
	KnowledgeBaseID string
}

func newFeedbackNotifier(coreURL, key string) *feedbackNotifier {
	if coreURL == "" || len(key) < 32 {
		return nil
	}
	return &feedbackNotifier{coreURL: coreURL, key: key, queue: make(chan feedbackNotice, 256), client: &http.Client{Timeout: 3 * time.Second}}
}

// Signal enqueues a feedback notification without blocking the submit path.
func (n *feedbackNotifier) Signal(_ context.Context, tenantID uint64, knowledgeBaseID string, feedbackID string) {
	if n == nil {
		return
	}
	select {
	case n.queue <- feedbackNotice{FeedbackID: feedbackID, TenantID: tenantID, KnowledgeBaseID: knowledgeBaseID}:
	default:
	}
}

// Run drains queued notices until the context is cancelled.
func (n *feedbackNotifier) Run(ctx context.Context, report func(string)) {
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

func (n *feedbackNotifier) deliver(ctx context.Context, notice feedbackNotice, report func(string)) {
	body, err := json.Marshal(map[string]any{
		"feedback_id":       notice.FeedbackID,
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
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.coreURL+"/api/v1/notifications/internal/feedback-alert", bytes.NewReader(body))
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
		report("feedback_notification_failed")
	}
}
