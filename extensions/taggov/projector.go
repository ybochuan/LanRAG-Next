package taggov

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// Projector handles lifecycle tag intents using current facts rather than an old
// event payload. One application shares one Writer across all KBs and retries.
type Projector struct {
	db        *store.DB
	lifecycle *lifecycle.Store
	mapping   *Mapping
	writer    *Writer
}

// NewProjector does not own its dependencies or run a background worker.
func NewProjector(db *store.DB, lc *lifecycle.Store, mapping *Mapping, writer *Writer) *Projector {
	return &Projector{db, lc, mapping, writer}
}

// Handle is compatible with Outbox.Worker.Handler. Failures are recorded without
// changing lifecycle truth; Outbox remains responsible for retry/dead state.
func (p *Projector) Handle(ctx context.Context, item outbox.Item) (err error) {
	if p.db == nil || p.lifecycle == nil || p.mapping == nil || p.writer == nil {
		return errors.New("tag projector dependencies required")
	}
	scope := Scope{item.TenantID, item.KnowledgeBaseID, item.AggregateID}
	if !validScope(scope) || item.ID == "" || item.ProjectionType != "tag_projection" || item.AggregateType != "document" || item.CausationType != "lifecycle_event" {
		return errors.New("unsupported tag projection intent")
	}
	defer func() {
		if err == nil {
			return
		}
		// A handler deadline must not prevent its failure audit from being attempted.
		recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		message := []rune(err.Error())
		if len(message) > 2048 {
			message = message[:2048]
		}
		recordErr := p.db.Write(recordCtx, func(tx *sql.Tx) error {
			result, err := tx.ExecContext(recordCtx, `INSERT INTO ext_tag_sync_failures(outbox_id,tenant_id,knowledge_base_id,document_id,reason,occurred_at)
			SELECT outbox_id,tenant_id,knowledge_base_id,aggregate_id,?,? FROM ext_projection_outbox
			WHERE outbox_id=? AND tenant_id=? AND knowledge_base_id=? AND aggregate_id=?`, string(message), time.Now().UTC().UnixMilli(), item.ID, strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, scope.DocumentID)
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return errors.New("failure scope does not match stored outbox")
			}
			return nil
		})
		err = errors.Join(err, recordErr)
	}()
	lcScope := lifecycle.Scope{TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID, DocumentID: scope.DocumentID}
	var projectedEvent string
	err = p.writer.ApplyResolved(ctx, scope, "gov:state:", func(ctx context.Context) ([]Tag, error) {
		current, err := p.lifecycle.Current(ctx, lcScope)
		if err != nil {
			return nil, err
		}
		projectedEvent = current.LastEventID
		tag, err := p.mapping.Resolve(ctx, scope, "gov:state:"+string(current.State))
		if err != nil {
			return nil, err
		}
		return []Tag{tag}, nil
	})
	if err != nil {
		return err
	}
	current, err := p.lifecycle.Current(ctx, lcScope)
	if err != nil {
		return err
	}
	if current.LastEventID != projectedEvent {
		return errors.New("lifecycle truth changed during projection; retry required")
	}
	return nil
}
