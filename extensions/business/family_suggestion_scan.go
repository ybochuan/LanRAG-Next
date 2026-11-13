package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lineage"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

func (s *Service) queueFamilySuggestionScan(ctx context.Context, c Command) (Result, error) {
	if !validID(c.ID) || c.ExpectedRevision != 0 {
		return Result{}, ErrInvalid
	}
	return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		if _, err := suggestionFamilyRevision(ctx, tx, c.Scope, c.ID); err != nil {
			return Result{}, err
		}
		var revision int64
		err := tx.QueryRowContext(ctx, `INSERT INTO ext_family_suggestion_scans
			VALUES(?,?,?,1,?,'queued') ON CONFLICT(tenant_id,knowledge_base_id,business_family_id)
			DO UPDATE SET revision=ext_family_suggestion_scans.revision+1,requested_at=excluded.requested_at,status='queued'
			WHERE ext_family_suggestion_scans.status<>'queued' RETURNING revision`,
			args(c.Scope, c.ID, s.now().UTC().Format(time.RFC3339Nano))...).Scan(&revision)
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrConflict
		}
		return Result{ID: c.ID, Revision: revision, Status: "queued"}, err
	})
}

// RunFamilySuggestionScans serves durable scan requests independently of ingestion and approvals.
func (s *Service) RunFamilySuggestionScans(ctx context.Context, scopes []provenance.Scope, report func(error)) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		for _, scope := range scopes {
			if err := s.sweepFamilySuggestionScan(ctx, scope); err != nil && ctx.Err() == nil {
				report(err)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) sweepFamilySuggestionScan(ctx context.Context, scope provenance.Scope) error {
	var family string
	var revision int64
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT business_family_id,revision FROM ext_family_suggestion_scans
			WHERE tenant_id=? AND knowledge_base_id=? AND status='queued'
			ORDER BY requested_at,business_family_id LIMIT 1`, args(scope)...).Scan(&family, &revision)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	scanErr := s.discoverFamilySuggestions(ctx, scope, family)
	if ctx.Err() != nil {
		return ctx.Err() // Pending work can resume on the next worker start.
	}
	status := "completed"
	if scanErr != nil {
		status = "failed"
	}
	err = s.db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE ext_family_suggestion_scans SET status=?
			WHERE tenant_id=? AND knowledge_base_id=? AND business_family_id=? AND revision=? AND status='queued'`,
			append([]any{status}, args(scope, family, revision)...)...)
		return err
	})
	return errors.Join(scanErr, err)
}

type scanDocument struct {
	doc    weknora.AttachmentDocument
	family string
}

type suggestionObservation struct {
	version string
	chunks  []string
}

const suggestionScanPageSize = 64

func (s *Service) suggestionDocuments(ctx context.Context, scope provenance.Scope, client *weknora.Client, family, after string) ([]scanDocument, string, error) {
	ids := make([]string, 0, suggestionScanPageSize)
	members := make(map[string]string)
	asOf := s.now().UTC().Format(time.RFC3339Nano)
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT c.document_id,COALESCE(m.business_family_id,'')
			FROM ext_lifecycle_current c `+store.LifecycleEventJoinSQL+`
			`+store.LifecycleAnnotationJoinSQL+`
			LEFT JOIN ext_business_document_family_members m USING(tenant_id,knowledge_base_id,document_id)
			WHERE c.tenant_id=? AND c.knowledge_base_id=? AND c.document_id>? AND (?='' OR m.business_family_id=?)
			AND c.state='active' AND `+store.LifecycleReadableSQL+` AND NOT `+store.PublicationTargetPendingSQL+`
			AND (a.effective_from IS NULL OR a.effective_from='' OR a.effective_from<=?)
			AND (a.effective_until IS NULL OR a.effective_until='' OR a.effective_until>=?)
			ORDER BY c.document_id LIMIT ?`, args(scope, after, family, family, asOf, asOf, suggestionScanPageSize)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, memberOf string
			if err := rows.Scan(&id, &memberOf); err != nil {
				return err
			}
			ids = append(ids, id)
			members[id] = memberOf
		}
		return rows.Err()
	})
	if err != nil || len(ids) == 0 {
		return nil, "", err
	}
	native, err := client.AttachmentDocuments(ctx, []string{scope.KnowledgeBaseID}, ids)
	if err != nil {
		return nil, "", err
	}
	out := make([]scanDocument, 0, len(native))
	for _, doc := range native {
		if doc.IsAttachment || doc.ImportID != "" || doc.ParentDocumentID != "" || doc.ParseStatus != "completed" ||
			doc.EnableStatus != "enabled" || strings.TrimSpace(doc.Title) == "" || strings.HasPrefix(doc.Channel, weknora.SplitPublicationChannelPrefix) {
			continue
		}
		out = append(out, scanDocument{doc: doc, family: members[doc.ID]})
	}
	next := ""
	if len(ids) == suggestionScanPageSize {
		next = ids[len(ids)-1]
	}
	return out, next, nil
}

func (s *Service) discoverFamilySuggestions(ctx context.Context, scope provenance.Scope, family string) error {
	native, ok := s.remote.(*NativePublisher)
	if !ok || native == nil || native.client == nil {
		return ErrUnavailable
	}
	var policy SuggestionPolicy
	var published string
	if err := s.db.View(ctx, func(tx *sql.Tx) error {
		var err error
		policy, err = readSuggestionPolicy(ctx, tx, scope)
		if err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT document_id FROM ext_business_family_published_heads
			WHERE tenant_id=? AND knowledge_base_id=? AND business_family_id=?),'')`, args(scope, family)...).Scan(&published)
	}); err != nil {
		return err
	}
	var anchors []scanDocument
	for after := ""; ; {
		docs, next, err := s.suggestionDocuments(ctx, scope, native.client, family, after)
		if err != nil {
			return err
		}
		anchors = append(anchors, docs...)
		if next == "" {
			break
		}
		after = next
	}
	if len(anchors) == 0 {
		return nil
	}
	semantic := &familyScanSemantic{anchors: make(map[string][]float64), disabled: s.familyEmbedder == nil}
	for after := ""; ; {
		peers, next, err := s.suggestionDocuments(ctx, scope, native.client, "", after)
		if err != nil {
			return err
		}
		if err := s.discoverSuggestionPage(ctx, scope, family, published, policy, anchors, peers, semantic); err != nil {
			return err
		}
		if next == "" {
			return nil
		}
		after = next
	}
}

