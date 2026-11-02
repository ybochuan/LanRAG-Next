package chatpipeline

import (
	"context"
	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"os"
	"testing"
)

// Legacy algorithm tests use an explicit allow fixture. New governance tests
// select denial with a private test-only context marker. Process E2E and the
// extensionbridge HTTP tests exercise the real production client separately.
type governanceFixtureKey struct{}

func TestMain(m *testing.M) {
	extensionbridge.EvaluateDocuments = func(ctx context.Context, _ uint64, _ []string, ids []string) (map[string]bool, error) {
		if ctx.Value(governanceFixtureKey{}) != nil {
			return nil, extensionbridge.ErrDocumentUnavailable
		}
		allowed := map[string]bool{}
		for _, id := range ids {
			if id != "governance-revoked" {
				allowed[id] = true
			}
		}
		return allowed, nil
	}
	extensionbridge.EvaluateUnattributed = func(ctx context.Context) error {
		if ctx.Value(governanceFixtureKey{}) != nil {
			return extensionbridge.ErrUnattributed
		}
		return nil
	}

	governedDocuments = func(ctx context.Context, _ uint64, _ []string, ids []string) (map[string]bool, error) {
		if ctx.Value(governanceFixtureKey{}) != nil {
			return nil, extensionbridge.ErrDocumentUnavailable
		}
		allowed := map[string]bool{}
		for _, id := range ids {
			if id != "governance-revoked" {
				allowed[id] = true
			}
		}
		return allowed, nil
	}

	os.Exit(m.Run())
}
