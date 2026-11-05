package host

import (
	"context"
	"errors"

	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
	"github.com/ybochuan/LanRAG-Next/extensions/review"
	"github.com/ybochuan/LanRAG-Next/extensions/taggov"
)

// Unknown projection types must never be handed to the native tag projector.
func projectionDispatcher(tags, alerts func(context.Context, outbox.Item) error) func(context.Context, outbox.Item) error {
	return func(ctx context.Context, item outbox.Item) error {
		switch item.ProjectionType {
		case "tag_projection", taggov.FamilyProjectionType:
			if tags != nil {
				return tags(ctx, item)
			}
		case review.ProjectionType:
			if alerts != nil {
				return alerts(ctx, item)
			}
		}
		return errors.New("unsupported projection type or missing handler")
	}
}
