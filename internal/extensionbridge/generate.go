package extensionbridge

import (
	"context"
	"encoding/json"

	"github.com/Tencent/WeKnora/internal/models/chat"
)

type generationRequest struct {
	StepID              string               `json:"step_id"`
	CallID              string               `json:"call_id"`
	ConsumedToolOutputs []ConsumedToolOutput `json:"consumed_tool_outputs,omitempty"`
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
	Units int `json:"units"`
}

// BeginGuidedSourceCall applies verified source annotations in the leading
// system message. Legacy guidance keeps its trailing user message. The bridge
// fails open and leaves the original messages in place when guidance is unavailable.
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
	}
	call.annotationDelivery = "unavailable"
	var out generationResult
	if sourceCall(ctx, "/bridge/before-generate", sourceRequest{Scope: handle.Scope, Handle: &handle, Guidance: &q}, &out) != nil || out.StepID != step || out.CallID != q.CallID || len(out.Items) > 3 {
		return ctx, messages, call, nil
	}
	if len(out.Items) == 0 {
		if out.Content == "" && out.AnnotationContent == "" && out.AnnotationPolicy == "" && out.Units == 0 {
			call.annotationDelivery = ""
		}
		return ctx, messages, call, nil
	}
	units := 0
	if out.Content != "" {
		units += len(out.Content) + 16
	}
	if (out.AnnotationContent == "") != (out.AnnotationPolicy == "") || len(out.AnnotationPolicy) > 800 {
		return ctx, messages, call, nil
	}
	if out.AnnotationContent != "" {
		if step == "" {
			return ctx, messages, call, nil
		}
		units += len(out.AnnotationContent) + 16
	}
	if units == 0 || units != out.Units || units > 800 {
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
	if out.AnnotationContent != "" {
		annotation := out.AnnotationPolicy + "\n\n" + out.AnnotationContent
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
	body, err := json.Marshal(struct{ Messages, Options any }{messages, opts})
	if err != nil {
		return ctx, messages, call, err
	}
	call.inputHash = SourceHash(body)
	call.annotationDelivery = ""
	if out.AnnotationContent != "" {
		call.annotationDelivery = "applied"
	}
	return ctx, messages, call, nil
}
