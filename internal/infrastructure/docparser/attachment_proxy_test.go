package docparser

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestOfficeAttachmentProxyPrecedesNativeReader(t *testing.T) {
	for _, status := range []int{200, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			posted, read := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				posted++
				if r.URL.Path != "/parser/native/v1/attachments" || r.Header.Get("Authorization") != "Bearer "+parserProxyTestKey || r.Header.Get("X-AgentRAG-Doc-ID") != "parent" || r.Header.Get("X-AgentRAG-Tenant-ID") != "7" || r.Header.Get("X-AgentRAG-KB-ID") != "kb-a" {
					t.Error("missing fixed identity")
				}
				b, _ := io.ReadAll(r.Body)
				if string(b) != "office-bytes" {
					t.Error("source changed")
				}
				w.WriteHeader(status)
				io.WriteString(w, `{"status":"ok","attachments":[]}`)
			}))
			defer server.Close()
			t.Setenv("AGENTRAG_PARSER_PROXY_ADDR", server.URL)
			t.Setenv("AGENTRAG_PARSER_PROXY_KEY", parserProxyTestKey)
			original := parserProxyReadFunc(func(context.Context, *types.ReadRequest) (*types.ReadResult, error) {
				read++
				if posted != 1 {
					t.Error("native parser ran before extraction")
				}
				return &types.ReadResult{MarkdownContent: "native markdown"}, nil
			})
			reader := WithAttachmentExtraction(original, ParserProxyIdentity{TenantID: 7, KBID: "kb-a", DocID: "parent"})
			out, e := reader.Read(context.Background(), &types.ReadRequest{FileName: "parent.docx", FileType: "docx", FileContent: []byte("office-bytes")})
			if status == 200 {
				if e != nil || read != 1 || out.MarkdownContent != "native markdown" {
					t.Fatalf("native result altered: %v", e)
				}
			} else if e == nil || read != 0 {
				t.Fatal("failed import silently skipped")
			}
		})
	}
}
