// Command attachment-backfill populates semantic attachment metadata for
// documents imported before the semantic mapping schema existed.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ybochuan/LanRAG-Next/extensions/parser/attachment"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	_ "modernc.org/sqlite"
)

type nativeDocument struct {
	ID       string
	TenantID uint64
	KBID     string
	Title    string
	FileName string
	FileHash string
}

type attachmentChild struct {
	ID       string
	FileName string
	Status   string
}

type parentWork struct {
	Document nativeDocument
	Children []attachmentChild
}

func main() {
	extDB := flag.String("ext-db", "", "extension SQLite database path")
	nativeDB := flag.String("native-db", "", "native WeKnora SQLite database path")
	filesRoot := flag.String("files-root", "", "native file storage root (contains <tenant>/<document-id>/...)")
	tenant := flag.Uint64("tenant", 1, "tenant id to backfill")
	kb := flag.String("kb", "", "optional knowledge base id filter")
	includeDisabled := flag.Bool("include-disabled", false, "include disabled attachments")
	dryRun := flag.Bool("dry-run", false, "analyze and print without writing extension rows")
	audit := flag.Bool("audit-ownership", false, "count attachment ownership records without writing either database")
	planLegacy := flag.Bool("plan-legacy-ownership", false, "re-extract local originals and emit a read-only legacy ownership plan")
	applyLegacy := flag.String("apply-legacy-ownership", "", "apply only the entries in this reviewed and revalidated plan")
	flag.Parse()
	if *planLegacy || *applyLegacy != "" {
		if *audit || *planLegacy && *applyLegacy != "" || *dryRun {
			log.Fatal("choose one ownership mode")
		}
		if err := legacyOwnership(context.Background(), *extDB, *nativeDB, *filesRoot, *tenant, *kb, *applyLegacy, os.Stdout); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *audit {
		if err := auditOwnership(context.Background(), *extDB, *nativeDB, *tenant, *kb, os.Stdout); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *extDB == "" || *nativeDB == "" || *filesRoot == "" {
		log.Fatal("-ext-db, -native-db and -files-root are required")
	}
	if err := run(context.Background(), *extDB, *nativeDB, *filesRoot, *tenant, *kb, *includeDisabled, *dryRun); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, extDB, nativeDB, filesRoot string, tenant uint64, kb string, includeDisabled, dryRun bool) error {
	db, err := store.OpenDurable(ctx, extDB)
	if err != nil {
		return fmt.Errorf("open extension database: %w", err)
	}
	defer db.Close()

	native, err := sql.Open("sqlite", "file:"+filepath.ToSlash(nativeDB)+"?mode=ro&_pragma=query_only(ON)")
	if err != nil {
		return fmt.Errorf("open native database: %w", err)
	}
	defer native.Close()

	parents, err := loadParents(ctx, native, tenant, kb, includeDisabled)
	if err != nil {
		return err
	}
	total := 0
	written := 0
	for _, parent := range parents {
		records, err := analyzeParent(filesRoot, parent)
		if err != nil {
			log.Printf("skip parent %s (%s): %v", parent.Document.Title, parent.Document.ID, err)
			continue
		}
		total += len(records)
		if dryRun {
			for _, record := range records {
				log.Printf("dry-run %s -> %s (%s, %.2f)", record.OriginalFilename, record.DisplayName, record.MappingStatus, record.MappingConfidence)
			}
			continue
		}
		if err := db.SaveAttachmentSemantics(ctx, records); err != nil {
			return fmt.Errorf("save semantics for parent %s: %w", parent.Document.ID, err)
		}
		written += len(records)
	}
	log.Printf("attachment backfill: parents=%d analyzed=%d written=%d dry_run=%v", len(parents), total, written, dryRun)
	return nil
}

func loadParents(ctx context.Context, native *sql.DB, tenant uint64, kb string, includeDisabled bool) ([]parentWork, error) {
	query := `SELECT id, tenant_id, knowledge_base_id, COALESCE(title,''), COALESCE(file_name,''), COALESCE(file_hash,''),
		COALESCE(parse_status,''), COALESCE(enable_status,''), metadata
		FROM knowledges WHERE tenant_id=? AND (deleted_at IS NULL OR deleted_at='')`
	args := []any{int64(tenant)}
	if kb != "" {
		query += ` AND knowledge_base_id=?`
		args = append(args, kb)
	}
	rows, err := native.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read native documents: %w", err)
	}
	defer rows.Close()

	children := map[string][]attachmentChild{}
	parents := map[string]nativeDocument{}
	for rows.Next() {
		var doc nativeDocument
		var parseStatus, enableStatus string
		var metadata []byte
		if err := rows.Scan(&doc.ID, &doc.TenantID, &doc.KBID, &doc.Title, &doc.FileName, &doc.FileHash, &parseStatus, &enableStatus, &metadata); err != nil {
			return nil, err
		}
		var fields map[string]json.RawMessage
		if len(metadata) > 0 {
			if err := json.Unmarshal(metadata, &fields); err != nil {
				continue
			}
		}
		var isAttachment, parent string
		if raw, ok := fields["is_attachment"]; ok {
			_ = json.Unmarshal(raw, &isAttachment)
		}
		if raw, ok := fields["parent_document_id"]; ok {
			_ = json.Unmarshal(raw, &parent)
		}
		if isAttachment == "true" {
			if parent == "" || (!includeDisabled && (parseStatus == "deleting" || enableStatus == "disabled")) {
				continue
			}
			children[parent] = append(children[parent], attachmentChild{ID: doc.ID, FileName: doc.FileName})
			continue
		}
		parents[doc.ID] = doc
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := make([]parentWork, 0, len(children))
	for parentID, kids := range children {
		parent, ok := parents[parentID]
		if !ok {
			continue
		}
		sort.Slice(kids, func(i, j int) bool { return kids[i].FileName < kids[j].FileName })
		result = append(result, parentWork{Document: parent, Children: kids})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Document.ID < result[j].Document.ID })
	return result, nil
}

func analyzeParent(filesRoot string, parent parentWork) ([]store.AttachmentSemantic, error) {
	files, err := parentFiles(filesRoot, parent.Document)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no source file found")
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		return nil, err
	}
	name := filepath.Base(files[0])
	extracted, err := attachment.DefaultRegistry().Extract(name, data)
	if err != nil {
		return nil, err
	}
	semantics := attachment.Analyze(name, data, parent.Document.Title, extracted)
	byName := map[string]attachment.Semantic{}
	for _, semantic := range semantics {
		byName[strings.ToLower(semantic.OriginalFilename)] = semantic
	}
	parentTitle := strings.TrimSuffix(parent.Document.Title, filepath.Ext(parent.Document.Title))
	records := make([]store.AttachmentSemantic, 0, len(parent.Children))
	for index, child := range parent.Children {
		semantic, ok := byName[strings.ToLower(child.FileName)]
		if !ok {
			semantic = attachment.Semantic{
				OriginalFilename:   child.FileName,
				DisplayName:        child.FileName,
				AttachmentSequence: index + 1,
				MappingStatus:      attachment.MappingUnmatched,
				IndexPolicy:        "default",
				ParentTitle:        parentTitle,
			}
		}
		hash := parent.Document.FileHash
		if hash == "" {
			hash = semantic.ParentFileHash
		}
		records = append(records, store.AttachmentSemantic{
			AttachmentID:       child.ID,
			TenantID:           parent.Document.TenantID,
			KnowledgeBaseID:    parent.Document.KBID,
			ParentDocumentID:   parent.Document.ID,
			ParentFileHash:     hash,
			ParentTitle:        semantic.ParentTitle,
			OriginalFilename:   semantic.OriginalFilename,
			DisplayName:        semantic.DisplayName,
			AttachmentSequence: semantic.AttachmentSequence,
			ReferenceTitle:     semantic.ReferenceTitle,
			ReferenceLocation:  semantic.ReferenceLocation,
			ReferenceExcerpt:   semantic.ReferenceExcerpt,
			MappingStatus:      semantic.MappingStatus,
			MappingConfidence:  semantic.MappingConfidence,
			IndexPolicy:        semantic.IndexPolicy,
		})
	}
	return records, nil
}

func parentFiles(root string, parent nativeDocument) ([]string, error) {
	dir := filepath.Join(root, strconv.FormatUint(parent.TenantID, 10), parent.ID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), filepath.Ext(parent.FileName)) {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}
