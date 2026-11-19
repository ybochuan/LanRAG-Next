package weknora

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// ReclaimDeletedAttachment asks Core to verify or retry the exact native original.
func (c *Client) ReclaimDeletedAttachment(ctx context.Context, key, kb, doc, hash string) (string, error) {
	if len(key) < 32 || !ValidID(kb) || !ValidID(doc) || len(hash) != 32 {
		return "", errors.New("invalid native file reclaim binding")
	}
	body, _ := json.Marshal(map[string]string{"knowledge_base_id": kb, "file_hash": hash})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v1/knowledge/"+url.PathEscape(doc)+"/attachment-reclaim", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("X-API-Key", c.key)
	request.Header.Set("X-Tenant-ID", strconv.FormatUint(c.tenant, 10))
	request.Header.Set("X-AgentRAG-Management-Key", key)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return "", classifyTransportError(ctx, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", &UpstreamError{Code: UpstreamHTTPError, Status: response.StatusCode}
	}
	var out struct {
		Success bool
		Data    struct{ Status string }
	}
	if json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&out) != nil || !out.Success ||
		(out.Data.Status != "removed" && out.Data.Status != "shared") {
		return "", errors.New("invalid native file reclaim result")
	}
	return out.Data.Status, nil
}