type familyScanSemantic struct {
	anchors  map[string][]float64
	disabled bool
}

func (s *Service) discoverSuggestionPage(ctx context.Context, scope provenance.Scope, family, published string, p SuggestionPolicy, anchors, peers []scanDocument, semantic *familyScanSemantic) error {
	observations := map[string]suggestionObservation{}
	observe := func(doc weknora.AttachmentDocument) (suggestionObservation, error) {
		if existing, ok := observations[doc.ID]; ok {
			return existing, nil
		}
		result, err := s.observeSuggestionDocument(ctx, scope, doc)
		if errors.Is(err, ErrConflict) || errors.Is(err, lineage.ErrSourceMissing) {
			err = nil
		}
		if err == nil {
			observations[doc.ID] = result
		}
		return result, err
	}
	var vectors map[string][]float64
	if !semantic.disabled {
		var err error
		vectors, err = s.suggestionPageVectors(ctx, scope, anchors, peers, semantic, observe)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			// The remainder of this scan can still produce lexical candidates.
			semantic.disabled, vectors = true, nil
		}
	}
	best := map[string]FamilySuggestion{}
	for _, peer := range peers {
		for _, anchor := range anchors {
			if anchor.doc.ID == peer.doc.ID || peer.family == family && anchor.doc.ID > peer.doc.ID {
				continue
			}
			item, ok := candidateSignals(family, published, peer, anchor)
			if item.Kind != "" && vectors != nil {
				ok = applyFamilySemanticEvidence(&item.Evidence, vectors[peer.doc.ID], semantic.anchors[anchor.doc.ID], s.familyEmbedder.model) || ok
			}
			if !ok || p.confidence(item.Evidence)+p.StructureWeight < p.Threshold {
				continue
			}
			source, err := observe(peer.doc)
			if err != nil {
				return err
			}
			target, err := observe(anchor.doc)
			if err != nil {
				return err
			}
			if source.version == "" || target.version == "" {
				continue
			}
			if item.DocumentID != peer.doc.ID {
				source, target = target, source
			}
			item.SourceVersionID, item.TargetVersionID = source.version, target.version
			value, reason := structuralEvidence(source.chunks, target.chunks)
			item.Evidence.Structure = value
			if reason != "" {
				item.Evidence.Reasons = append(item.Evidence.Reasons, reason)
			}
			id := item.stableID()
			if current, ok := best[id]; !ok || p.confidence(item.Evidence) > p.confidence(current.Evidence) {
				best[id] = item
			}
		}
	}
	items := make([]FamilySuggestion, 0, len(best))
	for _, item := range best {
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil
	}
	return s.recordFamilySuggestions(ctx, scope, items)
}

