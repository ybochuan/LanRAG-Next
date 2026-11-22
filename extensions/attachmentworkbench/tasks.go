package attachmentworkbench

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	"github.com/ybochuan/LanRAG-Next/extensions/business"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

// TaskItem is one attachment that needs a naming or mapping decision.
type TaskItem struct {
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	NativeTitle       string   `json:"native_title"`
	FileName          string   `json:"file_name"`
	DisplayName       string   `json:"display_name,omitempty"`
	MappingStatus     string   `json:"mapping_status"`
	ReferenceTitle    string   `json:"reference_title,omitempty"`
	ReferenceLocation string   `json:"reference_location,omitempty"`
	TitleState        string   `json:"title_state,omitempty"`
	Reasons           []string `json:"reasons"`
}

// TaskDocument groups the pending attachment work of one parent document.
type TaskDocument struct {
	ParentID       string     `json:"parent_id"`
	ParentTitle    string     `json:"parent_title"`
	ParentFileName string     `json:"parent_file_name"`
	Count          int        `json:"count"`
	Items          []TaskItem `json:"items"`
}

// TaskList is the read-only work queue shown by the document workbench.
type TaskList struct {
	Documents []TaskDocument `json:"documents"`
	Total     int            `json:"total"`
	Truncated bool           `json:"truncated"`
}

const taskItemLimit = 500

