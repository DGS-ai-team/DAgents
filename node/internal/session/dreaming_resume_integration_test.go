package session

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

// dreamingResumeClient exercises two user-information resumes before the
// handbook write approval. The final response is only returned after the
// write approval has actually been resumed.
type dreamingResumeClient struct {
	calls atomic.Int32
}

func (c *dreamingResumeClient) StreamChat(_ context.Context, _ llm.ChatRequest, h llm.StreamHandler) (llm.ChatResult, error) {
	call := c.calls.Add(1)
	if h.OnUsage != nil {
		h.OnUsage(llm.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2})
	}
	switch call {
	case 1:
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "ask-1", Type: "function", Function: llm.ToolCallFunction{Name: "ask_user_information", Arguments: `{"question":"first"}`}}}, FinishReason: "tool_calls"}, nil
	case 2:
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "ask-2", Type: "function", Function: llm.ToolCallFunction{Name: "ask_user_information", Arguments: `{"question":"second"}`}}}, FinishReason: "tool_calls"}, nil
	case 3:
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "write-1", Type: "function", Function: llm.ToolCallFunction{Name: "write_file", Arguments: `{"path":"handbook/resumed/lesson.md","content":"approved lesson","call_purpose":"record"}`}}}, FinishReason: "tool_calls"}, nil
	default:
		return llm.ChatResult{Content: "经验已整理并写入手册", FinishReason: "stop"}, nil
	}
}

func (*dreamingResumeClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

func (*dreamingResumeClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func dreamingResumeFixture(t *testing.T, client llm.Client, writeMode policy.ApprovalMode) (*Manager, *tools.Registry, *store.SQLiteStore, string, string, func()) {
	t.Helper()
	workspace, handbook := t.TempDir(), t.TempDir()
	reg, err := tools.NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"})); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetHandbookRoot(handbook); err != nil {
		t.Fatal(err)
	}
	allow := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{
		"read_file": policy.ModeNever, "write_file": writeMode,
		"search_replace": policy.ModeNever, "glob_files": policy.ModeNever,
		"grep_file": policy.ModeNever, "grep_files": policy.ModeNever,
	}})
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewManager("agent-1", stream.NewHub(32, logx.Discard()), client, reg, allow, st, TurnOptions{AutoAgent: true}, logx.Discard())
	const sessionID = "dreaming-resume"
	if _, _, err := mgr.CreateWithOptionsAndLLM(sessionID, TurnOptions{AutoAgent: true, WorkspaceRoot: workspace}, reg, allow, client, "agent-1"); err != nil {
		st.Close()
		mgr.Stop()
		t.Fatal(err)
	}
	return mgr, reg, st, dbPath, sessionID, func() { mgr.Stop(); _ = st.Close() }
}

