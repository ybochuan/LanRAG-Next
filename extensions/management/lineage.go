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
	graph, err := lineage.NewRegistry(s.db, nil).Graph(ctx, provenance.Scope{TenantID: r.TenantID, KnowledgeBaseID: r.KnowledgeBaseID}, mode, root)
	if err != nil {
		if !errors.Is(err, lineage.ErrEvidence) && !errors.Is(err, lineage.ErrTraversalLimit) {
			return nil, errDatabase
		}
		return nil, err
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, node := range graph.Nodes {
		if node.DocumentID != "" && !seen[node.DocumentID] {
			ids = append(ids, node.DocumentID)
			seen[node.DocumentID] = true
		}
	}
	graph.TitlesUnavailable = len(ids) > 0 && s.titles == nil
	if len(ids) > 0 && s.titles != nil {
		titles, err := s.titles(ctx, r.KnowledgeBaseID, ids)
		if err != nil {
			graph.TitlesUnavailable = true
		} else {
			for i := range graph.Nodes {
				graph.Nodes[i].Title = titles[graph.Nodes[i].DocumentID]
			}
		}
	}
	return graph, nil
}