// Tasks collects the current attachments whose native title is still generic
// while an extracted Chinese name is available, whose mapping is still a
// candidate, or whose title projection is pending/conflicting. Detached,
// historical and deleting files are not actionable and are omitted.
func (s *Service) Tasks(ctx context.Context, scope Scope) (TaskList, error) {
	result := TaskList{Documents: []TaskDocument{}}
	if s == nil || s.client == nil || s.client.TenantID() != scope.Tenant {
		return result, business.ErrUnavailable
	}
	all, err := s.client.ListAttachmentDocuments(ctx, scope.KB)
	if err != nil {
		return result, business.ErrUnavailable
	}
	byParent := map[string][]weknora.AttachmentDocument{}
	ids := []string{}
	for _, doc := range all {
		if !doc.IsAttachment || doc.ParentDocumentID == "" {
			continue
		}
		byParent[doc.ParentDocumentID] = append(byParent[doc.ParentDocumentID], doc)
		ids = append(ids, doc.ID)
	}
	if len(ids) == 0 {
		return result, nil
	}
	ownership := map[string]store.AttachmentOwnership{}
	if err := s.db.View(ctx, func(tx *sql.Tx) error {
		var err error
		ownership, err = store.AttachmentOwnershipsTx(ctx, tx, scope.Tenant, ids)
		return err
	}); err != nil {
		return result, err
	}
	semantics, err := s.db.AttachmentSemantics(ctx, scope.Tenant, ids)
	if err != nil {
		return result, err
	}
	titleStates, err := s.db.AttachmentTitleStatuses(ctx, scope.Tenant, scope.KB, ids)
	if err != nil {
		return result, err
	}
	parentIDs := make([]string, 0, len(byParent))
	for parent := range byParent {
		parentIDs = append(parentIDs, parent)
	}
	sort.Strings(parentIDs)
	parents, err := s.documents(ctx, scope, parentIDs...)
	if err != nil {
		return result, err
	}
	for _, parentID := range parentIDs {
		parent := parents[parentID]
		if parent.ID == "" || parent.IsAttachment {
			continue
		}
		states, err := s.db.AttachmentImportState(ctx, scope.Tenant, scope.KB, parent.ID, parent.FileHash)
		if err != nil {
			return result, err
		}
		type candidate struct {
			child    weknora.AttachmentDocument
			semantic store.AttachmentSemantic
			name     string
			reasons  []string
		}
		candidates := []candidate{}
		for _, child := range byParent[parentID] {
			link := ownership[child.ID]
			managed := link.KnowledgeBaseID == scope.KB && link.ParentDocumentID == parent.ID &&
				link.ParentFileHash == parent.FileHash && link.AttachmentFileHash == child.FileHash &&
				((link.EvidenceKind == store.AttachmentUploadEvidence && link.ImportID == child.ImportID) ||
					(link.EvidenceKind == store.AttachmentLegacyEvidence && child.ImportID == "" && child.ExtractedSHA256 == link.AttachmentSHA256))
			if !managed || link.Historical || child.ParseStatus == "deleting" || states[link.AttachmentSHA256] == "removed" {
				continue
			}
			sem := semantics[child.ID]
			valid := sem.KnowledgeBaseID == scope.KB && sem.ParentDocumentID == parent.ID && sem.ParentFileHash == parent.FileHash
			if !valid {
				sem = store.AttachmentSemantic{}
			}
			title := strings.TrimSpace(child.Title)
			generic := title == "" || strings.EqualFold(title, strings.TrimSpace(child.FileName))
			proposal := strings.TrimSpace(sem.DisplayName)
			reasons := []string{}
			if generic && proposal != "" && !strings.EqualFold(proposal, child.FileName) && !strings.EqualFold(proposal, title) {
				reasons = append(reasons, "name_review")
			}
			if valid && sem.MappingStatus == "candidate" {
				reasons = append(reasons, "mapping_review")
			}
			state := titleStates[child.ID]
			if state == "pending" {
				reasons = append(reasons, "title_pending")
			} else if state == "conflict" {
				reasons = append(reasons, "title_conflict")
			}
			name := title
			if generic && proposal != "" {
				name = proposal
			}
			if name == "" {
				name = child.FileName
			}
			candidates = append(candidates, candidate{child: child, semantic: sem, name: name, reasons: reasons})
		}
		counts := map[string]int{}
		for _, c := range candidates {
			if key := taskNameKey(c.name); key != "" {
				counts[key]++
			}
		}
		items := []TaskItem{}
		for _, c := range candidates {
			if key := taskNameKey(c.name); key != "" && counts[key] > 1 {
				c.reasons = append(c.reasons, "duplicate_name")
			}
			if len(c.reasons) == 0 {
				continue
			}
			if result.Total >= taskItemLimit {
				result.Truncated = true
				break
			}
			items = append(items, TaskItem{
				ID: c.child.ID, Title: c.name, NativeTitle: c.child.Title, FileName: c.child.FileName,
				DisplayName: c.semantic.DisplayName, MappingStatus: c.semantic.MappingStatus,
				ReferenceTitle: c.semantic.ReferenceTitle, ReferenceLocation: c.semantic.ReferenceLocation,
				TitleState: titleStates[c.child.ID], Reasons: c.reasons,
			})
			result.Total++
		}
		if len(items) == 0 {
			continue
		}
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].FileName == items[j].FileName {
				return items[i].ID < items[j].ID
			}
			return items[i].FileName < items[j].FileName
		})
		title := strings.TrimSpace(parent.Title)
		if title == "" {
			title = parent.FileName
		}
		result.Documents = append(result.Documents, TaskDocument{
			ParentID: parent.ID, ParentTitle: title, ParentFileName: parent.FileName, Count: len(items), Items: items,
		})
		if result.Truncated {
			break
		}
	}
	sort.SliceStable(result.Documents, func(i, j int) bool {
		if result.Documents[i].ParentTitle == result.Documents[j].ParentTitle {
			return result.Documents[i].ParentID < result.Documents[j].ParentID
		}
		return result.Documents[i].ParentTitle < result.Documents[j].ParentTitle
	})
	return result, nil
}

func taskNameKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, ext := range []string{".docx", ".doc", ".pdf", ".xlsx", ".xls", ".pptx", ".ppt"} {
		value = strings.TrimSuffix(value, ext)
	}
	return strings.Join(strings.Fields(value), "")
}
