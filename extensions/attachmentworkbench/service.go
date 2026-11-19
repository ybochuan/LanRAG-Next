package attachmentworkbench

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"unicode"

	"github.com/ybochuan/LanRAG-Next/extensions/business"
	"github.com/ybochuan/LanRAG-Next/extensions/lineage"
	"github.com/ybochuan/LanRAG-Next/extensions/parser/attachment"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

// Service coordinates scoped native files with their versioned governance records.
type Service struct {
	db        *store.DB
	client    *weknora.Client
	importer  *attachment.Importer
	relations *business.Service
	mu        sync.Mutex
}

func New(db *store.DB, client *weknora.Client, importer *attachment.Importer) *Service {
	return &Service{db: db, client: client, importer: importer}
}

func (s *Service) WithRelations(relations *business.Service) *Service {
	s.relations = relations
	return s
}

type Scope struct {
	Tenant uint64
	KB     string
}

type Result struct {
	Parent         string                       `json:"parent_id"`
	ParentVersion  string                       `json:"parent_version"`
	ParentFileName string                       `json:"parent_file_name"`
	Items          []Item                       `json:"items"`
	References     []attachment.ParentReference `json:"references,omitempty"`
	Preview        []attachment.PreviewFile     `json:"preview,omitempty"`
}

type Item struct {
	StoreReference store.AttachmentReference `json:"reference"`
	ID             string                    `json:"id"`
	Version        string                    `json:"file_hash"`
	Title          string                    `json:"title"`
	NativeTitle    string                    `json:"native_title"`
	Original       string                    `json:"file_name"`
	Folder         string                    `json:"folder_path"`
	Mapping        string                    `json:"mapping_status"`
	State          string                    `json:"state"`
	ParseStatus    string                    `json:"parse_status"`
	TitleState     string                    `json:"title_state,omitempty"`
	Revision       int64                     `json:"revision"`
	RelationID     string                    `json:"relation_id,omitempty"`
	RelationType   string                    `json:"relation_type"`
	Managed        bool                      `json:"managed"`
}

// Command carries expected document and workbench revisions supplied by the UI.
type Command struct {
	Parent          string   `json:"parent_id"`
	ParentVersion   string   `json:"parent_version"`
	Document        string   `json:"attachment_id"`
	DocumentVersion string   `json:"file_hash"`
	PreviousTitle   string   `json:"expected_title"`
	Title           string   `json:"title"`
	ReferenceID     *string  `json:"reference_id"`
	Fingerprints    []string `json:"fingerprints"`
}

func (s *Service) documents(ctx context.Context, scope Scope, ids ...string) (map[string]weknora.AttachmentDocument, error) {
	if s == nil || s.client == nil || s.client.TenantID() != scope.Tenant {
		return nil, business.ErrUnavailable
	}
	docs, err := s.client.AttachmentDocuments(ctx, []string{scope.KB}, ids)
	if err != nil {
		return nil, business.ErrUnavailable
	}
	result := make(map[string]weknora.AttachmentDocument, len(docs))
	for _, d := range docs {
		result[d.ID] = d
	}
	return result, nil
}

