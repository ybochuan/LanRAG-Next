package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/agent"
	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"github.com/Tencent/WeKnora/internal/types"
)

var governedDocuments = extensionbridge.AllowedDocuments
var rejectUnattributed = extensionbridge.RejectUnattributed

func filterGovernedKnowledge(ctx context.Context, rows []*types.Knowledge) ([]*types.Knowledge, error) {
	type scope struct {
		tenant uint64
		kb     string
	}
	groups := map[scope][]string{}
	for _, k := range rows {
		if k != nil {
			tenant := k.TenantID
			if tenant == 0 {
				tenant, _ = types.TenantIDFromContext(ctx)
			}
			s := scope{tenant, k.KnowledgeBaseID}
			groups[s] = append(groups[s], k.ID)
		}
	}
	allowed := map[scope]map[string]bool{}
	for s, ids := range groups {
		idsAllowed, err := governedDocuments(ctx, s.tenant, s.kb, ids)
		if err != nil {
			return nil, err
		}
		allowed[s] = idsAllowed
	}
	kept := make([]*types.Knowledge, 0, len(rows))
	for _, k := range rows {
		if k != nil {
			tenant := k.TenantID
			if tenant == 0 {
				tenant, _ = types.TenantIDFromContext(ctx)
			}
			if allowed[scope{tenant, k.KnowledgeBaseID}][k.ID] {
				kept = append(kept, k)
			}
		}
	}
	return kept, nil
}

func governPromptKBInfo(ctx context.Context, infos []*agent.KnowledgeBaseInfo, owners map[string]uint64) ([]*agent.KnowledgeBaseInfo, error) {
	out := make([]*agent.KnowledgeBaseInfo, 0, len(infos))
	for _, info := range infos {
		if info == nil {
			continue
		}
		copyInfo := *info
		// Aggregate text cannot establish complete document provenance. Keep only
		// static routing capabilities and metadata individually permitted by Host.
		copyInfo.Description = ""
		copyInfo.Profile = nil
		copyInfo.DocCount = 0
		ids := []string{}
		for _, doc := range info.RecentDocs {
			ids = append(ids, doc.KnowledgeID)
		}
		tenant := owners[info.ID]
		if tenant == 0 {
			tenant, _ = types.TenantIDFromContext(ctx)
		}
		allowed, err := governedDocuments(ctx, tenant, info.ID, ids)
		if err != nil {
			return nil, err
		}
		// Recent-doc enumeration is KB-wide, not necessarily the turn's narrower
		// document/tag authorization. Do not inject it implicitly into the model.
		_ = allowed
		copyInfo.RecentDocs = nil
		out = append(out, &copyInfo)
	}
	return out, nil
}
