package weknora

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// DocumentTitles reads titles through the existing scoped native batch API.
func (c *Client) DocumentTitles(ctx context.Context, kb string, ids []string) (map[string]string, error) {
	titles := make(map[string]string, len(ids))
	for offset := 0; offset < len(ids); offset += 100 {
		batch := ids[offset:min(offset+100, len(ids))]
		wanted := make(map[string]bool, len(batch))
		for _, id := range batch {
			wanted[id] = true
		}
		var out struct {
			Data []struct {
				ID     string `json:"id"`
				KB     string `json:"knowledge_base_id"`
				Tenant uint64 `json:"tenant_id"`
				Title  string `json:"title"`
			} `json:"data"`
		}
		if err := c.JSON(ctx, http.MethodGet, "/api/v1/knowledge/batch", url.Values{"kb_id": {kb}, "ids": batch}, nil, &out); err != nil {
			return nil, err
		}
		for _, d := range out.Data {
			if !wanted[d.ID] || d.KB != kb || d.Tenant != c.tenant {
				return nil, errors.New("document title scope mismatch")
			}
			titles[d.ID] = d.Title
		}
	}
	return titles, nil
}