func (s *Service) List(ctx context.Context, scope Scope, document string) (Result, error) {
	result := Result{Items: []Item{}}
	docs, err := s.documents(ctx, scope, document)
	if err != nil {
		return result, err
	}
	parent := docs[document]
	if parent.ID == "" {
		return result, business.ErrNotFound
	}
	if parent.IsAttachment {
		docs, err = s.documents(ctx, scope, parent.ParentDocumentID)
		if err != nil {
			return result, err
		}
		parent = docs[parent.ParentDocumentID]
		if parent.ID == "" || parent.IsAttachment {
			return result, business.ErrNotFound
		}
	}
	result.Parent, result.ParentVersion, result.ParentFileName = parent.ID, parent.FileHash, parent.FileName
	all, err := s.client.ListAttachmentDocuments(ctx, scope.KB)
	if err != nil {
		return result, business.ErrUnavailable
	}
	attachments := []weknora.AttachmentDocument{}
	ids := []string{}
	for _, doc := range all {
		if doc.ParentDocumentID == parent.ID {
			attachments = append(attachments, doc)
			ids = append(ids, doc.ID)
		}
	}
	ownership := map[string]store.AttachmentOwnership{}
	if err = s.db.View(ctx, func(tx *sql.Tx) error {
		var err error
		ownership, err = store.AttachmentOwnershipsTx(ctx, tx, scope.Tenant, ids)
		return err
	}); err != nil {
		return result, err
	}
	states, err := s.db.AttachmentImportState(ctx, scope.Tenant, scope.KB, parent.ID, parent.FileHash)
	if err != nil {
		return result, err
	}
	semantics, err := s.db.AttachmentSemantics(ctx, scope.Tenant, ids)
	if err != nil {
		return result, err
	}
	confirmed, err := s.db.AttachmentConfirmations(ctx, scope.Tenant, ids)
	if err != nil {
		return result, err
	}
	for _, child := range attachments {
		link := ownership[child.ID]
		managed := link.KnowledgeBaseID == scope.KB && link.ParentDocumentID == parent.ID &&
			link.ParentFileHash == parent.FileHash && link.AttachmentFileHash == child.FileHash &&
			((link.EvidenceKind == store.AttachmentUploadEvidence && link.ImportID == child.ImportID) ||
				(link.EvidenceKind == store.AttachmentLegacyEvidence && child.ImportID == "" && child.ExtractedSHA256 == link.AttachmentSHA256))
		row := Item{ID: child.ID, Version: child.FileHash, RelationType: "attachment_of", Title: child.Title,
			NativeTitle: child.Title, Original: child.FileName, Folder: child.FolderPath, ParseStatus: child.ParseStatus,
			Mapping: "unmatched", State: "current", Managed: managed}
		if child.ParseStatus == "deleting" {
			row.State = "deleting"
		} else if states[link.AttachmentSHA256] == "removed" && managed {
			row.State = "detached"
		} else if link.Historical || link.ImportID != "" && !managed {
			row.State = "historical"
		}
		auto := semantics[child.ID]
		if auto.KnowledgeBaseID == scope.KB && auto.ParentDocumentID == parent.ID && auto.ParentFileHash == parent.FileHash {
			if (row.Title == "" || strings.EqualFold(row.Title, row.Original)) && auto.DisplayName != "" {
				row.Title = auto.DisplayName
			}
			row.Mapping = auto.MappingStatus
			row.StoreReference = store.AttachmentReference{Title: auto.ReferenceTitle, Location: auto.ReferenceLocation,
				Excerpt: auto.ReferenceExcerpt, Sequence: auto.AttachmentSequence}
		}
		for _, manual := range confirmed {
			if manual.KnowledgeBaseID == scope.KB && manual.ParentID == parent.ID && manual.ParentHash == parent.FileHash &&
				manual.AttachmentID == child.ID && manual.AttachmentHash == child.FileHash {
				row.Revision = manual.Revision
				row.Mapping = "manual"
				row.StoreReference = manual.AttachmentReference
				break
			}
		}
		if row.Title == "" {
			row.Title = row.Original
		}
		row.TitleState, err = s.db.AttachmentTitleStatus(ctx, scope.Tenant, scope.KB, child.ID)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, row)
	}
	result.Items, err = s.relatedFiles(ctx, scope, parent.ID, result.Items)
	if err != nil {
		return result, err
	}
	result.Items, err = s.reclaimStatus(ctx, scope, parent.ID, result.Items)
	return result, err
}

