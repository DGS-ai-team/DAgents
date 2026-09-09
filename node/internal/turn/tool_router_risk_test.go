package turn

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

type recordingRiskSubmitter struct {
	mu     sync.Mutex
	inputs []hooks.RiskObservationInput
	panic  bool
}

func (r *recordingRiskSubmitter) Submit(in hooks.RiskObservationInput) bool {
	if r.panic {
		panic("observer failure")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inputs = append(r.inputs, in)
	return true
}
func (r *recordingRiskSubmitter) last() hooks.RiskObservationInput {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.inputs[len(r.inputs)-1]
}

func configureRiskRouter(t *testing.T, root string, engine *policy.Engine) (*Orchestrator, *recordingRiskSubmitter) {
	t.Helper()
	orch := NewOrchestrator("agent-risk", root, stream.NewHub(16, logx.Discard()), &llm.MockClient{}, testRegistryAt(t, root), engine, SkillAccess{}, nil, nil, hooks.RuntimeConfig{Duplicate: hooks.DuplicateConfig{Enabled: boolPtr(false)}, ToolResult: hooks.DefaultToolResultConfig(root)}, logx.Discard())
	orch.SetLifecycleMetadataProvider(func(string) map[string]any { return map[string]any{"turn_id": "turn-1", "step_id": "step-1"} })
	r := &recordingRiskSubmitter{}
	orch.SetRiskSubmitter(r)
	return orch, r
}

func TestToolRouterSubmitsFinalAutoDenyAndAskWithoutChangingExecution(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "read.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	// auto: the real router executes the read and records the final action.
	orch, rec := configureRiskRouter(t, root, policy.NewDefaultEngine())
	history := []llm.Message{}
	if _, _, err := orch.processToolCalls(context.Background(), "session-1", &history, []llm.ToolCall{{ID: "auto-1", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"read.txt"}`}}}); err != nil {
		t.Fatal(err)
	}
	if got := rec.last(); got.PolicyAction != string(policy.ActionAuto) || got.RequestID != "session-1:turn-1:step-1:auto-1" {
		t.Fatalf("auto observation=%+v", got)
	}
	// deny: no file is created, while the observer still sees deny.
	orch, rec = configureRiskRouter(t, root, policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"write_file": policy.ModeDeny}}))
	var pending *PendingHITL
	var state string
	var err error
	pending, state, err = orch.processToolCalls(context.Background(), "session-2", &history, []llm.ToolCall{{ID: "deny-1", Type: "function", Function: llm.ToolCallFunction{Name: "write_file", Arguments: `{"path":"denied.txt","content":"no"}`}}})
	if err != nil || pending != nil {
		t.Fatalf("deny result pending=%+v err=%v", pending, err)
	}
	if got := rec.last(); got.PolicyAction != string(policy.ActionDeny) {
		t.Fatalf("deny observation=%+v", got)
	}
	if _, err := os.Stat(filepath.Join(root, "denied.txt")); !os.IsNotExist(err) {
		t.Fatal("denied tool executed")
	}
	// ask: approval remains pending and is not auto executed.
	orch, rec = configureRiskRouter(t, root, policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"write_file": policy.ModeAlways}}))
	pending, state, err = orch.processToolCalls(context.Background(), "session-3", &history, []llm.ToolCall{{ID: "ask-1", Type: "function", Function: llm.ToolCallFunction{Name: "write_file", Arguments: `{"path":"ask.txt","content":"wait"}`}}})
	if err != nil || state != "awaiting_hitl" || pending == nil {
		t.Fatalf("ask pending=%+v state=%s err=%v", pending, state, err)
	}
	if got := rec.last(); got.PolicyAction != string(policy.ActionRequireApproval) {
		t.Fatalf("ask observation=%+v", got)
	}
}

type denyPreflightExecutor struct {
	tools.Executor
	executions atomic.Int32
}

func (e *denyPreflightExecutor) Execute(ctx context.Context, name, args string) (string, error) {
	e.executions.Add(1)
	return e.Executor.Execute(ctx, name, args)
}
func (e *denyPreflightExecutor) PreflightTool(context.Context, string, map[string]any) (tools.ToolPreflightDecision, bool) {
	return tools.ToolPreflightDecision{Action: policy.ActionDeny, ApprovalReason: "live deny"}, true
}

func TestToolRouterRiskSeesFinalLivePreflightDeny(t *testing.T) {
	root := t.TempDir()
	orch, rec := configureRiskRouter(t, root, policy.NewDefaultEngine())
	wrapped := &denyPreflightExecutor{Executor: orch.tools}
	orch.tools = wrapped
	history := []llm.Message{}
	pending, _, err := orch.processToolCalls(context.Background(), "session-live", &history, []llm.ToolCall{{ID: "live-1", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"missing"}`}}})
	if err != nil || pending != nil {
		t.Fatalf("live deny pending=%+v err=%v", pending, err)
	}
	if wrapped.executions.Load() != 0 {
		t.Fatal("live denied tool executed")
	}
	if got := rec.last(); got.PolicyAction != string(policy.ActionDeny) {
		t.Fatalf("final preflight observation=%+v", got)
	}
}
