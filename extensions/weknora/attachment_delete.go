package weknora

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// DeleteAttachment checks the live Core for the parent in the authorized KB,
// then asks the native async cleanup pipeline to delete the exact child ID.
// Acceptance is not evidence that the child has been removed.
func (c *Client) DeleteAttachment(ctx context.Context, kb, parent, child string) error {
	if !ValidID(kb) || !ValidID(parent) || !ValidID(child) || parent == child {
		return errors.New("invalid attachment deletion scope")
	}
	var parents struct {
		Data []attachmentDocument `json:"data"`
	}
	query := url.Values{"ids": {parent}, "kb_id": {kb}}
	if err := c.JSON(ctx, http.MethodGet, "/api/v1/knowledge/batch", query, nil, &parents); err != nil {
		return err
	}
	if len(parents.Data) != 0 {
		return errors.New("native Core still reports the attachment parent")
	}
	var submitted struct {
		Data struct {
			TaskID string `json:"task_id"`
		} `json:"data"`
	}
	if err := c.JSON(ctx, http.MethodDelete, "/api/v1/knowledge/"+url.PathEscape(child), nil, nil, &submitted); err != nil {
		return err
	}
	if submitted.Data.TaskID == "" {
		return errors.New("native attachment deletion outcome unknown")
	}
	return nil
}
