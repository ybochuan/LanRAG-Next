// Package retrieval holds policies independent of WeKnora retrieval types.
package retrieval

import "fmt"

// Scope is the native authorization scope to which an exclusion list belongs.
type Scope struct {
	TenantID        uint64 `json:"tenant_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
}

// Exclusions is the M0 best-effort exclusion policy for one tenant and KB.
type Exclusions struct {
	Scope
	ExcludedDocumentIDs []string `json:"excluded_document_ids"`
}

// Selection must become an explicit document target in the native adapter.
// DenyAll must never be converted to an unrestricted, whole-KB search.
type Selection struct {
	KnowledgeIDs []string `json:"knowledge_ids"`
	DenyAll      bool     `json:"deny_all"`
}

// Apply subtracts excluded IDs from an already enumerated authorized set.
// It does not enumerate a whole KB, grant permissions, or claim all tools are gated.
func (e Exclusions) Apply(scope Scope, authorizedDocumentIDs []string) (Selection, error) {
	selection := Selection{KnowledgeIDs: []string{}, DenyAll: true}
	if scope.TenantID == 0 || scope.KnowledgeBaseID == "" || scope != e.Scope {
		return selection, fmt.Errorf("exclusion scope does not match authorized tenant and knowledge base")
	}
	excluded := make(map[string]bool, len(e.ExcludedDocumentIDs))
	for _, id := range e.ExcludedDocumentIDs {
		excluded[id] = true
	}
	seen := make(map[string]bool, len(authorizedDocumentIDs))
	for _, id := range authorizedDocumentIDs {
		if id != "" && !excluded[id] && !seen[id] {
			selection.KnowledgeIDs = append(selection.KnowledgeIDs, id)
			seen[id] = true
		}
	}
	selection.DenyAll = len(selection.KnowledgeIDs) == 0
	return selection, nil
}
