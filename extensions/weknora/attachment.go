package weknora

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode"
)

type attachmentDocument struct {
	ID       string                     `json:"id"`
	TenantID uint64                     `json:"tenant_id"`
	KBID     string                     `json:"knowledge_base_id"`
	Metadata map[string]json.RawMessage `json:"metadata"`
}

func (d attachmentDocument) value(key string) string {
	var v string
	_ = json.Unmarshal(d.Metadata[key], &v)
	return v
}
func (c *Client) attachmentScope(d attachmentDocument, kb string) bool {
	return ValidID(d.ID) && d.TenantID == c.tenant && d.KBID == kb
}

// AttachmentParent verifies the native parent scope and detects one-level children.
func (c *Client) AttachmentParent(ctx context.Context, kb, id string) (bool, error) {
	if !ValidID(kb) || !ValidID(id) {
		return false, errors.New("invalid attachment parent identity")
	}
	var out struct {
		Data attachmentDocument `json:"data"`
	}
	if err := c.JSON(ctx, "GET", "/api/v1/knowledge/"+url.PathEscape(id), nil, nil, &out); err != nil {
		return false, err
	}
	if out.Data.ID != id || !c.attachmentScope(out.Data, kb) {
		return false, errors.New("attachment parent scope mismatch")
	}
	return out.Data.value("is_attachment") == "true", nil
}

// UploadAttachment uses native file ingestion, forces relationship metadata and
// reconciles prior successful uploads before posting. It never retries a POST.
// Native dedup conflicts belonging to another parent are not modified or adopted.
func (c *Client) UploadAttachment(ctx context.Context, kb, parent, name string, data []byte) (string, error) {
	if !ValidID(kb) || !ValidID(parent) || name == "" || path.Base(name) != name || strings.ContainsAny(name, `\`) || strings.ContainsFunc(name, unicode.IsControl) || len(data) == 0 || len(data) >= 20<<20 {
		return "", errors.New("invalid attachment upload")
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	matches := func(d attachmentDocument) bool {
		return c.attachmentScope(d, kb) && d.value("is_attachment") == "true" && d.value("parent_document_id") == parent && d.value("attachment_sha256") == hash
	}
	for page := 1; page != 0; {
		var out struct {
			Pagination
			Data []attachmentDocument `json:"data"`
		}
		query := url.Values{"page": {strconv.Itoa(page)}, "page_size": {"100"}}
		if err := c.JSON(ctx, "GET", "/api/v1/knowledge-bases/"+url.PathEscape(kb)+"/knowledge", query, nil, &out); err != nil {
			return "", err
		}
		next, err := out.Pagination.Next(page, 100, len(out.Data))
		if err != nil {
			return "", err
		}
		for _, d := range out.Data {
			if !c.attachmentScope(d, kb) {
				return "", errors.New("attachment list scope mismatch")
			}
			if matches(d) {
				return d.ID, nil
			}
		}
		page = next
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	metadata, _ := json.Marshal(map[string]string{"is_attachment": "true", "parent_document_id": parent, "attachment_sha256": hash})
	if err := writer.WriteField("metadata", string(metadata)); err != nil {
		return "", errors.New("attachment metadata encoding failed")
	}
	file, err := writer.CreateFormFile("file", name)
	if err != nil {
		return "", errors.New("attachment filename encoding failed")
	}
	if _, err = file.Write(data); err != nil {
		return "", err
	}
	if err = writer.Close(); err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/v1/knowledge-bases/"+url.PathEscape(kb)+"/knowledge/file", &body)
	if err != nil {
		return "", errors.New("invalid attachment request")
	}
	request.Header.Set("X-API-Key", c.key)
	request.Header.Set("X-Tenant-ID", strconv.FormatUint(c.tenant, 10))
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errors.New("attachment upload outcome unknown")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(raw) > 4<<20 {
		return "", errors.New("attachment response unavailable")
	}
	var out struct {
		Success bool               `json:"success"`
		Code    string             `json:"code"`
		Data    attachmentDocument `json:"data"`
	}
	if json.Unmarshal(raw, &out) != nil || !matches(out.Data) {
		return "", errors.New("attachment response identity mismatch")
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 && out.Success {
		return out.Data.ID, nil
	}
	if response.StatusCode == 409 && out.Code == "duplicate_file" {
		return out.Data.ID, nil
	}
	return "", errors.New("attachment upload rejected")
}
