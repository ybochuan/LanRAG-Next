package agent

import (
	"context"
	"errors"
	"github.com/Tencent/WeKnora/internal/background"
	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"testing"
)

func TestGovernanceResumeRejectsBeforeSnapshotReplay(t *testing.T) {
	ctx := context.WithValue(context.Background(), governanceFixtureKey{}, true)
	_, err := (&AgentEngine{}).Resume(ctx, "session", "message", nil, []byte(`{}`), nil)
	if !errors.Is(err, extensionbridge.ErrUnattributed) {
		t.Fatal("resume did not reject unknown old provenance before replay")
	}
}

func TestGovernanceUnknownBackgroundIsNotReadOrAcknowledged(t *testing.T) {
	ctx := context.WithValue(context.Background(), governanceFixtureKey{}, true)
	run := &fakeBackgroundRun{roundStart: []background.Delivery{{Text: "REVOKED_NOTICE", AckIDs: []string{"notice"}}}}
	engine := &AgentEngine{backgroundRun: run}
	state := &types.AgentState{}
	messages := []chat.Message{}
	if n := engine.injectBackground(ctx, state, &messages, false); n != 0 || len(messages) != 0 || len(run.roundStart) != 1 || len(state.PendingBackground) != 0 {
		t.Fatal("unproved notice consumed or queued for ACK")
	}
}

func TestGovernanceUnknownToolImagesCannotEnterAutomaticVision(t *testing.T) {
	ctx := context.WithValue(context.Background(), governanceFixtureKey{}, true)
	if e := guardToolImageInputs(ctx, "read_document", &types.ToolResult{Success: true, Images: []string{"data:image/png;base64,REVOKED_IMAGE"}}); e == nil {
		t.Fatal("unproved tool image can reach automatic vision")
	}
}

func TestAgentSourceFieldsCoverPinnedDocumentMetadata(t *testing.T) {
	e := &AgentEngine{selectedDocs: []*SelectedDocumentInfo{{KnowledgeID: "doc", KnowledgeBaseID: "kb", Title: "TITLE", FileName: "FILE", FileType: "pdf"}}}
	inputs, known := e.modelSourceInputs([]chat.Message{{Role: "user", Content: "query"}})
	if !known || len(inputs) != 3 {
		t.Fatal("pinned document fields not proved")
	}
	for _, input := range inputs {
		if input.DocumentID != "doc" || input.ChunkID != "" || input.ValueHash == "" {
			t.Fatal("wrong source binding")
		}
	}
	e.pageContext = &PageContextInfo{Title: "UNPROVEN_PAGE"}
	if _, known = e.modelSourceInputs(nil); known {
		t.Fatal("unknown page context can seal")
	}
	e.pageContext = nil
	if _, known = e.modelSourceInputs([]chat.Message{{Images: []string{"UNPROVEN_IMAGE"}}}); known {
		t.Fatal("unknown vision input can seal")
	}
}
