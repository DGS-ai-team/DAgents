package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

func TestDreamingWaitingApprovalSurvivesRuntimeRestart(t *testing.T) {
	client := &dreamingResumeClient{}
	mgr, reg, st, dbPath, sessionID, _ := dreamingResumeFixture(t, client, policy.ModeAlways)
	leaseCtx, release, ok, err := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1")
	if err != nil || !ok {
		t.Fatalf("lease: ok=%v err=%v", ok, err)
	}
	if _, err := mgr.RunDreaming(leaseCtx, sessionID, "重启后继续整理", 5); err == nil {
		t.Fatal("dreaming unexpectedly completed")
	}
	release()
	if attempt, found, _ := mgr.GetDreamingAttempt(sessionID); !found || attempt.State != DreamingAttemptWaiting {
		t.Fatalf("waiting attempt=%+v found=%v", attempt, found)
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
	allow := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{
		"read_file": policy.ModeNever, "write_file": policy.ModeAlways, "search_replace": policy.ModeNever,
		"glob_files": policy.ModeNever, "grep_file": policy.ModeNever, "grep_files": policy.ModeNever,
	}})
	reopened := NewManager("agent-1", stream.NewHub(32, logx.Discard()), client, reg, allow, reopenedStore, TurnOptions{AutoAgent: true}, logx.Discard())
	defer reopened.Stop()
	if _, _, err := reopened.CreateWithOptionsAndLLM(sessionID, TurnOptions{AutoAgent: true, WorkspaceRoot: reg.WorkspaceRoot()}, reg, allow, client, "agent-1"); err != nil {
		t.Fatal(err)
	}
	if attempt, found, err := reopened.GetDreamingAttempt(sessionID); err != nil || !found || attempt.State != DreamingAttemptWaiting {
		t.Fatalf("reopened attempt=%+v found=%v err=%v", attempt, found, err)
	}
	resume := func(id string, value map[string]any) {
		t.Helper()
		if _, err := reopened.EnqueueMessage(context.Background(), sessionID, "resume", "", nil, value, ""); err != nil {
			t.Fatal(err)
		}
	}
	resume("ask-1", map[string]any{"type": "user_information", "tool_call_id": "ask-1", "answer": "确认第一步"})
	waitDreamingPending(t, reopened, sessionID, "ask_user_information", "ask-2")
	resume("ask-2", map[string]any{"type": "user_information", "tool_call_id": "ask-2", "answer": "确认第二步"})
	waitDreamingPending(t, reopened, sessionID, "write_file", "write-1")
	resume("write-1", map[string]any{"type": "selection", "approved": []string{"write-1"}, "rejected": []string{}})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if attempt, found, _ := reopened.GetDreamingAttempt(sessionID); found && attempt.State == DreamingAttemptCompleted {
			if attempt.FinalMessage == "" {
				t.Fatal("completed attempt has no final message")
			}
			if got, err := os.ReadFile(filepath.Join(reg.HandbookRoot(), "resumed", "lesson.md")); err != nil || string(got) != "approved lesson" {
				t.Fatalf("reopened handbook=%q err=%v", got, err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	attempt, found, _ := reopened.GetDreamingAttempt(sessionID)
	t.Fatalf("reopened dreaming did not complete: found=%v attempt=%+v", found, attempt)
}

func TestDreamingOrphanedAttemptIsFailedOnRestore(t *testing.T) {
	client := &dreamingResumeClient{}
	mgr, reg, st, dbPath, sessionID, _ := dreamingResumeFixture(t, client, policy.ModeAlways)
	rt := mgr.getRuntime(sessionID)
	rt.dreamingAttempt = &DreamingAttempt{AgentID: "agent-1", SessionID: sessionID, TurnID: "missing-turn", MaxToolRounds: 2, State: DreamingAttemptWaiting, Boundary: "orphan", StartedAt: time.Now().UTC()}
	if err := rt.persist(context.Background()); err != nil {
		t.Fatal(err)
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
	reopened := NewManager("agent-1", stream.NewHub(16, logx.Discard()), client, reg, policy.NewDefaultEngine(), reopenedStore, TurnOptions{AutoAgent: true}, logx.Discard())
	defer reopened.Stop()
	if _, _, err := reopened.CreateWithOptionsAndLLM(sessionID, TurnOptions{AutoAgent: true, WorkspaceRoot: reg.WorkspaceRoot()}, reg, nil, client, "agent-1"); err != nil {
		t.Fatal(err)
	}
	attempt, found, err := reopened.GetDreamingAttempt(sessionID)
	if err != nil || !found || attempt.State != DreamingAttemptFailed {
		t.Fatalf("orphan attempt=%+v found=%v err=%v", attempt, found, err)
	}
}
