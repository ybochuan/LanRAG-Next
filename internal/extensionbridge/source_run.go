package extensionbridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
)

// SourceScope is an independently encoded proof scope, not a native ACL.
type SourceScope struct {
	TenantID        uint64 `json:"tenant_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
}

// SourceRunIdentity is built only after native authorization.
type SourceRunIdentity struct {
	Scope                 SourceScope `json:"scope"`
	UserID                string      `json:"user_id"`
	SessionID             string      `json:"session_id"`
	AttemptID             string      `json:"attempt_id"`
	AuthorizationKind     string      `json:"authorization_kind,omitempty"`
	AuthorizedDocumentIDs []string    `json:"authorized_document_ids"`
}

// SourceRunHandle is server-only and must never be added to tool results or logs.
type SourceRunHandle struct {
	Scope      SourceScope `json:"scope"`
	RunID      string      `json:"run_id"`
	Capability string      `json:"capability"`
}

// SourceRef identifies the immutable native version actually consumed.
type SourceRef struct {
	DocumentID string `json:"document_id"`
	VersionID  string `json:"version_id"`
}

// SourceInput fingerprints an actual native field using its JSON representation.
type SourceInput struct {
	DocumentID string `json:"document_id"`
	ChunkID    string `json:"chunk_id,omitempty"`
	Field      string `json:"field"`
	ValueHash  string `json:"value_hash"`
}

type sourceStep struct {
	OperationID string `json:"operation_id"`
	Kind        string `json:"kind"`
}
type sourceInputRequest struct {
	OperationID string      `json:"operation_id"`
	StepID      string      `json:"step_id"`
	Evidence    SourceInput `json:"evidence"`
}
type sourceStepResult struct {
	OperationID string `json:"operation_id"`
	StepID      string `json:"step_id"`
	Outcome     string `json:"outcome"`
	InputHash   string `json:"input_hash"`
	OutputHash  string `json:"output_hash"`
}
type sourceFinish struct {
	OperationID      string `json:"operation_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	Abort            bool   `json:"abort"`
}
type sourceRunSnapshot struct {
	Identity  SourceRunIdentity `json:"identity"`
	RunID     string            `json:"run_id"`
	Revision  int64             `json:"revision"`
	State     string            `json:"state"`
	Sources   []SourceRef       `json:"sources"`
	CreatedAt time.Time         `json:"created_at"`
	ExpiresAt time.Time         `json:"expires_at"`
}

type sourceRequest struct {
	RequestID             string              `json:"request_id"`
	Scope                 SourceScope         `json:"scope"`
	OperationID           string              `json:"operation_id,omitempty"`
	Action                string              `json:"action,omitempty"`
	Identity              *SourceRunIdentity  `json:"identity,omitempty"`
	Handle                *SourceRunHandle    `json:"handle,omitempty"`
	Step                  *sourceStep         `json:"step,omitempty"`
	Input                 *sourceInputRequest `json:"input,omitempty"`
	Result                *sourceStepResult   `json:"result,omitempty"`
	Finish                *sourceFinish       `json:"finish,omitempty"`
	Producer              *SourceProducer     `json:"producer,omitempty"`
	Produced              *SourceProduced     `json:"produced,omitempty"`
	Save                  *sourceSave         `json:"save,omitempty"`
	Read                  *SourceCopyRead     `json:"read,omitempty"`
	Binding               *SourceCopyBinding  `json:"binding,omitempty"`
	CopyID                string              `json:"copy_id,omitempty"`
	AuthorizedDocumentIDs []string            `json:"authorized_document_ids,omitempty"`
}

