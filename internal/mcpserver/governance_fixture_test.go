package mcpserver

import (
	"context"
	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"os"
	"testing"
)

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

	governedDocuments = func(ctx context.Context, _ uint64, _ string, ids []string) (map[string]bool, error) {
		if ctx.Value(governanceFixtureKey{}) != nil {
			return nil, extensionbridge.ErrDocumentUnavailable
		}
		m := map[string]bool{}
		for _, id := range ids {
			if id != "governance-revoked" {
				m[id] = true
			}
		}
		return m, nil
	}
	rejectUnattributed = func(ctx context.Context) error {
		if ctx.Value(governanceFixtureKey{}) != nil {
			return extensionbridge.ErrUnattributed
		}
		return nil
	}
	os.Exit(m.Run())
}
