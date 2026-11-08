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
