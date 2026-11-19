package weknora

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	ID           string                     `json:"id"`
	TenantID     uint64                     `json:"tenant_id"`
	KBID         string                     `json:"knowledge_base_id"`
	Title        string                     `json:"title"`
	FileHash     string                     `json:"file_hash"`
	ParseStatus  string                     `json:"parse_status"`
	EnableStatus string                     `json:"enable_status"`
	Metadata     map[string]json.RawMessage `json:"metadata"`
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
func (c *Client) AttachmentParent(ctx context.Context, kb, id string) (bool, string, string, error) {
	if !ValidID(kb) || !ValidID(id) {
		return false, "", "", errors.New("invalid attachment parent identity")
	}
	var out struct {
		Data attachmentDocument `json:"data"`
	}
	if err := c.JSON(ctx, "GET", "/api/v1/knowledge/"+url.PathEscape(id), nil, nil, &out); err != nil {
		return false, "", "", err
	}
	if out.Data.ID != id || !c.attachmentScope(out.Data, kb) {
		return false, "", "", errors.New("attachment parent scope mismatch")
	}
	if out.Data.ParseStatus == "deleting" {
		return false, "", "", errors.New("attachment parent is deleting")
	}
	return out.Data.value("is_attachment") == "true", out.Data.Title, out.Data.FileHash, nil
}

// UploadAttachment uses native file ingestion, forces relationship metadata and
// reconciles prior successful uploads before posting. It never retries a POST.
// Native dedup conflicts belonging to another parent are not modified or adopted.
func (c *Client) UploadAttachment(ctx context.Context, kb, parent, name string, data []byte) (string, error) {
	id, _, err := c.uploadAttachment(ctx, kb, parent, name, data, "", nil)
	return id, err
}

// UploadManagedAttachment reconciles a scoped import receipt before making the
// one POST reserved by claim. Existing legacy attachments remain unmanaged.
func (c *Client) UploadManagedAttachment(ctx context.Context, kb, parent, name string, data []byte, importID string, claim func() error) (string, bool, error) {
	if importID == "" || claim == nil {
		return "", false, errors.New("missing attachment import reservation")
	}
	return c.uploadAttachment(ctx, kb, parent, name, data, importID, claim)
}

func (c *Client) uploadAttachment(ctx context.Context, kb, parent, name string, data []byte, importID string, claim func() error) (string, bool, error) {
	if !ValidID(kb) || !ValidID(parent) || name == "" || path.Base(name) != name || strings.ContainsAny(name, `\`) || strings.ContainsFunc(name, unicode.IsControl) || len(data) == 0 || len(data) >= 20<<20 {
		return "", false, errors.New("invalid attachment upload")
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	fileHash := fmt.Sprintf("%x", md5.Sum(data)) // Match the native file revision field.
	matches := func(d attachmentDocument) bool {
		return c.attachmentScope(d, kb) && d.ID != parent && d.ParseStatus != "deleting" && d.value("is_attachment") == "true" &&
			d.value("parent_document_id") == parent && d.value("attachment_sha256") == hash && (importID == "" || d.FileHash == fileHash)
	}
	var existing attachmentDocument
	for page := 1; page != 0; {
		var out struct {
			Pagination
			Data []attachmentDocument `json:"data"`
		}
		query := url.Values{"page": {strconv.Itoa(page)}, "page_size": {"100"}}
		if err := c.JSON(ctx, "GET", "/api/v1/knowledge-bases/"+url.PathEscape(kb)+"/knowledge", query, nil, &out); err != nil {
			return "", false, err
		}
		next, err := out.Pagination.Next(page, 100, len(out.Data))
		if err != nil {
			return "", false, err
		}
		for _, d := range out.Data {
			if !c.attachmentScope(d, kb) {
				return "", false, errors.New("attachment list scope mismatch")
			}
			if matches(d) {
				if existing.ID != "" {
					return "", false, errors.New("ambiguous attachment receipt")
				}
				existing = d
			}
		}
		page = next
	}
	if existing.ID != "" {
		receipt := existing.value("attachment_import_id")
		if importID != "" && receipt != "" && receipt != importID {
			return "", false, errors.New("attachment receipt belongs to a different import")
		}
		return existing.ID, importID != "" && receipt == importID, nil
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fields := map[string]string{"is_attachment": "true", "parent_document_id": parent, "attachment_sha256": hash}
	if importID != "" {
		fields["attachment_import_id"] = importID
	}
	metadata, _ := json.Marshal(fields)
	if err := writer.WriteField("metadata", string(metadata)); err != nil {
		return "", false, errors.New("attachment metadata encoding failed")
	}
	if err := writer.WriteField("fileName", AttachmentFolder+"/"+name); err != nil {
		return "", false, errors.New("attachment folder encoding failed")
	}
	file, err := writer.CreateFormFile("file", name)
	if err != nil {
		return "", false, errors.New("attachment filename encoding failed")
	}
	if _, err = file.Write(data); err != nil {
		return "", false, err
	}
	if err = writer.Close(); err != nil {
		return "", false, err
	}
	request, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/v1/knowledge-bases/"+url.PathEscape(kb)+"/knowledge/file", &body)
	if err != nil {
		return "", false, errors.New("invalid attachment request")
	}
	request.Header.Set("X-API-Key", c.key)
	request.Header.Set("X-Tenant-ID", strconv.FormatUint(c.tenant, 10))
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Accept", "application/json")
	if claim != nil {
		if err := claim(); err != nil {
			return "", false, err
		}
	}
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return "", false, ctx.Err()
		}
		return "", false, errors.New("attachment upload outcome unknown")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(raw) > 4<<20 {
		return "", false, errors.New("attachment response unavailable")
	}
	var out struct {
		Success bool               `json:"success"`
		Code    string             `json:"code"`
		Data    attachmentDocument `json:"data"`
	}
	if json.Unmarshal(raw, &out) != nil || !matches(out.Data) || importID != "" && out.Data.value("attachment_import_id") != importID {
		return "", false, errors.New("attachment response identity mismatch")
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 && out.Success {
		return out.Data.ID, importID != "", nil
	}
	if response.StatusCode == 409 && out.Code == "duplicate_file" {
		return out.Data.ID, importID != "", nil
	}
	return "", false, errors.New("attachment upload rejected")
}