var sourceHTTPClient = &http.Client{Timeout: 500 * time.Millisecond, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// SourceHash hashes exact bytes; SourceFieldHash additionally preserves JSON field encoding.
func SourceHash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// SourceFieldHash matches the native component-view field encoding.
func SourceFieldHash(value any) string {
	body, e := json.Marshal(value)
	if e != nil {
		return ""
	}
	return SourceHash(body)
}

func sourceDigest(value string) bool {
	b, e := hex.DecodeString(value)
	return e == nil && len(b) == 32 && value == strings.ToLower(value)
}
func sourceID(value string) bool {
	return value != "" && len(value) <= 128 && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\x00\r\n\t ")
}

func sourceCall(ctx context.Context, path string, q sourceRequest, out any) error {
	key := os.Getenv("AGENTRAG_MANAGEMENT_KEY")
	if len(key) < 32 || q.Scope.TenantID == 0 || !sourceID(q.Scope.KnowledgeBaseID) {
		return ErrUnattributed
	}
	q.RequestID = uuid.NewString()
	body, e := json.Marshal(q)
	if e != nil || len(body) > 128<<10 {
		return ErrUnattributed
	}
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:8090"+path, bytes.NewReader(body))
	if e != nil {
		return ErrUnattributed
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("X-AgentRAG-Bridge-Version", "1")
	req.Header.Set("Content-Type", "application/json")
	response, e := sourceHTTPClient.Do(req)
	if e != nil {
		return ErrUnattributed
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return ErrUnattributed
	}
	data, e := io.ReadAll(io.LimitReader(response.Body, (256<<10)+1))
	if e != nil || len(data) > 256<<10 {
		return ErrUnattributed
	}
	var env struct {
		Success   bool            `json:"success"`
		RequestID string          `json:"request_id"`
		Scope     SourceScope     `json:"scope"`
		Data      json.RawMessage `json:"data"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if dec.Decode(&env) != nil || dec.Decode(new(any)) != io.EOF || !env.Success || env.RequestID != q.RequestID || env.Scope != q.Scope || len(env.Data) == 0 || bytes.Equal(env.Data, []byte("null")) {
		return ErrUnattributed
	}
	dec = json.NewDecoder(bytes.NewReader(env.Data))
	dec.DisallowUnknownFields()
	if dec.Decode(out) != nil || dec.Decode(new(any)) != io.EOF {
		return ErrUnattributed
	}
	return nil
}

type sourceRunKey struct{}
type sourceStepKey struct{}
type sourceUnknownKey struct{}

// SourceRunFromContext cannot read a handle from user/model arguments.
func SourceRunFromContext(ctx context.Context) (SourceRunHandle, bool) {
	h, ok := ctx.Value(sourceRunKey{}).(SourceRunHandle)
	return h, ok
}

// BeginSourceRun binds an explicitly native-authorized scope. Missing or
// ambiguous scopes remain unattributed; empty IDs never imply a whole KB.
func BeginSourceRun(ctx context.Context, i SourceRunIdentity) (context.Context, error) {
	if i.AuthorizationKind == "" {
		i.AuthorizationKind = "documents"
	}
	if i.Scope.TenantID == 0 || !sourceID(i.Scope.KnowledgeBaseID) || !sourceID(i.UserID) || !sourceID(i.SessionID) || !sourceID(i.AttemptID) || len(i.AuthorizedDocumentIDs) > 256 || i.AuthorizationKind != "documents" && i.AuthorizationKind != "knowledge_base" || i.AuthorizationKind == "documents" && len(i.AuthorizedDocumentIDs) == 0 || i.AuthorizationKind == "knowledge_base" && len(i.AuthorizedDocumentIDs) != 0 {
		return ctx, ErrUnattributed
	}
	i.AuthorizedDocumentIDs = append([]string(nil), i.AuthorizedDocumentIDs...)
	sort.Strings(i.AuthorizedDocumentIDs)
	for n, id := range i.AuthorizedDocumentIDs {
		if !sourceID(id) || n > 0 && id == i.AuthorizedDocumentIDs[n-1] {
			return ctx, ErrUnattributed
		}
	}
	var h SourceRunHandle
	if e := sourceCall(ctx, "/provenance/runs/begin", sourceRequest{Scope: i.Scope, Identity: &i, OperationID: "run:" + SourceFieldHash(i)}, &h); e != nil {
		return ctx, e
	}
	if h.Scope != i.Scope || !sourceID(h.RunID) || !sourceDigest(h.Capability) {
		return ctx, ErrUnattributed
	}
	return context.WithValue(ctx, sourceRunKey{}, h), nil
}

// BeginSourceRunForTargets only converts the already authorized native target
// DTO. Cross-KB or unresolved tag scopes cannot seal in this first adapter.
func BeginSourceRunForTargets(ctx context.Context, userID, sessionID, attemptID string, targets types.SearchTargets) (context.Context, error) {
	kbs := targets.GetAllKnowledgeBaseIDs()
	if len(kbs) != 1 {
		return ctx, ErrUnattributed
	}
	tenant := targets.GetTenantIDForKB(kbs[0])
	if tenant == 0 {
		tenant, _ = types.TenantIDFromContext(ctx)
	}
	i := SourceRunIdentity{Scope: SourceScope{tenant, kbs[0]}, UserID: userID, SessionID: sessionID, AttemptID: attemptID, AuthorizationKind: "documents"}
	seen := map[string]bool{}
	for _, target := range targets {
		if target == nil {
			continue
		}
		if target.KnowledgeBaseID != kbs[0] || target.TenantID != 0 && target.TenantID != tenant {
			return ctx, ErrUnattributed
		}
		if target.Type == types.SearchTargetTypeKnowledgeBase && len(target.KnowledgeIDs) == 0 && len(target.TagIDs) == 0 && len(target.ScopeTagIDs) == 0 {
			i.AuthorizationKind = "knowledge_base"
			continue
		}
		if target.Type != types.SearchTargetTypeKnowledge || len(target.KnowledgeIDs) == 0 {
			return ctx, ErrUnattributed
		}
		for _, doc := range target.KnowledgeIDs {
			if !seen[doc] {
				seen[doc] = true
				i.AuthorizedDocumentIDs = append(i.AuthorizedDocumentIDs, doc)
			}
		}
	}
	if i.AuthorizationKind == "knowledge_base" {
		i.AuthorizedDocumentIDs = nil
	}
	return BeginSourceRun(ctx, i)
}

// BeginSourceStep attaches a server-issued step to a child context for parallel calls.
func BeginSourceStep(ctx context.Context, kind string) (context.Context, error) {
	h, ok := SourceRunFromContext(ctx)
	if !ok {
		return ctx, ErrUnattributed
	}
	var result struct {
		StepID string `json:"step_id"`
	}
	if e := sourceCall(ctx, "/provenance/runs/steps", sourceRequest{Scope: h.Scope, Handle: &h, Action: "begin", Step: &sourceStep{uuid.NewString(), kind}}, &result); e != nil {
		return ctx, e
	}
	if !sourceID(result.StepID) {
		return ctx, ErrUnattributed
	}
	return context.WithValue(ctx, sourceStepKey{}, result.StepID), nil
}

// RecordSourceInput sends only an actual field fingerprint, never raw document text.
func RecordSourceInput(ctx context.Context, e SourceInput) error {
	h, ok := SourceRunFromContext(ctx)
	step, _ := ctx.Value(sourceStepKey{}).(string)
	if !ok || !sourceID(step) {
		return ErrUnattributed
	}
	var result struct{}
	return sourceCall(ctx, "/provenance/runs/inputs", sourceRequest{Scope: h.Scope, Handle: &h, Input: &sourceInputRequest{uuid.NewString(), step, e}}, &result)
}

// FinishSourceStep records actual encoded request/output hashes. Unknown is
// monotonic in Host; a transport failure must never replay the underlying call.
func FinishSourceStep(ctx context.Context, input, output []byte, outcome string) error {
	return finishSourceStepHashes(ctx, SourceHash(input), SourceHash(output), outcome, uuid.NewString())
}

func finishSourceStepHashes(ctx context.Context, input, output, outcome, operation string) error {
	h, ok := SourceRunFromContext(ctx)
	step, _ := ctx.Value(sourceStepKey{}).(string)
	if !ok || !sourceID(step) {
		return ErrUnattributed
	}
	var result struct{}
	return sourceCall(ctx, "/provenance/runs/steps", sourceRequest{Scope: h.Scope, Handle: &h, Action: "finish", Result: &sourceStepResult{operation, step, outcome, input, output}}, &result)
}

// SourceCall holds only request-local transport digests. Host owns all run facts.
// Record and Finish are called by the single consumer of this model/tool call.
type SourceCall struct {
	ctx       context.Context
	inputHash string
	output    hash.Hash
	operation string
	err       error
}

// BeginSourceCall proves consumed fields before execution. Unsupported input
// composition is acknowledged as unknown before it reaches the model, keeping
// native QA available while preventing this run from publishing readable copies.
// A missing run never creates provenance or grants a copy permission.
func BeginSourceCall(ctx context.Context, kind string, request any, inputs []SourceInput, complete bool) (context.Context, *SourceCall, error) {
	h, tracked := SourceRunFromContext(ctx)
	if !tracked {
		return ctx, &SourceCall{}, nil
	}
	body, err := json.Marshal(request)
	if err != nil {
		return ctx, nil, ErrUnattributed
	}
	stepCtx, err := BeginSourceStep(ctx, kind)
	if err != nil {
		// Once Host has permanently refused attribution, subsequent native QA
		// calls may continue. Never treat an unknown transport result as that fact.
		var snapshot sourceRunSnapshot
		if sourceCall(ctx, "/provenance/runs/finish", sourceRequest{Scope: h.Scope, Handle: &h, Action: "inspect"}, &snapshot) == nil && snapshot.RunID == h.RunID && snapshot.Identity.Scope == h.Scope && snapshot.State == "unattributed" {
			return context.WithValue(ctx, sourceUnknownKey{}, true), &SourceCall{}, nil
		}
		return ctx, nil, ErrUnattributed
	}
	call := &SourceCall{ctx: stepCtx, inputHash: SourceHash(body), output: sha256.New(), operation: uuid.NewString()}
	if !complete {
		if err := call.Finish("unknown"); err != nil {
			return ctx, nil, err
		}
		return context.WithValue(ctx, sourceUnknownKey{}, true), &SourceCall{}, nil
	}
	for _, input := range inputs {
		if err := RecordSourceInput(stepCtx, input); err != nil {
			return ctx, nil, err
		}
	}
	return stepCtx, call, nil
}

// CaptureSourceInputs attaches fields loaded inside an already registered tool
// step. Only an acknowledged Host refusal permits skipping further attribution.
func CaptureSourceInputs(ctx context.Context, inputs []SourceInput, complete bool) error {
	if _, tracked := SourceRunFromContext(ctx); !tracked || ctx.Value(sourceUnknownKey{}) == true {
		return nil
	}
	step, _ := ctx.Value(sourceStepKey{}).(string)
	if !sourceID(step) {
		return ErrUnattributed
	}
	if !complete {
		return FinishSourceStep(ctx, nil, nil, "unknown")
	}
	for _, input := range inputs {
		if err := RecordSourceInput(ctx, input); err != nil {
			return err
		}
	}
	return nil
}

// Record hashes each actual response/chunk before native decoding changes it.
// Newline framing preserves chunk boundaries without storing generated content.
func (c *SourceCall) Record(response any) {
	if c == nil || c.output == nil || c.err != nil {
		return
	}
	body, err := json.Marshal(response)
	if err != nil {
		c.err = ErrUnattributed
		return
	}
	_, _ = c.output.Write(body)
	_, _ = c.output.Write([]byte{'\n'})
}

// Finish acknowledges a terminal call without retrying the model or its tools.
// Cancellation/error cannot be recorded as successful completion.
func (c *SourceCall) Finish(outcome string) error {
	if c == nil || c.output == nil {
		return nil
	}
	if c.err != nil || c.ctx.Err() != nil {
		outcome = "error"
	}
	return finishSourceStepHashes(context.WithoutCancel(c.ctx), c.inputHash, hex.EncodeToString(c.output.Sum(nil)), outcome, c.operation)
}

// FinishSourceRun is called at the actual execution terminal, not HTTP return.
func FinishSourceRun(ctx context.Context, abort bool) error {
	h, ok := SourceRunFromContext(ctx)
	if !ok {
		return ErrUnattributed
	}
	var snapshot sourceRunSnapshot
	if e := sourceCall(ctx, "/provenance/runs/finish", sourceRequest{Scope: h.Scope, Handle: &h, Action: "inspect"}, &snapshot); e != nil {
		return e
	}
	if snapshot.RunID != h.RunID || snapshot.Identity.Scope != h.Scope || snapshot.Revision < 1 || snapshot.State != "open" {
		return ErrUnattributed
	}
	expected := snapshot.Revision
	if e := sourceCall(ctx, "/provenance/runs/finish", sourceRequest{Scope: h.Scope, Handle: &h, Finish: &sourceFinish{uuid.NewString(), expected, abort}}, &snapshot); e != nil {
		return e
	}
	want := "sealed"
	if abort {
		want = "aborted"
	}
	if snapshot.RunID != h.RunID || snapshot.Identity.Scope != h.Scope || snapshot.Revision != expected+1 || snapshot.State != want {
		return ErrUnattributed
	}
	return nil
}
