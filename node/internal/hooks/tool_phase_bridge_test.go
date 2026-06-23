package hooks

import (
	"context"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

func registryToolBeforeEach(reg *Registry, in ToolBeforeEachInput) ToolBeforeEachResult {
	if reg == nil {
		return DefaultToolBeforeEachResult()
	}
	hc := BuildToolBeforeEachContext(in)
	out, err := reg.RunPhase(context.Background(), PhaseToolBeforeEach, hc, NoopHost())
	if err != nil {
		return DefaultToolBeforeEachResult()
	}
	return ToolBeforeEachDecisionFrom(out)
}

func registryToolAfterEach(reg *Registry, in ToolAfterEachInput) ToolAfterEachOutput {
	if reg == nil {
		return defaultToolAfterEachOutput(in.RawResult)
	}
	hc := BuildToolAfterEachContext(in)
	out, err := reg.RunPhase(context.Background(), PhaseToolAfterEach, hc, NoopHost())
	if err != nil {
		return defaultToolAfterEachOutput(in.RawResult)
	}
	return ToolAfterEachOutputFrom(out)
}

func TestRunToolBeforeEachViaRunPhase_policyChain(t *testing.T) {
	dir := writeTestPolicyDir(t, "read_file=never\nwrite_file=always\n", "")
	engine, err := policy.LoadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(engine, RuntimeConfig{Duplicate: DefaultDuplicateConfig()})

	out := registryToolBeforeEach(reg, ToolBeforeEachInput{ToolName: "read_file"})
	if out.Action != policy.ActionAuto || out.ToolMode != policy.ModeNever {
		t.Fatalf("read_file = %+v", out)
	}

	out = registryToolBeforeEach(reg, ToolBeforeEachInput{ToolName: "write_file"})
	if out.Action != policy.ActionRequireApproval || out.ToolMode != policy.ModeAlways {
		t.Fatalf("write_file = %+v", out)
	}
}

func TestRunToolBeforeEachViaRunPhase_duplicate(t *testing.T) {
	dir := writeTestPolicyDir(t, "bash_run=rule\n", "echo=never\n")
	engine, err := policy.LoadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(engine, RuntimeConfig{Duplicate: DefaultDuplicateConfig()})
	log := &ToolExecutionLog{}
	reg.SetToolExecutionLog(log)

	in := ToolBeforeEachInput{
		ToolName:     "bash_run",
		ToolArgs:     map[string]any{"command": "echo ok"},
		RawArguments: `{"command":"echo ok"}`,
	}
	out := registryToolBeforeEach(reg, in)
	if out.Action != policy.ActionAuto {
		t.Fatalf("first call = %+v", out)
	}

	fp := ToolArgsFingerprint(in.ToolName, in.RawArguments)
	log.RecordSuccess(in.ToolName, fp, "call-1", "ok")

	out = registryToolBeforeEach(reg, in)
	if out.Action != policy.ActionRequireApproval {
		t.Fatalf("duplicate call = %+v", out)
	}
	if out.ApprovalSubtype != ApprovalSubtypeDuplicateToolCall || out.DuplicateMeta == nil {
		t.Fatalf("duplicate meta = %+v", out)
	}
}

func TestRunToolBeforeEachViaRunPhase_registersThreePhaseHooks(t *testing.T) {
	reg := NewRegistry(nil, RuntimeConfig{Duplicate: DefaultDuplicateConfig()})
	matched := reg.phaseHooksFor(PhaseToolBeforeEach)
	if len(matched) != 3 {
		t.Fatalf("before_each phaseHooks = %d, want 3", len(matched))
	}
	names := hookNames(matched)
	want := []string{"builtin.policy", "builtin.agent_owned_file", "builtin.duplicate_tool_call"}
	for i, name := range want {
		if names[i] != name {
			t.Fatalf("names = %v, want %v", names, want)
		}
	}
}

func TestRunToolAfterEachViaRunPhase_registersTwoPhaseHooks(t *testing.T) {
	reg := NewRegistry(nil, RuntimeConfig{Duplicate: DefaultDuplicateConfig()})
	matched := reg.phaseHooksFor(PhaseToolAfterEach)
	if len(matched) != 2 {
		t.Fatalf("after_each phaseHooks = %d, want 2", len(matched))
	}
	names := hookNames(matched)
	want := []string{"tool_result_package", "builtin.agent_owned_file_after"}
	for i, name := range want {
		if names[i] != name {
			t.Fatalf("names = %v, want %v", names, want)
		}
	}
}

func TestRunToolAfterEachViaRunPhase_passesThroughRawResult(t *testing.T) {
	reg := NewRegistry(nil, RuntimeConfig{
		Duplicate:  DefaultDuplicateConfig(),
		ToolResult: ToolResultConfig{Enabled: false},
	})
	in := ToolAfterEachInput{
		SessionID:  "s1",
		ToolCallID: "tc-1",
		ToolName:   "read_file",
		RawResult:  "file contents here",
	}
	out := registryToolAfterEach(reg, in)
	if out.ForClient != in.RawResult || out.ForHistory != in.RawResult {
		t.Fatalf("output = %+v", out)
	}
}

func TestRunToolBeforeEachViaRunPhase_contextCarriesDecision(t *testing.T) {
	dir := writeTestPolicyDir(t, "read_file=never\n", "")
	engine, err := policy.LoadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(engine, RuntimeConfig{Duplicate: DefaultDuplicateConfig()})
	hc := contextFromToolBeforeEachInput(ToolBeforeEachInput{ToolName: "read_file"})
	out, err := reg.RunPhase(context.Background(), PhaseToolBeforeEach, hc, NoopHost())
	if err != nil {
		t.Fatal(err)
	}
	if out.ToolDecision == nil {
		t.Fatal("expected ToolDecision on context")
	}
	if out.ToolDecision.Action != policy.ActionAuto {
		t.Fatalf("decision = %+v", out.ToolDecision)
	}
}

func TestRunToolAfterEachViaRunPhase_contextCarriesOutput(t *testing.T) {
	reg := NewRegistry(nil, RuntimeConfig{
		Duplicate:  DefaultDuplicateConfig(),
		ToolResult: ToolResultConfig{Enabled: false},
	})
	hc := contextFromToolAfterEachInput(ToolAfterEachInput{
		SessionID:  "s1",
		ToolCallID: "tc-1",
		ToolName:   "read_file",
		RawResult:  "payload",
	})
	out, err := reg.RunPhase(context.Background(), PhaseToolAfterEach, hc, NoopHost())
	if err != nil {
		t.Fatal(err)
	}
	if out.ToolAfterEachOutput == nil {
		t.Fatal("expected ToolAfterEachOutput on context")
	}
	if out.ToolAfterEachOutput.ForClient != "payload" {
		t.Fatalf("output = %+v", out.ToolAfterEachOutput)
	}
}

func hookNames(regs []registeredPhaseHook) []string {
	names := make([]string, 0, len(regs))
	for _, regHook := range regs {
		names = append(names, regHook.hook.Name())
	}
	return names
}

func TestRegisterPhaseHook_customToolBeforeEach(t *testing.T) {
	reg := NewRegistry(nil, RuntimeConfig{Duplicate: DefaultDuplicateConfig()})
	reg.RegisterPhaseHook(stubPhaseHook{
		name:   "custom.deny_all",
		phases: []Phase{PhaseToolBeforeEach},
		fn: func(_ context.Context, hc *Context, _ Host) (Result, error) {
			decision := ensureToolDecision(hc)
			decision.Action = policy.ActionDeny
			return Result{Action: ActionContinue}, nil
		},
	}, RegisterOpts{Priority: 100})

	out := registryToolBeforeEach(reg, ToolBeforeEachInput{ToolName: "read_file"})
	if out.Action != policy.ActionDeny {
		t.Fatalf("custom hook should run last and deny, got %+v", out)
	}
}

func TestPolicyToolHookRun_nativeHook(t *testing.T) {
	dir := writeTestPolicyDir(t, "read_file=never\n", "")
	engine, err := policy.LoadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	hook := NewPolicyToolHook(engine)
	hc := contextFromToolBeforeEachInput(ToolBeforeEachInput{ToolName: "read_file"})
	if _, err := hook.Run(context.Background(), hc, NoopHost()); err != nil {
		t.Fatal(err)
	}
	if hc.ToolDecision == nil || hc.ToolDecision.Action != policy.ActionAuto {
		t.Fatalf("decision = %+v", hc.ToolDecision)
	}
}

func TestToolResultPackageHookRun_disabled(t *testing.T) {
	hook := NewToolResultPackageHook(ToolResultConfig{Enabled: false})
	raw := strings.Repeat("x", 100)
	hc := contextFromToolAfterEachInput(ToolAfterEachInput{
		SessionID: "s1", ToolCallID: "tc-1", ToolName: "read_file", RawResult: raw,
	})
	if _, err := hook.Run(context.Background(), hc, NoopHost()); err != nil {
		t.Fatal(err)
	}
	if hc.ToolAfterEachOutput.ForHistory != raw {
		t.Fatalf("history = %q", hc.ToolAfterEachOutput.ForHistory)
	}
}
