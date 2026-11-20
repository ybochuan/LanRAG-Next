package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"unicode/utf8"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/types"
)

const (
	qaFeedbackQuestionBytes = 2048
	qaFeedbackAnswerBytes   = 8192
	qaFeedbackMaxSources    = 100
)

type qaFeedbackSnapshot struct {
	Question           string `json:"question,omitempty"`
	AnswerExcerpt      string `json:"answer_excerpt,omitempty"`
	QuestionTruncated  bool   `json:"question_truncated,omitempty"`
	AnswerTruncated    bool   `json:"answer_truncated,omitempty"`
	SnapshotRestricted bool   `json:"snapshot_restricted,omitempty"`
}

func (h *QAFeedbackHandler) feedbackSnapshot(ctx context.Context, tenantID uint64, answer *types.Message) (qaFeedbackSnapshot, string, []string, error) {
	restricted := qaFeedbackSnapshot{SnapshotRestricted: true}
	if answer.RequestID == "" || answer.AgentTenantID != 0 && answer.AgentTenantID != tenantID || hasPrivateFeedbackSources(answer) {
		return restricted, "", nil, nil
	}
	rows, err := h.messages.GetMessagesByRequestIDs(ctx, answer.SessionID, []string{answer.RequestID})
	if err != nil {
		return restricted, "", nil, err
	}
	question := qaFeedbackQuestion(answer, rows)
	if question == nil || len(question.Attachments) > 0 || len(question.Images) > 0 {
		return restricted, "", nil, nil
	}
	kbID, citationIDs, safe := h.feedbackSourceScope(ctx, tenantID, answer)
	if !safe {
		return restricted, "", nil, nil
	}
	var snapshot qaFeedbackSnapshot
	snapshot.Question, snapshot.QuestionTruncated = feedbackExcerpt(question.Content, qaFeedbackQuestionBytes)
	snapshot.AnswerExcerpt, snapshot.AnswerTruncated = feedbackExcerpt(answer.Content, qaFeedbackAnswerBytes)
	return snapshot, kbID, citationIDs, nil
}

func qaFeedbackQuestion(answer *types.Message, rows []*types.MessageWithSession) *types.Message {
	// The initial user message is persisted before the assistant row. Steers
	// share its request ID but are persisted after the assistant row.
	var question *types.Message
	for _, row := range rows {
		if row == nil || row.SessionID != answer.SessionID || row.RequestID != answer.RequestID || row.Role != "user" ||
			row.CreatedAt.After(answer.CreatedAt) {
			continue
		}
		if question != nil {
			return nil
		}
		question = &row.Message
	}
	return question
}

func hasPrivateFeedbackSources(answer *types.Message) bool {
	execution := answer.ExecutionContext
	if execution.WebSearchEnabled || execution.LocalBrowserEnabled || len(execution.MCPServiceIDs) > 0 || len(execution.SkillNames) > 0 ||
		len(answer.UsedMemories) > 0 || len(answer.Artifacts) > 0 {
		return true
	}
	for _, step := range answer.AgentSteps {
		if len(step.BackgroundInjections) > 0 {
			return true
		}
		for _, call := range step.ToolCalls {
			if types.IsPipelineToolCallID(call.ID) {
				continue
			}
			switch agenttools.SuccessorToolName(call.ExecutionName()) {
			case agenttools.ToolAskUser:
			case agenttools.ToolSearchKnowledge:
				if call.Target != nil || len(answer.KnowledgeReferences) == 0 {
					return true
				}
			default:
				// Opaque tool results can contain sources absent from the saved references.
				return true
			}
		}
	}
	return false
}

func (h *QAFeedbackHandler) feedbackSourceScope(ctx context.Context, tenantID uint64, answer *types.Message) (string, []string, bool) {
	var kbID string
	addKB := func(id string) bool {
		if !qaFeedbackID(id) || kbID != "" && kbID != id {
			return false
		}
		kbID = id
		return true
	}
	for _, id := range answer.ExecutionContext.KnowledgeBaseIDs {
		if !addKB(id) {
			return "", nil, false
		}
	}
	for _, tag := range answer.ExecutionContext.TagScopes {
		if !addKB(tag.KnowledgeBaseID) {
			return "", nil, false
		}
	}
	knowledgeIDs := make(map[string]struct{})
	for _, id := range answer.ExecutionContext.KnowledgeIDs {
		if !qaFeedbackID(id) {
			return "", nil, false
		}
		knowledgeIDs[id] = struct{}{}
	}
	seen := make(map[string]struct{})
	var citationIDs []string
	for _, ref := range answer.KnowledgeReferences {
		if ref == nil || !qaFeedbackID(ref.KnowledgeID) {
			return "", nil, false
		}
		switch ref.MatchType {
		case types.MatchTypeEmbedding, types.MatchTypeKeywords, types.MatchTypeNearByChunk, types.MatchTypeParentChunk, types.MatchTypeRelationChunk:
		default:
			return "", nil, false
		}
		if ref.KnowledgeBaseID == "" {
			knowledgeIDs[ref.KnowledgeID] = struct{}{}
		} else if !addKB(ref.KnowledgeBaseID) {
			return "", nil, false
		}
		if _, ok := seen[ref.ID]; !ok && qaFeedbackID(ref.ID) && len(citationIDs) < qaFeedbackMaxSources {
			seen[ref.ID] = struct{}{}
			citationIDs = append(citationIDs, ref.ID)
		}
	}
	if len(knowledgeIDs) > qaFeedbackMaxSources {
		return "", nil, false
	}
	if len(knowledgeIDs) > 0 {
		ids := make([]string, 0, len(knowledgeIDs))
		for id := range knowledgeIDs {
			ids = append(ids, id)
		}
		docs, err := h.knowledge.GetKnowledgeBatch(ctx, tenantID, ids)
		if err != nil {
			return "", nil, false
		}
		for _, doc := range docs {
			if doc == nil || doc.TenantID != tenantID || !addKB(doc.KnowledgeBaseID) {
				return "", nil, false
			}
			delete(knowledgeIDs, doc.ID)
		}
		if len(knowledgeIDs) > 0 {
			return "", nil, false
		}
	}
	if kbID == "" {
		return "", nil, true
	}
	if tenant, ok := types.TenantInfoFromContext(ctx); ok && tenant.ChatHistoryConfig != nil && tenant.ChatHistoryConfig.KnowledgeBaseID == kbID {
		return "", nil, false
	}
	// For authenticated tenant members, native KB reads grant access to a
	// same-tenant KB. Temporary stores and derived Wikis lack a publishable scope.
	kb, err := h.knowledgeBases.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil || kb == nil || kb.ID != kbID || kb.TenantID != tenantID || kb.IsTemporary || kb.Type == types.KnowledgeBaseTypeWiki {
		return "", nil, false
	}
	return kbID, citationIDs, true
}

func qaFeedbackSnapshotDigest(tenantID uint64, sessionID, messageID, kbID string, citationIDs []string, snapshot qaFeedbackSnapshot) string {
	// Fixed fields and persisted citation order make this stable across retries.
	encoded, _ := json.Marshal(struct {
		TenantID        uint64
		SessionID       string
		MessageID       string
		KnowledgeBaseID string
		CitationIDs     []string
		Snapshot        qaFeedbackSnapshot
	}{tenantID, sessionID, messageID, kbID, citationIDs, snapshot})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func feedbackExcerpt(text string, limit int) (string, bool) {
	if len(text) <= limit {
		return text, false
	}
	for !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit], true
}
