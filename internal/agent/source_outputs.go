package agent

import (
	"context"
	"encoding/json"
	"sync"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

type searchOutputReceipt struct {
	output       extensionbridge.ConsumedToolOutput
	runID        string
	result       *types.ToolResult
	renderedHash string
}

// searchOutputReceipts follows successful native search results from tool
// execution into the live model messages. Calls may finish in parallel.
type searchOutputReceipts struct {
	mu     sync.Mutex
	byCall map[string]searchOutputReceipt
}

func (r *searchOutputReceipts) record(ctx context.Context, output extensionbridge.ConsumedToolOutput, result *types.ToolResult) {
	run, tracked := extensionbridge.SourceRunFromContext(ctx)
	if !tracked || output.StepID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byCall == nil {
		r.byCall = make(map[string]searchOutputReceipt)
	}
	if _, duplicate := r.byCall[output.ToolCallID]; duplicate {
		r.byCall[output.ToolCallID] = searchOutputReceipt{}
		return
	}
	r.byCall[output.ToolCallID] = searchOutputReceipt{output: output, runID: run.RunID, result: result}
}

// bind accepts only the actual result object and the unmodified result bytes
// recorded by the tool step. A replayed result has no request-local receipt.
func (r *searchOutputReceipts) bind(call types.ToolCall, message chat.Message) {
	if call.Name != agenttools.ToolSearchKnowledge || call.Result == nil || !call.Result.Success {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	receipt := r.byCall[call.ID]
	if receipt.result != call.Result || message.Content == "" {
		return
	}
	body, err := json.Marshal(call.Result)
	if err != nil || extensionbridge.SourceHash(append(body, '\n')) != receipt.output.OutputHash {
		return
	}
	receipt.renderedHash = extensionbridge.SourceFieldHash(message.Content)
	r.byCall[call.ID] = receipt
}

func (r *searchOutputReceipts) consumed(ctx context.Context, messages []chat.Message) []extensionbridge.ConsumedToolOutput {
	run, tracked := extensionbridge.SourceRunFromContext(ctx)
	if !tracked {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.byCall) == 0 {
		return nil
	}
	counts := make(map[string]int, len(r.byCall))
	for _, message := range messages {
		if message.Role == "tool" && message.TurnID == "" {
			counts[message.ToolCallID]++
		}
	}
	var outputs []extensionbridge.ConsumedToolOutput
	for _, message := range messages {
		if message.Role != "tool" || message.Name != agenttools.ToolSearchKnowledge || message.TurnID != "" || counts[message.ToolCallID] != 1 {
			continue
		}
		receipt := r.byCall[message.ToolCallID]
		if receipt.runID == run.RunID && receipt.renderedHash != "" && receipt.renderedHash == extensionbridge.SourceFieldHash(message.Content) {
			outputs = append(outputs, receipt.output)
			if len(outputs) == 128 {
				break
			}
		}
	}
	return outputs
}
