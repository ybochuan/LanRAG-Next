// Package weknora contains independent DTOs and transport for the public API.
package weknora

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client is bound to one configured origin and tenant. It never follows redirects.
type Client struct {
	base   string
	key    string
	tenant uint64
	http   *http.Client
}

// NewClient permits HTTPS or local loopback HTTP. The base is an origin, not an
// API path. Credentials are sent in headers and excluded from all returned errors.
func NewClient(baseURL, apiKey string, tenantID uint64) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || u.User != nil || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("invalid upstream origin")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme == "http" && u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, errors.New("plaintext upstream requires loopback")
	}
	if tenantID == 0 || strings.TrimSpace(apiKey) == "" || strings.ContainsAny(apiKey, "\r\n") {
		return nil, errors.New("API key and tenant required")
	}
	return &Client{base: strings.TrimSuffix(u.String(), "/"), key: apiKey, tenant: tenantID, http: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// TenantID is the configured execution tenant, checked again against every DTO.
func (c *Client) TenantID() uint64 { return c.tenant }

// ValidID restricts IDs to a single unambiguous URL path segment.
func ValidID(id string) bool {
	return id != "" && strings.TrimSpace(id) == id && id != "." && id != ".." && !strings.ContainsAny(id, "/\\?#%\r\n")
}

// JSON performs one bounded request. It accepts only native success envelopes;
// HTTP/JSON/transport errors never include the response body, URL or credentials.
func (c *Client) JSON(ctx context.Context, method, path string, query url.Values, body, result any) error {
	if !strings.HasPrefix(path, "/api/v1/") {
		return errors.New("invalid API path")
	}
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return errors.New("invalid upstream request body")
		}
	}
	target := c.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	r, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(data))
	if err != nil {
		return errors.New("invalid upstream request")
	}
	r.Header.Set("X-API-Key", c.key)
	r.Header.Set("X-Tenant-ID", strconv.FormatUint(c.tenant, 10))
	r.Header.Set("Accept", "application/json")
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(r)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("upstream transport failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("upstream HTTP status %d", response.StatusCode)
	}
	const limit = 4 * 1024 * 1024
	data, err = io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("upstream body read failed")
	}
	if len(data) > limit {
		return errors.New("upstream response exceeds size limit")
	}
	var ack struct {
		Success bool `json:"success"`
	}
	if json.Unmarshal(data, &ack) != nil || !ack.Success {
		return errors.New("invalid upstream success envelope")
	}
	if err := json.Unmarshal(data, result); err != nil {
		return errors.New("invalid upstream response data")
	}
	return nil
}

// Pagination is the public page metadata. Pointers distinguish missing fields.
type Pagination struct {
	Total    *int64 `json:"total"`
	Page     *int   `json:"page"`
	PageSize *int   `json:"page_size"`
}

// Next checks exact cardinality and page identity before allowing pagination.
func (p Pagination) Next(requested, size, count int) (int, error) {
	if requested < 1 || requested > 10000 || size < 1 || size > 1000 || p.Total == nil || *p.Total < 0 || p.Page == nil || *p.Page != requested || p.PageSize == nil || *p.PageSize != size {
		return 0, errors.New("invalid upstream pagination")
	}
	offset := int64(requested-1) * int64(size)
	remaining := *p.Total - offset
	if remaining < 0 || int64(count) != min(int64(size), remaining) {
		return 0, errors.New("incomplete upstream page")
	}
	if remaining <= int64(size) {
		return 0, nil
	}
	if requested == 10000 {
		return 0, errors.New("upstream pagination limit exceeded")
	}
	return requested + 1, nil
}
