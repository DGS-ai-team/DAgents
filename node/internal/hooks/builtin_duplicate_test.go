package hooks

import (
	"context"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

func TestDuplicateToolCallHookRuleAutoHit(t *testing.T) {
	log := &ToolExecutionLog{}
	now := time.Unix(1_700_000_000, 0)
	raw := `{"path":"a.txt","call_purpose":"read"}`
	fp := ToolArgsFingerprint("read_file", raw)
	log.RecordSuccess("read_file", fp, "call-prev", "ok")
	log.last.ExecutedAt = now.Add(-12 * time.Second)

	hook := NewDuplicateToolCallHook(DefaultDuplicateConfig())
	hook.SetLog(log)
	hook.SetNow(func() time.Time { return now })

	var out ToolBeforeEachResult
	out.ToolMode = policy.ModeRule
	out.Action = policy.ActionAuto
	_ = hook.RunToolBeforeEach(context.Background(), ToolBeforeEachInput{
		ToolName:     "read_file",
		RawArguments: raw,
	}, &out)
	if out.ApprovalSubtype != ApprovalSubtypeDuplicateToolCall {
		t.Fatalf("subtype = %q", out.ApprovalSubtype)
	}
	if out.Action != policy.ActionRequireApproval {
		t.Fatalf("action = %q", out.Action)
	}
	if out.DuplicateMeta == nil || out.DuplicateMeta.SecondsSincePrevious != 12 {
		t.Fatalf("meta = %+v", out.DuplicateMeta)
	}
}

func TestDuplicateToolCallHookSkipsNever(t *testing.T) {
	log := &ToolExecutionLog{}
	log.RecordSuccess("read_file", "fp", "call-prev", "ok")
	hook := NewDuplicateToolCallHook(DefaultDuplicateConfig())
	hook.SetLog(log)

	var out ToolBeforeEachResult
	out.ToolMode = policy.ModeNever
	out.Action = policy.ActionAuto
	_ = hook.RunToolBeforeEach(context.Background(), ToolBeforeEachInput{
		ToolName:     "read_file",
		RawArguments: `{"path":"a.txt"}`,
	}, &out)
	if out.Action != policy.ActionAuto || out.ApprovalSubtype != "" {
		t.Fatalf("never should skip duplicate: %+v", out)
	}
}

func TestDuplicateToolCallHookSkipsOutsideWindow(t *testing.T) {
	log := &ToolExecutionLog{}
	now := time.Unix(1_700_000_000, 0)
	fp := ToolArgsFingerprint("read_file", `{"path":"a.txt"}`)
	log.RecordSuccess("read_file", fp, "call-prev", "ok")
	log.last.ExecutedAt = now.Add(-90 * time.Second)

	hook := NewDuplicateToolCallHook(DefaultDuplicateConfig())
	hook.SetLog(log)
	hook.SetNow(func() time.Time { return now })

	var out ToolBeforeEachResult
	out.ToolMode = policy.ModeRule
	out.Action = policy.ActionAuto
	_ = hook.RunToolBeforeEach(context.Background(), ToolBeforeEachInput{
		ToolName:     "read_file",
		RawArguments: `{"path":"a.txt"}`,
	}, &out)
	if out.ApprovalSubtype != "" {
		t.Fatalf("outside window should not hit duplicate: %+v", out)
	}
}

func TestDuplicateToolCallHookIgnoresCallPurpose(t *testing.T) {
	log := &ToolExecutionLog{}
	now := time.Now()
	fp := ToolArgsFingerprint("read_file", `{"path":"a.txt","call_purpose":"first"}`)
	log.RecordSuccess("read_file", fp, "call-prev", "ok")

	hook := NewDuplicateToolCallHook(DefaultDuplicateConfig())
	hook.SetLog(log)
	hook.SetNow(func() time.Time { return now })

	var out ToolBeforeEachResult
	out.ToolMode = policy.ModeRule
	out.Action = policy.ActionAuto
	_ = hook.RunToolBeforeEach(context.Background(), ToolBeforeEachInput{
		ToolName:     "read_file",
		RawArguments: `{"path":"a.txt","call_purpose":"second"}`,
	}, &out)
	if out.ApprovalSubtype != ApprovalSubtypeDuplicateToolCall {
		t.Fatalf("same args different purpose should hit duplicate: %+v", out)
	}
}
