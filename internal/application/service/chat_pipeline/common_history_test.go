package chatpipeline

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recentMessagesStub struct {
	interfaces.MessageService
	messages []*types.Message
}

func (s *recentMessagesStub) GetRecentMessagesBySession(context.Context, string, int) ([]*types.Message, error) {
	return s.messages, nil
}

func TestLoadAndProcessHistoryUsesOnlyPriorUserQuestionsForKnowledge(t *testing.T) {
	svc := &recentMessagesStub{messages: []*types.Message{
		{RequestID: "past", Role: "user", Content: "Which clause applies?", RenderedContent: "old prompt", Images: types.MessageImages{{Caption: "old caption"}}},
		{RequestID: "past", Role: "assistant", Content: "old correction", KnowledgeReferences: types.References{{ID: "old chunk"}}},
		{RequestID: "current", Role: "user", Content: "What is the deadline?"},
	}}
	ctx := WithFreshKnowledgeHistory(context.Background())
	history, err := loadAndProcessHistory(ctx, svc, "s1", 5, 20)
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.True(t, historyHasOnlyQuestions(ctx, history))
	assert.Equal(t, "Which clause applies?", history[0].Query)
	assert.Empty(t, history[0].Answer)
	assert.Empty(t, history[0].KnowledgeReferences)
	messages := AppendHistoryMessages(nil, history)
	require.Len(t, messages, 1)
	assert.Equal(t, "user", messages[0].Role)
	assert.NotContains(t, formatConversationHistory(history), "old correction")
}

func TestCurrentKnowledgeQuestionRewriteUsesHistoryOnlyForReferences(t *testing.T) {
	p := &PluginQueryUnderstand{config: &config.Config{Conversation: &config.ConversationConfig{
		RewritePromptSystem: "{{conversation}}",
		RewritePromptUser:   "{{query}}",
	}}}
	cm := &types.ChatManage{}
	cm.Query = "Who receives the blue receipt?"
	history := []*types.History{{Query: "When is reimbursement due?"}}
	ctx := WithFreshKnowledgeHistory(t.Context())
	system, user, _ := p.buildPrompts(ctx, cm, history)
	assert.Contains(t, system, history[0].Query)
	assert.Contains(t, system, "Never combine earlier questions with the current question")
	assert.Contains(t, user, cm.Query)
	system, _, _ = p.buildPrompts(ctx, cm, nil)
	assert.NotContains(t, system, "## Current knowledge question")
	system, _, _ = p.buildPrompts(t.Context(), cm, history)
	assert.NotContains(t, system, "## Current knowledge question")
}

func TestLoadAndProcessHistoryKeepsImageOnlyTurn(t *testing.T) {
	at := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	svc := &recentMessagesStub{messages: []*types.Message{
		{RequestID: "r1", Role: "user", Images: types.MessageImages{{URL: "resource://img"}}, CreatedAt: at},
		{RequestID: "r1", Role: "assistant", Content: "a cat", CreatedAt: at.Add(time.Second)},
	}}
	ctx := context.WithValue(context.Background(), types.LanguageContextKey, "zh-CN")

	history, err := loadAndProcessHistory(ctx, svc, "s1", 5, 20)
	require.NoError(t, err)

	require.Len(t, history, 1, "an image-only turn must stay in history for follow-up questions")
	assert.Equal(t, types.UploadOnlyQuestion("zh-CN"), history[0].Query)
	assert.Equal(t, "a cat", history[0].Answer)
}