func (s *Service) suggestionPageVectors(ctx context.Context, scope provenance.Scope, anchors, peers []scanDocument, semantic *familyScanSemantic,
	observe func(weknora.AttachmentDocument) (suggestionObservation, error)) (map[string][]float64, error) {
	page := make(map[string][]float64, len(peers))
	anchorIDs := make(map[string]bool, len(anchors))
	pending := make([]weknora.AttachmentDocument, 0, len(anchors)+len(peers))
	for _, item := range anchors {
		anchorIDs[item.doc.ID] = true
		if _, seen := semantic.anchors[item.doc.ID]; !seen {
			pending = append(pending, item.doc)
		}
	}
	for _, item := range peers {
		if !anchorIDs[item.doc.ID] {
			pending = append(pending, item.doc)
		}
	}
	for start := 0; start < len(pending); start += familyEmbeddingBatch {
		docs := pending[start:min(start+familyEmbeddingBatch, len(pending))]
		pins := make(map[string]string, len(docs))
		for _, doc := range docs {
			observed, err := observe(doc)
			if err != nil {
				return nil, err
			}
			if observed.version != "" {
				pins[doc.ID] = observed.version
			}
			if anchorIDs[doc.ID] {
				semantic.anchors[doc.ID] = nil // Remember ineligible anchors for this scan.
			}
		}
		allowed, err := s.authorizedEmbeddingDocuments(ctx, scope, docs, pins)
		if err != nil {
			return nil, err
		}
		if len(allowed) == 0 {
			continue
		}
		titles := make([]string, len(allowed))
		for i, doc := range allowed {
			titles[i] = doc.Title
		}
		batch, err := s.familyEmbedder.embed(ctx, titles)
		if err != nil {
			return nil, err
		}
		for i, doc := range allowed {
			if anchorIDs[doc.ID] {
				semantic.anchors[doc.ID] = batch[i]
			} else {
				page[doc.ID] = batch[i]
			}
		}
	}
	for _, item := range peers {
		if anchorIDs[item.doc.ID] {
			page[item.doc.ID] = semantic.anchors[item.doc.ID]
		}
	}
	return page, nil
}

func (s *Service) authorizedEmbeddingDocuments(ctx context.Context, scope provenance.Scope, docs []weknora.AttachmentDocument, pins map[string]string) ([]weknora.AttachmentDocument, error) {
	allowed := make([]weknora.AttachmentDocument, 0, len(docs))
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		for _, doc := range docs {
			version := pins[doc.ID]
			if version == "" {
				continue
			}
			err := s.current(ctx, tx, scope, doc.ID, version)
			if errors.Is(err, ErrConflict) {
				continue
			}
			if err != nil {
				return err
			}
			allowed = append(allowed, doc)
		}
		return nil
	})
	return allowed, err
}
func (s *Service) observeSuggestionDocument(ctx context.Context, scope provenance.Scope, doc weknora.AttachmentDocument) (suggestionObservation, error) {
	var out suggestionObservation
	version, err := s.verify(ctx, scope, doc.ID)
	if err != nil {
		return out, err
	}
	err = s.db.View(ctx, func(tx *sql.Tx) error {
		if err := requirePrimaryFamilyDocumentTx(ctx, tx, scope, doc.ID); err != nil {
			return err
		}
		held, err := store.LegacyPublicationHeldTx(ctx, tx, doc)
		if err != nil {
			return err
		}
		if held {
			return ErrConflict
		}
		if strings.HasPrefix(doc.Channel, weknora.RevisionPublicationChannelPrefix) {
			var allowed string
			if err := tx.QueryRowContext(ctx, `SELECT COALESCE(`+store.PublishedPublicationChannelSQL+`,'')
				FROM ext_lifecycle_current c WHERE c.tenant_id=? AND c.knowledge_base_id=? AND c.document_id=?`,
				args(scope, doc.ID)...).Scan(&allowed); err != nil {
				return err
			}
			if allowed != doc.Channel {
				return ErrConflict
			}
		}
		var raw string
		err = tx.QueryRowContext(ctx, `SELECT snapshot_json FROM ext_source_snapshot_evidence
			WHERE tenant_id=? AND knowledge_base_id=? AND version_node_id=?`, args(scope, version)...).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) && s.registry == nil {
			return nil
		}
		if err != nil {
			return err
		}
		var snapshot lineage.Snapshot
		if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
			return err
		}
		if s.registry != nil && (snapshot.HeaderFields["title"] != digest(doc.Title) || snapshot.HeaderFields["description"] != digest(doc.Description)) {
			return ErrConflict
		}
		for _, chunk := range snapshot.Chunks {
			out.chunks = append(out.chunks, chunk.ContentHash)
		}
		return nil
	})
	if err == nil {
		out.version = version
	}
	return out, err
}