func waitDreamingPending(t *testing.T, mgr *Manager, sessionID, toolName, callID string) *turn.PendingHITL {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rt := mgr.getRuntime(sessionID)
		if rt != nil {
			pending := rt.pendingSnapshot()
			if pending != nil && len(pending.Items) == 1 && pending.Items[0].ToolCall.Function.Name == toolName && pending.Items[0].ToolCall.ID == callID {
				return pending
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for dreaming interaction " + toolName)
	return nil
}

func TestRunDreamingResumeApprovalPersistsCompletionAndHandbook(t *testing.T) {
	client := &dreamingResumeClient{}
	mgr, reg, st, dbPath, sessionID, cleanup := dreamingResumeFixture(t, client, policy.ModeAlways)
	defer cleanup()
	leaseCtx, release, ok, err := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1")
	if err != nil || !ok {
		t.Fatalf("maintenance lease: ok=%v err=%v", ok, err)
	}
	result, runErr := mgr.RunDreaming(leaseCtx, sessionID, "整理并确认经验", 5)
	release()
	if runErr == nil || result.Content != "" {
		t.Fatalf("first dreaming turn should wait: result=%+v err=%v", result, runErr)
	}
	if attempt, found, _ := mgr.GetDreamingAttempt(sessionID); !found || attempt.State != DreamingAttemptWaiting {
		t.Fatalf("first approval did not persist waiting attempt: found=%v attempt=%+v", found, attempt)
	}

	resume := func(callID, answer string) {
		t.Helper()
		_, err := mgr.EnqueueMessage(context.Background(), sessionID, "resume", "", nil, map[string]any{
			"type": "user_information", "tool_call_id": callID, "answer": answer,
		}, "")
		if err != nil {
			t.Fatal(err)
		}
	}
	resume("ask-1", "确认第一步")
	waitDreamingPending(t, mgr, sessionID, "ask_user_information", "ask-2")
	if attempt, found, _ := mgr.GetDreamingAttempt(sessionID); !found || attempt.State != DreamingAttemptWaiting {
		t.Fatalf("second ask did not remain waiting: found=%v attempt=%+v", found, attempt)
	}
	resume("ask-2", "确认第二步")
	waitDreamingPending(t, mgr, sessionID, "write_file", "write-1")
	if _, err := mgr.EnqueueMessage(context.Background(), sessionID, "resume", "", nil, map[string]any{
		"type": "selection", "approved": []string{"write-1"}, "rejected": []string{},
	}, ""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if attempt, found, _ := mgr.GetDreamingAttempt(sessionID); found && attempt.State == DreamingAttemptCompleted {
			if attempt.FinalMessage != "经验已整理并写入手册" {
				t.Fatalf("unexpected completed experience: %+v", attempt)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	attempt, found, err := mgr.GetDreamingAttempt(sessionID)
	if err != nil || !found || attempt.State != DreamingAttemptCompleted {
		t.Fatalf("dreaming did not complete after approval: found=%v err=%v attempt=%+v pending=%+v calls=%d", found, err, attempt, mgr.getRuntime(sessionID).pendingSnapshot(), client.calls.Load())
	}
	if got, err := os.ReadFile(filepath.Join(reg.HandbookRoot(), "resumed", "lesson.md")); err != nil || string(got) != "approved lesson" {
		t.Fatalf("approved handbook write missing: %q err=%v", got, err)
	}
	mgr.Stop()
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reopenedStore, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopenedStore.Close()
	reopened := NewManager("agent-1", stream.NewHub(32, logx.Discard()), &dreamingResumeClient{}, reg, policy.NewDefaultEngine(), reopenedStore, TurnOptions{AutoAgent: true}, logx.Discard())
	defer reopened.Stop()
	if _, _, err := reopened.CreateWithOptionsAndLLM(sessionID, TurnOptions{AutoAgent: true, WorkspaceRoot: reg.WorkspaceRoot()}, reg, nil, &dreamingResumeClient{}, "agent-1"); err != nil {
		t.Fatal(err)
	}
	restored, found, err := reopened.GetDreamingAttempt(sessionID)
	if err != nil || !found || restored.State != DreamingAttemptCompleted {
		t.Fatalf("completed dreaming was not restored: found=%v err=%v attempt=%+v", found, err, restored)
	}
}

func TestRunDreamingResumeRejectsNonHandbookToolAndDoesNotComplete(t *testing.T) {
	client := &dreamingRejectClient{}
	mgr, reg, _, _, sessionID, cleanup := dreamingResumeFixture(t, client, policy.ModeNever)
	defer cleanup()
	leaseCtx, release, ok, err := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1")
	if err != nil || !ok {
		t.Fatalf("maintenance lease: ok=%v err=%v", ok, err)
	}
	result, runErr := mgr.RunDreaming(leaseCtx, sessionID, "尝试运行非手册工具", 1)
	release()
	if runErr == nil || result.Content != "" {
		t.Fatalf("non-handbook tool unexpectedly completed: result=%+v err=%v", result, runErr)
	}
	if attempt, found, _ := mgr.GetDreamingAttempt(sessionID); found && attempt.State == DreamingAttemptCompleted {
		t.Fatalf("rejected dreaming was marked completed: %+v", attempt)
	}
	if _, err := os.Stat(filepath.Join(reg.HandbookRoot(), "escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("rejected dreaming wrote handbook: %v", err)
	}
}

type dreamingWriteClient struct{ calls atomic.Int32 }

func (c *dreamingWriteClient) StreamChat(_ context.Context, _ llm.ChatRequest, h llm.StreamHandler) (llm.ChatResult, error) {
	if h.OnUsage != nil {
		h.OnUsage(llm.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2})
	}
	if c.calls.Add(1) == 1 {
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "write-reject", Type: "function", Function: llm.ToolCallFunction{Name: "write_file", Arguments: `{"path":"handbook/rejected.md","content":"must not persist","call_purpose":"record"}`}}}, FinishReason: "tool_calls"}, nil
	}
	return llm.ChatResult{Content: "should not be accepted", FinishReason: "stop"}, nil
}
func (*dreamingWriteClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*dreamingWriteClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func TestRunDreamingResumeRejectsHandbookWriteWithoutCompleting(t *testing.T) {
	client := &dreamingWriteClient{}
	mgr, reg, _, _, sessionID, cleanup := dreamingResumeFixture(t, client, policy.ModeAlways)
	defer cleanup()
	leaseCtx, release, ok, err := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1")
	if err != nil || !ok {
		t.Fatalf("maintenance lease: ok=%v err=%v", ok, err)
	}
	result, runErr := mgr.RunDreaming(leaseCtx, sessionID, "需要审批的手册写入", 2)
	release()
	if runErr == nil || result.Content != "" {
		t.Fatalf("write approval unexpectedly completed: result=%+v err=%v", result, runErr)
	}
	waitDreamingPending(t, mgr, sessionID, "write_file", "write-reject")
	if _, err := mgr.EnqueueMessage(context.Background(), sessionID, "resume", "", nil, map[string]any{
		"type": "selection", "approved": []string{}, "rejected": []string{"write-reject"},
	}, ""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var attempt DreamingAttempt
	var found bool
	for time.Now().Before(deadline) {
		attempt, found, err = mgr.GetDreamingAttempt(sessionID)
		if err != nil {
			t.Fatal(err)
		}
		pending := mgr.getRuntime(sessionID).pendingSnapshot()
		if found && pending == nil && (attempt.State == DreamingAttemptFailed || attempt.State == DreamingAttemptCancelled) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !found || attempt.State != DreamingAttemptFailed && attempt.State != DreamingAttemptCancelled || mgr.getRuntime(sessionID).pendingSnapshot() != nil {
		t.Fatalf("rejected write did not reach a terminal failed state: found=%v attempt=%+v pending=%+v", found, attempt, mgr.getRuntime(sessionID).pendingSnapshot())
	}
	if _, err := os.Stat(filepath.Join(reg.HandbookRoot(), "rejected.md")); !os.IsNotExist(err) {
		t.Fatalf("rejected write changed handbook: %v", err)
	}
}

type dreamingRejectClient struct{}

func (*dreamingRejectClient) StreamChat(context.Context, llm.ChatRequest, llm.StreamHandler) (llm.ChatResult, error) {
	return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "bash-1", Type: "function", Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"command":"echo escape"}`}}}, FinishReason: "tool_calls"}, nil
}
func (*dreamingRejectClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*dreamingRejectClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}
