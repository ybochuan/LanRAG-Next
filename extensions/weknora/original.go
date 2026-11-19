package weknora

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// OriginalBytes reads one scoped native original for the attachment workbench.
// The caller must separately verify the document metadata and its file hash.
func (c *Client) OriginalBytes(ctx context.Context, doc string, limit int64) ([]byte, error) {
	if c == nil || !ValidID(doc) || limit <= 0 {
		return nil, fmt.Errorf("invalid original document")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v1/knowledge/"+url.PathEscape(doc)+"/download", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("X-API-Key", c.key)
	request.Header.Set("X-Tenant-ID", strconv.FormatUint(c.tenant, 10))
	response, err := c.http.Do(request)
	if err != nil {
		return nil, classifyTransportError(ctx, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, &UpstreamError{Code: UpstreamHTTPError, Status: response.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(data) == 0 || int64(len(data)) > limit {
		return nil, fmt.Errorf("original is unavailable or exceeds the attachment limit")
	}
	return data, nil
}

// OriginalSHA256 hashes the scoped native download without retaining file bytes.
func (c *Client) OriginalSHA256(ctx context.Context, doc string) (string, error) {
	if c == nil || !ValidID(doc) {
		return "", &UpstreamError{Code: UpstreamInvalidResponse, detail: "invalid original document"}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v1/knowledge/"+url.PathEscape(doc)+"/download", nil)
	if err != nil {
		return "", &UpstreamError{Code: UpstreamInvalidResponse, detail: "invalid original request"}
	}
	request.Header.Set("X-API-Key", c.key)
	request.Header.Set("X-Tenant-ID", strconv.FormatUint(c.tenant, 10))
	response, err := c.http.Do(request)
	if err != nil {
		return "", classifyTransportError(ctx, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", &UpstreamError{Code: UpstreamHTTPError, detail: fmt.Sprintf("upstream HTTP status %d", response.StatusCode), Status: response.StatusCode}
	}
	const limit = 50 << 20 // Same maximum as parser input.
	if response.ContentLength > limit {
		return "", &UpstreamError{Code: UpstreamInvalidResponse, detail: "original exceeds parser size limit"}
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(response.Body, limit+1))
	if err != nil || n == 0 || n > limit {
		return "", &UpstreamError{Code: UpstreamInvalidResponse, detail: "invalid original download"}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
