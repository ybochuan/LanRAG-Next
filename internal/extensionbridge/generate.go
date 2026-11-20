package extensionbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

type generationRequest struct {
	StepID              string               `json:"step_id"`
	CallID              string               `json:"call_id"`
	ConsumedToolOutputs []ConsumedToolOutput `json:"consumed_tool_outputs,omitempty"`
	CorrectionEvidence  bool                 `json:"correction_evidence,omitempty"`
	Query               string               `json:"query,omitempty"`
}

type generationResult struct {
	StepID            string `json:"step_id"`
	CallID            string `json:"call_id"`
	Content           string `json:"content"`
	AnnotationContent string `json:"annotation_content"`
	AnnotationPolicy  string `json:"annotation_policy"`
	Items             []struct {
		ID       string `json:"id"`
		Revision int64  `json:"revision"`
	} `json:"items"`
	Units             int                         `json:"units"`
	CorrectionContent string                      `json:"correction_content"`
	CorrectionItems   []types.CorrectionReference `json:"correction_items"`
}

// BeginGuidedSourceCall applies the trusted policy and existing source annotations
// in the leading system message. Reviewed correction evidence follows native
// evidence. The bridge fails open when guidance is unavailable.
func BeginGuidedSourceCall(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions, inputs []SourceInput, complete bool) (context.Context, []chat.Message, *SourceCall, error) {
	request := struct{ Messages, Options any }{messages, opts}
	ctx, call, err := BeginSourceCall(ctx, "model", request, inputs, complete)
	if err != nil {
		return ctx, messages, call, err
	}
	handle, tracked := SourceRunFromContext(ctx)
	step, _ := ctx.Value(sourceStepKey{}).(string)
	if ctx.Value(sourceUnknownKey{}) == true {
		step = ""
	}
	if !tracked || step == "" && ctx.Value(sourceUnknownKey{}) != true {
		return ctx, messages, call, nil
	}
	q := generationRequest{StepID: step, CallID: SourceFieldHash(request)}
	if step != "" {
		q.ConsumedToolOutputs = consumedToolOutputsFromContext(ctx)
		if query, _ := ctx.Value(correctionQueryKey{}).(string); query != "" {
			q.Query = query
			q.CorrectionEvidence = true
			q.CallID = SourceFieldHash([]string{q.CallID, query})
		}
	}
	call.annotationDelivery = "unavailable"
	var out generationResult
	if sourceCall(ctx, "/bridge/before-generate", sourceRequest{Scope: handle.Scope, Handle: &handle, Guidance: &q}, &out) != nil || out.StepID != step || out.CallID != q.CallID || len(out.Items) > 3 {
		return ctx, messages, call, nil
	}
	if !validCorrectionResult(q, out, inputs) {
		return ctx, messages, call, nil
	}
	if len(out.Items) == 0 && len(out.CorrectionItems) == 0 {
		if out.Content == "" && out.AnnotationContent == "" && out.AnnotationPolicy == "" && out.Units == 0 {
			call.annotationDelivery = ""
		}
		return ctx, messages, call, nil
	}
	units := 0
	if out.Content != "" {
		units += len(out.Content) + 16
	}
	if (out.AnnotationContent == "" && out.CorrectionContent == "") != (out.AnnotationPolicy == "") || len(out.AnnotationPolicy) > 800 {
		return ctx, messages, call, nil
	}
	if out.AnnotationContent != "" {
		if step == "" {
			return ctx, messages, call, nil
		}
		units += len(out.AnnotationContent) + 16
	}
	if (units == 0) != (len(out.Items) == 0) || units != out.Units || units > 800 {
		return ctx, messages, call, nil
	}
	seen := make(map[string]bool, len(out.Items))
	for _, item := range out.Items {
		if !sourceID(item.ID) || item.Revision < 1 || seen[item.ID] {
			return ctx, messages, call, nil
		}
		seen[item.ID] = true
	}
	messages = append([]chat.Message(nil), messages...)
	if out.AnnotationContent != "" || out.CorrectionContent != "" {
		annotation := out.AnnotationPolicy
		if out.AnnotationContent != "" {
			annotation += "\n\n" + out.AnnotationContent
		}
		if len(messages) > 0 && messages[0].Role == "system" && len(messages[0].MultiContent) == 0 {
			messages[0].Content += "\n\n" + annotation
		} else if len(messages) == 0 || messages[0].Role != "system" {
			messages = append([]chat.Message{{Role: "system", Content: annotation}}, messages...)
		} else {
			return ctx, messages, call, nil
		}
	}
	if out.Content != "" {
		messages = append(messages, chat.Message{Role: "user", Content: out.Content})
	}
	if out.CorrectionContent != "" {
		data := out.CorrectionContent + correctionSourceLabels(ctx, out.CorrectionItems)
		last := &messages[len(messages)-1]
		if last.Role == "user" && len(last.MultiContent) == 0 {
			last.Content += "\n\n" + data
		} else {
			messages = append(messages, chat.Message{Role: "user", Content: data})
		}
	}
	body, err := json.Marshal(struct{ Messages, Options any }{messages, opts})
	if err != nil {
		return ctx, messages, call, err
	}
	call.inputHash = SourceHash(body)
	call.annotationDelivery = ""
	if out.AnnotationContent != "" || len(out.CorrectionItems) > 0 {
		call.annotationDelivery = "applied"
	}
	call.correctionItems = out.CorrectionItems
	return ctx, messages, call, nil
}

// CorrectionItems returns the reviewed evidence included in this provider request.
func (c *SourceCall) CorrectionItems() []types.CorrectionReference {
	if c == nil {
		return nil
	}
	return c.correctionItems
}

func correctionSourceLabels(ctx context.Context, items []types.CorrectionReference) string {
	resolve, _ := ctx.Value(correctionSourceHandlesKey{}).(func(string) string)
	if resolve == nil {
		return ""
	}
	var labels strings.Builder
	for i, item := range items {
		if handle := resolve(item.ChunkID); handle != "" {
			if labels.Len() == 0 {
				labels.WriteString("\n【勘误对应的本轮原文引用】\n")
			}
			fmt.Fprintf(&labels, "- 勘误 %d：%s\n", i+1, handle)
		}
	}
	return labels.String()
}

func validCorrectionResult(q generationRequest, out generationResult, inputs []SourceInput) bool {
	if out.CorrectionContent == "" {
		return len(out.CorrectionItems) == 0
	}
	if !q.CorrectionEvidence || q.StepID == "" || len(out.CorrectionContent) > 6000 || len(out.CorrectionItems) == 0 || len(out.CorrectionItems) > 6 {
		return false
	}
	chunks := make(map[[2]string]bool, len(inputs))
	for _, input := range inputs {
		if input.Field == "content" && input.ChunkID != "" {
			chunks[[2]string{input.DocumentID, input.ChunkID}] = true
		}
	}
	seen := make(map[string]bool, len(out.CorrectionItems)+len(out.Items))
	for _, item := range out.Items {
		seen[item.ID] = true
	}
	for _, item := range out.CorrectionItems {
		if !sourceID(item.ID) || item.Revision < 1 || !sourceID(item.VersionID) || !sourceID(item.DocumentID) || !sourceID(item.ChunkID) || seen[item.ID] {
			return false
		}
		// Agent evidence uses the committed tool output proof verified by Host.
		if len(q.ConsumedToolOutputs) == 0 && !chunks[[2]string{item.DocumentID, item.ChunkID}] {
			return false
		}
		seen[item.ID] = true
	}
	return true
}
