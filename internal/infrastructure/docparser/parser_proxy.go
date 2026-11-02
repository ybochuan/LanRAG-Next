package docparser

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// ParserProxyIdentity is copied from the persisted native Knowledge record.
type ParserProxyIdentity struct {
	TenantID uint64 // Native owner tenant.
	KBID     string // Native knowledge base.
	DocID    string // Native knowledge ID, never a source hash or job ID.
}

// ParserProxyAddress selects only the PDF file hook; other readers stay native.
func ParserProxyAddress(fileType string, isURL bool) string {
	if isURL || !strings.EqualFold(fileType, "pdf") {
		return ""
	}
	return strings.TrimSpace(os.Getenv("AGENTRAG_PARSER_PROXY_ADDR"))
}

type parserProxyReadFunc func(context.Context, *types.ReadRequest) (*types.ReadResult, error)

func (f parserProxyReadFunc) Read(ctx context.Context, r *types.ReadRequest) (*types.ReadResult, error) {
	return f(ctx, r)
}

type parserProxyRoundTripFunc func(*http.Request) (*http.Response, error)

func (f parserProxyRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// NewParserProxyReader injects transport only; native V1 result handling is reused.
// Credentials/identity are immutable for all legs, including detached cancellation.
func NewParserProxyReader(addr string, id ParserProxyIdentity, overrides map[string]string) interfaces.DocReader {
	return parserProxyReadFunc(func(ctx context.Context, req *types.ReadRequest) (*types.ReadResult, error) {
		u, err := parserProxyOrigin(addr)
		if err != nil {
			return nil, err
		}
		key := strings.TrimSpace(os.Getenv("AGENTRAG_PARSER_PROXY_KEY"))
		if len(key) < 16 || len(key) > 256 || strings.IndexFunc(key, func(r rune) bool { return r <= 32 || r >= 127 }) >= 0 {
			return nil, errors.New("invalid parser proxy credential")
		}
		if id.TenantID == 0 || !validParserProxyID(id.KBID) || !validParserProxyID(id.DocID) || req == nil || req.URL != "" || len(req.FileContent) == 0 {
			return nil, errors.New("invalid parser proxy file identity or content")
		}
		reader := NewMinerUReader(overrides)
		client := newMinerUV1Client(strings.TrimRight(u.String(), "/")+"/parser/native", key, reader.timeout)
		base := http.DefaultTransport.(*http.Transport).Clone()
		base.Proxy = nil
		defer base.CloseIdleConnections()
		transport := parserProxyRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			if !client.sameOrigin(r.URL) || r.URL.User != nil || r.URL.RawQuery != "" || r.URL.ForceQuery || r.URL.Fragment != "" || r.URL.RawPath != "" || !strings.HasPrefix(r.URL.Path, "/parser/native/v1/") {
				return nil, errors.New("parser proxy request escaped configured origin")
			}
			out := r.Clone(r.Context())
			out.Header.Set("Authorization", "Bearer "+key)
			out.Header.Set("X-AgentRAG-Bridge-Version", "1")
			out.Header.Set("X-AgentRAG-Tenant-ID", strconv.FormatUint(id.TenantID, 10))
			out.Header.Set("X-AgentRAG-KB-ID", id.KBID)
			out.Header.Set("X-AgentRAG-Doc-ID", id.DocID)
			return base.RoundTrip(out)
		})
		localClient := func(timeout time.Duration) *http.Client {
			return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		}
		client.apiClient = localClient(mineruV1APITimeout)
		client.bulkClient = localClient(reader.timeout)
		reader.v1Client = client
		return reader.readV1(ctx, req)
	})
}

// The address is trusted operator configuration, not a tenant-supplied URL.
// Host IPs are allowed explicitly; requests remain pinned and never redirect.
func parserProxyOrigin(addr string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(addr))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("invalid parser proxy origin")
	}
	if port := u.Port(); port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return nil, errors.New("invalid parser proxy port")
		}
	}
	return u, nil
}
func validParserProxyID(value string) bool {
	if value == "" || len(value) > 256 || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}
