package host

import (
	"context"
	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
	"testing"
)

func TestProjectionDispatcherSeparatesReviewFromNativeTags(t *testing.T) {
	tags, reviews := 0, 0
	dispatch := projectionDispatcher(func(context.Context, outbox.Item) error { tags++; return nil }, func(context.Context, outbox.Item) error { reviews++; return nil })
	if err := dispatch(context.Background(), outbox.Item{ProjectionType: "review_alert"}); err != nil || tags != 0 || reviews != 1 {
		t.Fatalf("tags=%d reviews=%d err=%v", tags, reviews, err)
	}
	if err := dispatch(context.Background(), outbox.Item{ProjectionType: "tag_projection"}); err != nil || tags != 1 || reviews != 1 {
		t.Fatal(err)
	}
	if err := dispatch(context.Background(), outbox.Item{ProjectionType: "family_tag"}); err != nil || tags != 2 || reviews != 1 {
		t.Fatal("family projection did not use the shared tag handler", err)
	}
	if err := dispatch(context.Background(), outbox.Item{ProjectionType: "unknown"}); err == nil || tags != 2 || reviews != 1 {
		t.Fatal("unknown projection entered a handler")
	}
}
