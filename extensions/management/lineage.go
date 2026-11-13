package management

import (
	"context"
	"errors"

	"github.com/ybochuan/LanRAG-Next/extensions/lineage"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
)

func (s *Server) lineageGraph(ctx context.Context, r Request, root string) (any, error) {
	mode := r.GraphMode
	if mode == "" {
		mode = "version-ancestry"
	}
	graph, err := lineage.NewRegistry(s.db, nil).WithKnowledgeBases(s.knowledgeBases(r.TenantID)).Graph(ctx, provenance.Scope{TenantID: r.TenantID, KnowledgeBaseID: r.KnowledgeBaseID}, mode, root)
	if err != nil {
		if !errors.Is(err, lineage.ErrEvidence) && !errors.Is(err, lineage.ErrTraversalLimit) {
			return nil, errDatabase
		}
		return nil, err
	}
	byKB := map[string][]string{}
	seen := map[string]bool{}
	for _, node := range graph.Nodes {
		if node.DocumentID == "" {
			continue
		}
		kb := node.KnowledgeBaseID
		if kb == "" {
			kb = r.KnowledgeBaseID
		}
		key := kb + "\x00" + node.DocumentID
		if !seen[key] {
			seen[key] = true
			byKB[kb] = append(byKB[kb], node.DocumentID)
		}
	}
	graph.TitlesUnavailable = len(byKB) > 0 && s.titles == nil
	if len(byKB) > 0 && s.titles != nil {
		titlesByKB := map[string]map[string]string{}
		for kb, ids := range byKB {
			titles, err := s.titles(ctx, kb, ids)
			if err != nil {
				graph.TitlesUnavailable = true
				continue
			}
			titlesByKB[kb] = titles
		}
		for i := range graph.Nodes {
			kb := graph.Nodes[i].KnowledgeBaseID
			if kb == "" {
				kb = r.KnowledgeBaseID
			}
			if title := titlesByKB[kb][graph.Nodes[i].DocumentID]; title != "" {
				graph.Nodes[i].Title = title
			}
		}
	}
	return graph, nil
}
