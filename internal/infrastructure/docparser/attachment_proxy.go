package docparser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// WithAttachmentExtraction adds the identity-bearing Office pre-parse HTTP hook.
// The original reader and its result handling remain unchanged.
func WithAttachmentExtraction(reader interfaces.DocReader, id ParserProxyIdentity) interfaces.DocReader {
	return parserProxyReadFunc(func(ctx context.Context, req *types.ReadRequest) (*types.ReadResult, error) {
		addr := strings.TrimSpace(os.Getenv("AGENTRAG_PARSER_PROXY_ADDR"))
		if req == nil {
			return nil, errors.New("attachment source required")
		}
		typ := strings.TrimPrefix(strings.ToLower(req.FileType), ".")
		if addr == "" || req.URL != "" || (typ != "docx" && typ != "xlsx" && typ != "pptx") {
			return reader.Read(ctx, req)
		}
		origin, err := parserProxyOrigin(addr)
		if err != nil {
			return nil, err
		}
		key := strings.TrimSpace(os.Getenv("AGENTRAG_PARSER_PROXY_KEY"))
		if len(key) < 16 || len(key) > 256 || strings.IndexFunc(key, func(r rune) bool { return r <= 32 || r >= 127 }) >= 0 || id.TenantID == 0 || !validParserProxyID(id.KBID) || !validParserProxyID(id.DocID) || len(req.FileContent) == 0 || len(req.FileContent) > 50<<20 {
			return nil, errors.New("invalid attachment proxy identity or source")
		}
		request, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(origin.String(), "/")+"/parser/native/v1/attachments", bytes.NewReader(req.FileContent))
		if err != nil {
			return nil, errors.New("attachment proxy request failed")
		}
		request.Header.Set("Authorization", "Bearer "+key)
		request.Header.Set("X-AgentRAG-Bridge-Version", "1")
		request.Header.Set("X-AgentRAG-Tenant-ID", strconv.FormatUint(id.TenantID, 10))
		request.Header.Set("X-AgentRAG-KB-ID", id.KBID)
		request.Header.Set("X-AgentRAG-Doc-ID", id.DocID)
		request.Header.Set("Content-Type", "application/octet-stream")
		request.Header.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": req.FileName}))
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, Timeout: 120 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		response, err := client.Do(request)
		if err != nil {
			return nil, errors.New("attachment proxy unavailable")
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
		var ack struct {
			Status string `json:"status"`
		}
		if err != nil || len(data) > 1<<20 || response.StatusCode != 200 || json.Unmarshal(data, &ack) != nil || ack.Status != "ok" {
			return nil, errors.New("attachment import not acknowledged")
		}
		return reader.Read(ctx, req)
	})
}