func (s *Service) reclaimStatus(ctx context.Context, scope Scope, parent string, result []Item) ([]Item, error) {
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT document_id,file_hash,title,status FROM ext_attachment_reclaims
			WHERE tenant_id=? AND knowledge_base_id=? AND parent_document_id=? ORDER BY rowid DESC LIMIT 100`,
			fmt.Sprint(scope.Tenant), scope.KB, parent)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item := Item{RelationType: "attachment_of"}
			if err := rows.Scan(&item.ID, &item.Version, &item.Title, &item.State); err != nil {
				return err
			}
			found := false
			for i := range result {
				if result[i].ID == item.ID && result[i].Version == item.Version {
					result[i].State = item.State
					found = true
					break
				}
			}
			if !found {
				result = append(result, item)
			}
		}
		return rows.Err()
	})
	return result, err
}

func (s *Service) relatedFiles(ctx context.Context, scope Scope, parent string, result []Item) ([]Item, error) {
	if s.relations == nil {
		return result, nil
	}
	related, err := s.relations.QueryRelated(ctx, provenance.Scope{TenantID: scope.Tenant, KnowledgeBaseID: scope.KB}, parent)
	if errors.Is(err, business.ErrConflict) || errors.Is(err, lineage.ErrSourceMissing) || errors.Is(err, business.ErrNotFound) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	ids := []string{}
	kinds := map[string]string{}
	for _, row := range related {
		if row.KnowledgeBaseID == scope.KB &&
			(row.EdgeType == "template_for" && row.Direction == "incoming" || row.EdgeType == "cites" && row.Direction == "outgoing") {
			if kinds[row.DocumentID] == "" {
				ids = append(ids, row.DocumentID)
			}
			kinds[row.DocumentID] = row.EdgeType
		}
	}
	if len(ids) == 0 {
		return result, nil
	}
	docs, err := s.documents(ctx, scope, ids...)
	if err != nil {
		return result, err
	}
	err = s.db.View(ctx, func(tx *sql.Tx) error {
		for _, id := range ids {
			d := docs[id]
			if d.ID == "" || d.IsAttachment || d.ParseStatus == "deleting" {
				continue
			}
			source, target := id, parent
			if kinds[id] == "cites" {
				source, target = parent, id
			}
			item := Item{ID: id, Version: d.FileHash, Title: d.Title, NativeTitle: d.Title, Original: d.FileName, Folder: d.FolderPath,
				RelationType: kinds[id], State: "current", ParseStatus: d.ParseStatus}
			err := tx.QueryRowContext(ctx, `SELECT relation_id,revision FROM ext_document_relations
				WHERE tenant_id=? AND knowledge_base_id=? AND target_knowledge_base_id=? AND document_id=? AND target_document_id=?
				AND edge_type=? AND status='active' ORDER BY rowid DESC LIMIT 1`, fmt.Sprint(scope.Tenant), scope.KB, scope.KB, source, target, item.RelationType).
				Scan(&item.RelationID, &item.Revision)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			result = append(result, item)
		}
		return nil
	})
	return result, err
}

func (s *Service) Preview(ctx context.Context, scope Scope, document string) (Result, error) {
	result, err := s.List(ctx, scope, document)
	if err != nil {
		return result, err
	}
	result.Preview, result.References, err = s.importer.Preview(ctx, scope.Tenant, scope.KB, result.Parent, result.ParentVersion)
	return result, err
}

func (s *Service) Import(ctx context.Context, scope Scope, c Command) ([]attachment.Receipt, error) {
	return s.importer.ImportSelected(ctx, scope.Tenant, scope.KB, c.Parent, c.ParentVersion, c.Fingerprints)
}

func (s *Service) Upload(ctx context.Context, scope Scope, parent, version, name string, data []byte) (attachment.Receipt, error) {
	return s.importer.Upload(ctx, scope.Tenant, scope.KB, parent, version, name, data)
}

func (s *Service) Edit(ctx context.Context, scope Scope, action, actor, operation string, revision int64, c Command) (store.AttachmentReviewResult, error) {
	var result store.AttachmentReviewResult
	if actor == "" || !weknora.ValidID(operation) || len(operation) > 128 || revision < 0 || c.Parent == c.Document ||
		(action == "confirm" && c.ReferenceID == nil) {
		return result, business.ErrInvalid
	}
	title := strings.TrimSpace(c.Title)
	if len(title) > 1024 || strings.ContainsFunc(title, unicode.IsControl) {
		return result, business.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	docs, err := s.documents(ctx, scope, c.Parent, c.Document)
	if err != nil {
		return result, err
	}
	parent, child := docs[c.Parent], docs[c.Document]
	if parent.ID == "" || child.ID == "" || parent.IsAttachment || !child.IsAttachment || child.ParentDocumentID != c.Parent ||
		parent.FileHash == "" || parent.FileHash != c.ParentVersion || child.FileHash == "" || child.FileHash != c.DocumentVersion ||
		parent.ParseStatus == "deleting" || child.ParseStatus == "deleting" {
		return result, store.ErrAttachmentConflict
	}
	var evidence store.AttachmentReference
	if action == "confirm" && *c.ReferenceID != "" {
		name, _, data, err := s.importer.Source(ctx, scope.Tenant, scope.KB, parent.ID, c.ParentVersion)
		if err != nil {
			return result, err
		}
		found := false
		for _, option := range attachment.ReferenceOptions(name, data) {
			if option.ID == *c.ReferenceID {
				evidence, found = option.AttachmentReference, true
				break
			}
		}
		if !found {
			return result, store.ErrAttachmentConflict
		}
	}
	if action != "confirm" {
		title = ""
	}
	result, err = s.db.AttachmentReview(ctx, store.AttachmentReviewEdit{Tenant: scope.Tenant, KB: scope.KB,
		Actor: actor, Operation: operation, Action: action, Parent: parent.ID, ParentHash: c.ParentVersion,
		Document: child.ID, DocumentHash: c.DocumentVersion, ImportID: child.ImportID, PreviousTitle: c.PreviousTitle,
		NextTitle: title, NativeTitle: child.Title, ExpectedRevision: revision, Reference: evidence})
	if err == nil && result.TitleState == "pending" {
		_ = s.syncTitles(ctx, scope.Tenant)
		if state, readErr := s.db.AttachmentTitleStatus(ctx, scope.Tenant, scope.KB, child.ID); readErr == nil && state != "" {
			result.TitleState = state
		}
	}
	return result, err
}

// SyncTitles resumes pending native writes after a failure or restart.
func (s *Service) SyncTitles(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.syncTitles(ctx, s.client.TenantID())
}

func (s *Service) syncTitles(ctx context.Context, tenant uint64) error {
	updates, err := s.db.PendingAttachmentTitles(ctx, tenant)
	if err != nil {
		return err
	}
	var failures error
	for _, update := range updates {
		docs, err := s.documents(ctx, Scope{Tenant: tenant, KB: update.KB}, update.Document)
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		doc := docs[update.Document]
		status := "synced"
		switch {
		case doc.ID == "" || doc.FileHash != update.Version || doc.ParseStatus == "deleting":
			status = "conflict"
		case doc.Title == update.Title:
			// The earlier HTTP request may have succeeded without an acknowledgement.
		case doc.Title != update.Previous:
			status = "conflict"
		default:
			var response struct{ Data struct{ Title string } }
			err = s.client.JSON(ctx, http.MethodPut, "/api/v1/knowledge/"+url.PathEscape(update.Document), nil, map[string]string{
				"title": update.Title, "expected_title": update.Previous, "expected_file_hash": update.Version,
			}, &response)
			var upstream *weknora.UpstreamError
			if errors.As(err, &upstream) && upstream.Status == http.StatusConflict {
				status = "conflict"
			} else if err != nil {
				failures = errors.Join(failures, err)
				continue
			} else if response.Data.Title != update.Title {
				failures = errors.Join(failures, business.ErrUnavailable)
				continue
			}
		}
		failures = errors.Join(failures, s.db.CompleteAttachmentTitle(ctx, tenant, update, status))
	}
	return failures
}
