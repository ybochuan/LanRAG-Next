package extensionbridge

import (
	"context"
	"encoding/json"

	"github.com/Tencent/WeKnora/internal/models/chat"
)

type generationRequest struct {
	StepID string `json:"step_id"`
	CallID string `json:"call_id"`
}

type generationResult struct {
	StepID  string `json:"step_id"`
	CallID  string `json:"call_id"`
	Content string `json:"content"`
	Items   []struct {
		ID       string `json:"id"`
		Revision int64  `json:"revision"`
	} `json:"items"`
	Units int `json:"units"`
}

// BeginGuidedSourceCall appends an optional instruction after the cached prefix.
// A guidance failure preserves the original messages and provenance flow.
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
	var out generationResult
	if sourceCall(ctx, "/bridge/before-generate", sourceRequest{Scope: handle.Scope, Handle: &handle, Guidance: &q}, &out) != nil || out.StepID != step || out.CallID != q.CallID || len(out.Items) == 0 || len(out.Items) > 3 || out.Content == "" || out.Units != len(out.Content)+16 || out.Units > 800 {
		return ctx, messages, call, nil
	}
	seen := make(map[string]bool, len(out.Items))
	for _, item := range out.Items {
		if !sourceID(item.ID) || item.Revision < 1 || seen[item.ID] {
			return ctx, messages, call, nil
		}
		seen[item.ID] = true
	}
	messages = append(append([]chat.Message(nil), messages...), chat.Message{Role: "system", Content: out.Content})
	body, err := json.Marshal(struct{ Messages, Options any }{messages, opts})
	if err != nil {
		return ctx, messages, call, err
	}
	call.inputHash = SourceHash(body)
	return ctx, messages, call, nil
}
