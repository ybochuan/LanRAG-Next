package business

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"

	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
)

// feedbackSamplePercent is the fast-path sampling rate. Sampling is
// deterministic by feedback ID so a retry cannot change whether an item is
// selected, and every selected item receives a durable review record.
var feedbackSamplePercent = 20

func feedbackSampleSelected(id string) bool {
	sum := sha256.Sum256([]byte(id))
	return binary.BigEndian.Uint32(sum[:4])%100 < uint32(feedbackSamplePercent)
}

func negativeFeedbackCount(ctx context.Context, tx *sql.Tx, scope provenance.Scope, topic string) (int, error) {
	var count int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM ext_feedback_events e
WHERE e.tenant_id=? AND e.knowledge_base_id=? AND e.topic=? AND e.negative=1
  AND e.occurred_at > COALESCE((SELECT MAX(r.recovered_at) FROM ext_feedback_topic_recoveries r
    WHERE r.tenant_id=? AND r.knowledge_base_id=? AND r.topic=?), '')`,
		args(scope, topic, scope.TenantID, scope.KnowledgeBaseID, topic)...).Scan(&count)
	return count, err
}

// SweepFeedbackActivations activates approved low/medium-risk feedback once
// its cooling period has elapsed. High risk remains an explicit,
// second-confirmation action. Topic-level negative feedback is checked in the
// same statement that selects candidates so a blocked row is not retried every
// tick.
func (s *Service) SweepFeedbackActivations(ctx context.Context, scopes []provenance.Scope) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	now := s.now().UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
	var failures []error
	for _, scope := range scopes {
		cursor := ""
		for {
			type candidate struct {
				id  string
				rev int64
			}
			var page []candidate
			err := s.db.View(ctx, func(tx *sql.Tx) error {
				rows, err := tx.QueryContext(ctx, `SELECT f.feedback_id,f.revision
FROM ext_feedback_staging f
WHERE f.tenant_id=? AND f.knowledge_base_id=? AND f.status='approved'
  AND f.risk IN ('low','medium') AND f.ready_at IS NOT NULL AND datetime(f.ready_at)<=datetime(?)
  AND (SELECT count(*) FROM ext_feedback_events e
       WHERE e.tenant_id=f.tenant_id AND e.knowledge_base_id=f.knowledge_base_id
         AND e.topic=f.topic AND e.negative=1
         AND e.occurred_at > COALESCE((SELECT MAX(r.recovered_at) FROM ext_feedback_topic_recoveries r
           WHERE r.tenant_id=f.tenant_id AND r.knowledge_base_id=f.knowledge_base_id AND r.topic=f.topic), '')) < 2
  AND f.feedback_id>?
ORDER BY f.feedback_id LIMIT 64`, args(scope, now, cursor)...)
				if err != nil {
					return err
				}
				defer rows.Close()
				for rows.Next() {
					var c candidate
					if err := rows.Scan(&c.id, &c.rev); err != nil {
						return err
					}
					page = append(page, c)
				}
				return rows.Err()
			})
			if err != nil {
				return errors.Join(append(failures, err)...)
			}
			for _, c := range page {
				cursor = c.id
				command := Command{
					Scope: scope, ActorID: "system:feedback-sweep", Action: "feedback_activate",
					OperationID: "feedback-auto-" + newID(), ID: c.id, ExpectedRevision: c.rev,
				}
				if _, err := s.Execute(ctx, command); err != nil {
					failures = append(failures, err)
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
			}
			if len(page) < 64 {
				break
			}
		}
	}
	return errors.Join(failures...)
}
