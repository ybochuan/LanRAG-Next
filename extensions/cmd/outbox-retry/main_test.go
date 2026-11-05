package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func TestRetryOnlyRequeuesMatchingDeadProjection(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ext.db")
	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		return outbox.EnqueueTx(ctx, tx, outbox.Item{ID: "family-intent", TenantID: 7, KnowledgeBaseID: "kb", ProjectionType: "family_tag", AggregateType: "document", AggregateID: "doc", CausationType: "document_version", CausationID: "version", Payload: json.RawMessage(`{}`), CreatedAt: now})
	}); err != nil {
		t.Fatal(err)
	}
	queue := outbox.NewStore(db)
	item, err := queue.Claim(ctx, now, time.Minute)
	if err != nil || item == nil {
		t.Fatal("fixture claim failed", err)
	}
	if err := queue.Fail(ctx, item.ID, item.LeaseToken, now, errors.New("fixture failure"), time.Minute, 1); err != nil {
		t.Fatal(err)
	}
	if err := retry(path, outbox.Scope{TenantID: 7, KnowledgeBaseID: "other"}, item.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("wrong scope could retry projection", err)
	}
	scope := outbox.Scope{TenantID: 7, KnowledgeBaseID: "kb"}
	if err := retry(path, scope, item.ID); err != nil {
		t.Fatal(err)
	}
	retried, err := queue.Claim(ctx, time.Now().UTC(), time.Minute)
	if err != nil || retried == nil || retried.ID != item.ID || retried.RetryCount != 0 {
		t.Fatal("dead projection not reset for another attempt", err)
	}
	if err := retry(path, scope, item.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("command reset an active claim", err)
	}
}
