package management

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/ybochuan/LanRAG-Next/extensions/business"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"io"
	"strings"
	"time"
)

func businessRead(action string) bool {
	return action == "business_related" || action == "business_relation_list" || action == "business_feedback_list" || action == "business_guidance_list" || action == "business_revision_list"
}
func (s *Server) executeBusiness(ctx context.Context, r Request) (any, error) {
	b := s.business
	if b == nil {
		b = business.New(s.db, s.sources, time.Now)
	}
	var body struct {
		ID      string           `json:"id"`
		Payload business.Payload `json:"payload"`
	}
	if len(r.Business) > 0 {
		dec := json.NewDecoder(bytes.NewReader(r.Business))
		dec.DisallowUnknownFields()
		if dec.Decode(&body) != nil || dec.Decode(new(any)) != io.EOF {
			return nil, business.ErrInvalid
		}
	}
	c := business.Command{Scope: provenance.Scope{TenantID: r.TenantID, KnowledgeBaseID: r.KnowledgeBaseID}, ActorID: r.ActorID, Action: strings.TrimPrefix(r.Action, "business_"), OperationID: r.OperationID, ID: body.ID, ExpectedRevision: r.ExpectedRevision, Payload: body.Payload}
	c.Page = r.Page
	c.PageSize = r.PageSize
	switch c.Action {
	case "related":
		return b.QueryRelated(ctx, c.Scope, r.DocumentID)
	case "relation_list", "feedback_list", "guidance_list", "revision_list":
		return b.List(ctx, c)
	case "revision_publish":
		return b.Publish(ctx, c.Scope, c.ID)
	default:
		return b.Execute(ctx, c)
	}
}
