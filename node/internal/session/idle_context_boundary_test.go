package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/history"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func TestIdleConversationBoundaryPersistsThroughRestartReplacementAndClear(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	stateRoot := filepath.Join(workspace, ".dagents", "agent-1")
	if err := os.MkdirAll(stateRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(root, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	opts := TurnOptions{WorkspaceRoot: workspace, WorkspaceStateRoot: stateRoot, IdleNewConversationSeconds: 1, SkillsEnabled: false}
	mgr := NewManager("agent-1", stream.NewHub(16, logx.Discard()), nil, nil, nil, st, opts, logx.Discard())
	defer mgr.Stop()
	sess, _, err := mgr.Create("idle-boundary")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	rt.mu.Lock()
	rt.messages = []llm.Message{
		llm.UserMessage("请检查这个部署并给出结论。", llm.UserNameHuman),
		{Role: "assistant", Content: "服务已恢复，日志没有新的错误。"},
	}
	rt.historyRevision = 9
	rt.lastHumanInteraction = time.Now().Add(-2 * time.Second)
	rt.mu.Unlock()
	if err := rt.persist(ctx); err != nil {
		t.Fatal(err)
	}

	if err := rt.maybeStartIdleConversation(ctx, 1); err != nil {
		t.Fatal(err)
	}
	messages := rt.messagesSnapshot()
	if len(messages) != 3 || rt.activeContextStart != 2 {
		t.Fatalf("boundary history/index = %d/%d", len(messages), rt.activeContextStart)
	}
	bridge := messages[2]
	if !llm.IsMessageSource(bridge, llm.MessageSourceRuntime, llm.MessageFormNotice, "idle_context") || bridge.Name != llm.UserNameConversationBoundary {
		t.Fatalf("unexpected bridge source: %+v", bridge)
	}
	if bridge.Provenance == nil || bridge.Provenance.Reference == "" || !filepath.IsAbs(bridge.Provenance.Reference) {
		t.Fatalf("archive reference is not absolute: %+v", bridge.Provenance)
	}
	if !strings.Contains(bridge.Content, "仅在需要延续时查阅") || !strings.Contains(bridge.Content, "请检查这个部署") || !strings.Contains(bridge.Content, "服务已恢复") {
		t.Fatalf("bridge does not contain a bounded continuation notice: %q", bridge.Content)
	}
	if _, err := os.Stat(bridge.Provenance.Reference); err != nil {
		t.Fatalf("archive missing: %v", err)
	}
	archived, err := history.ArchiveIdleContext(stateRoot, sess.ID, rt.lastContextResetID, messages[:2])
	if err != nil || archived != bridge.Provenance.Reference {
		t.Fatalf("archive retry path=%q reference=%q err=%v", archived, bridge.Provenance.Reference, err)
	}
	rec, err := st.Load(ctx, sess.ID)
	if err != nil || rec == nil || rec.RuntimeState.ActiveContextStart != 2 || len(rec.Messages) != 3 {
		t.Fatalf("persisted boundary record=%+v err=%v", rec, err)
	}

	if _, _, err := mgr.ReplaceWithOptions(sess.ID, opts, nil, nil); err != nil {
		t.Fatalf("replace runtime: %v", err)
	}
	rt = mgr.getRuntime(sess.ID)
	if rt == nil || rt.activeContextStart != 2 || rt.lastHumanInteraction.IsZero() || len(rt.messagesSnapshot()) != 3 {
		t.Fatalf("replacement lost boundary or interaction baseline: runtime=%+v", rt)
	}

	if _, err := mgr.ClearContext(sess.ID); err != nil {
		t.Fatal(err)
	}
	rec, err = st.Load(ctx, sess.ID)
	if err != nil || rec == nil || rec.RuntimeState.LastHumanInteraction.IsZero() || rec.RuntimeState.ActiveContextStart != 0 {
		t.Fatalf("clear did not reset idle baseline/context: record=%+v err=%v", rec, err)
	}
}

func TestIdleConversationBoundaryPersistenceFailureRollsBackAndRetryIsIdempotent(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	stateRoot := filepath.Join(workspace, ".dagents", "agent-1")
	if err := os.MkdirAll(stateRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(root, "sessions.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	opts := TurnOptions{WorkspaceRoot: workspace, WorkspaceStateRoot: stateRoot, IdleNewConversationSeconds: 1, SkillsEnabled: false}
	mgr := NewManager("agent-1", stream.NewHub(16, logx.Discard()), nil, nil, nil, st, opts, logx.Discard())
	sess, _, err := mgr.Create("boundary-retry")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	rt.mu.Lock()
	rt.messages = []llm.Message{llm.UserMessage("last request", llm.UserNameHuman), {Role: "assistant", Content: "last answer"}}
	rt.historyRevision = 7
	rt.lastHumanInteraction = time.Now().Add(-2 * time.Second)
	rt.mu.Unlock()
	if err := rt.persist(ctx); err != nil {
		t.Fatal(err)
	}
	mgr.Stop()
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.maybeStartIdleConversation(ctx, 1); err == nil {
		t.Fatal("expected persistence failure")
	}
	if got := rt.messagesSnapshot(); len(got) != 2 || rt.activeContextStart != 0 || rt.lastContextResetID != "" || rt.historyRevision != 7 {
		t.Fatalf("persist failure left tentative boundary in memory: messages=%d start=%d id=%q revision=%d", len(got), rt.activeContextStart, rt.lastContextResetID, rt.historyRevision)
	}
	archiveEntries, err := filepath.Glob(filepath.Join(stateRoot, "history", "*", "*.json"))
	if err != nil || len(archiveEntries) != 1 {
		t.Fatalf("expected one orphaned but reusable archive, entries=%v err=%v", archiveEntries, err)
	}
	st, err = store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rt.store = st
	if err := rt.maybeStartIdleConversation(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if got := rt.messagesSnapshot(); len(got) != 3 || rt.activeContextStart != 2 || rt.lastContextResetID == "" {
		t.Fatalf("retry did not commit boundary: messages=%d start=%d id=%q", len(got), rt.activeContextStart, rt.lastContextResetID)
	}
	archiveEntries, err = filepath.Glob(filepath.Join(stateRoot, "history", "*", "*.json"))
	if err != nil || len(archiveEntries) != 1 {
		t.Fatalf("retry duplicated archive: entries=%v err=%v", archiveEntries, err)
	}
}

func TestIdleConversationBoundaryUsesInteractionClockNotUpdatedAt(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	stateRoot := filepath.Join(root, ".dagents", "agent-1")
	if err := os.MkdirAll(stateRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(root, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	mgr := NewManager("agent-1", stream.NewHub(8, logx.Discard()), nil, nil, nil, st,
		TurnOptions{IdleNewConversationSeconds: 60, WorkspaceStateRoot: stateRoot}, logx.Discard())
	defer mgr.Stop()
	sess, _, err := mgr.Create("interaction-clock")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	rt.mu.Lock()
	rt.messages = []llm.Message{llm.UserMessage("request", llm.UserNameHuman), {Role: "assistant", Content: "reply"}}
	rt.historyRevision = 3
	rt.lastHumanInteraction = time.Now().UTC()
	rt.mu.Unlock()
	if err := rt.persist(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.BackdateUpdatedAt(ctx, sess.ID, time.Now().Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := rt.maybeStartIdleConversation(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if got := rt.messagesSnapshot(); len(got) != 2 {
		t.Fatalf("stale database UpdatedAt incorrectly reset recent interaction: messages=%d", len(got))
	}
	rt.mu.Lock()
	rt.lastHumanInteraction = time.Now().Add(-2 * time.Minute)
	rt.mu.Unlock()
	if err := rt.persist(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.BackdateUpdatedAt(ctx, sess.ID, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := rt.maybeStartIdleConversation(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if got := rt.messagesSnapshot(); len(got) != 3 {
		t.Fatalf("future database UpdatedAt incorrectly blocked idle boundary: messages=%d", len(got))
	}
}

func TestIdleConversationBoundaryDoesNotBorrowReplyForUnansweredHuman(t *testing.T) {
	request, reply := lastMeaningfulHumanExchange([]llm.Message{
		llm.UserMessage("上一个问题", llm.UserNameHuman),
		{Role: "assistant", Content: "上一个答案"},
		llm.UserMessage("最新未完成请求", llm.UserNameHuman),
		llm.ToolResultMessage("call-1", "shell", "失败"),
	})
	if request != "最新未完成请求" || reply != "" {
		t.Fatalf("exchange borrowed earlier reply: request=%q reply=%q", request, reply)
	}
}

func TestFinishTurnIdleUsesDurableTerminalFinishedAtOnce(t *testing.T) {
	r := newLifecycleTestRuntime()
	if err := r.lifecycleBeginHumanTurn(); err != nil {
		t.Fatal(err)
	}
	if err := r.lifecycleAfterModelStep(turn.StepOutcome{}, []llm.Message{{Role: "assistant", Content: "done"}}, 0); err != nil {
		t.Fatal(err)
	}
	state := r.turnCoordinator.Snapshot()
	if state.TurnFinishedAt == nil || state.Source != turn.TurnSourceHuman || !state.TurnStatus.Terminal() {
		t.Fatalf("expected terminal human fact with completion time: %+v", state)
	}
	r.finishTurnIdle(turn.StepOutcome{})
	first := r.lastHumanInteraction
	if !first.Equal(*state.TurnFinishedAt) {
		t.Fatalf("clock=%s terminal finished_at=%s", first, state.TurnFinishedAt)
	}
	r.finishTurnIdle(turn.StepOutcome{})
	if !r.lastHumanInteraction.Equal(first) {
		t.Fatalf("duplicate finish refreshed completion time: first=%s second=%s", first, r.lastHumanInteraction)
	}
}

func TestRecoverLastHumanInteractionUsesTerminalFactAndClearFence(t *testing.T) {
	started := turn.TurnEventEnvelope{TurnID: "human-1", EventType: turn.EventTurnStarted, Source: string(turn.TurnSourceHuman), CreatedAt: time.Unix(100, 0).UTC()}
	completed := turn.TurnEventEnvelope{TurnID: "human-1", EventType: turn.EventTurnCompleted, CreatedAt: time.Unix(123, 0).UTC()}
	got := recoverLastHumanInteraction([]turn.TurnEventEnvelope{started, completed}, time.Time{})
	if !got.Equal(completed.CreatedAt) {
		t.Fatalf("recovered clock=%s want terminal time=%s", got, completed.CreatedAt)
	}
	clearFence := completed.CreatedAt.Add(time.Second)
	got = recoverLastHumanInteraction([]turn.TurnEventEnvelope{started, completed}, clearFence)
	if !got.Equal(clearFence) {
		t.Fatalf("old terminal fact crossed clear fence: got=%s fence=%s", got, clearFence)
	}
}

func TestIdleConversationBoundaryDoesNotResetPendingApprovalOrResume(t *testing.T) {
	root := t.TempDir()
	stateRoot := filepath.Join(root, ".dagents", "agent-1")
	if err := os.MkdirAll(stateRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(root, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	mgr := NewManager("agent-1", stream.NewHub(8, logx.Discard()), nil, nil, nil, st,
		TurnOptions{IdleNewConversationSeconds: 1, WorkspaceStateRoot: stateRoot}, logx.Discard())
	defer mgr.Stop()
	sess, _, err := mgr.Create("pending-idle")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	call := llm.ToolCall{ID: "approval-call", Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"command":"true"}`}}
	rt.mu.Lock()
	rt.messages = []llm.Message{llm.UserMessage("run command", llm.UserNameHuman), {Role: "assistant", ToolCalls: []llm.ToolCall{call}}}
	rt.historyRevision = 2
	rt.lastHumanInteraction = time.Now().Add(-time.Hour)
	rt.mu.Unlock()
	setTestPendingHITL(t, rt, &turn.PendingHITL{Items: []turn.PendingHITLItem{{ToolCall: call}}})
	if err := rt.maybeStartIdleConversation(context.Background(), rt.sessionEpoch); err != nil {
		t.Fatal(err)
	}
	if got := rt.messagesSnapshot(); len(got) != 2 || rt.activeContextStart != 0 {
		t.Fatalf("pending approval reset context: messages=%d start=%d", len(got), rt.activeContextStart)
	}
	if err := rt.lifecyclePrepareResume(nil); err != nil {
		t.Fatal(err)
	}
	if err := rt.maybeStartIdleConversation(context.Background(), rt.sessionEpoch); err != nil {
		t.Fatal(err)
	}
	if got := rt.messagesSnapshot(); len(got) != 2 || rt.activeContextStart != 0 {
		t.Fatalf("resumed continuation reset context: messages=%d start=%d", len(got), rt.activeContextStart)
	}
	if err := rt.lifecycleAfterResume(turn.StepOutcome{}, []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{call}},
		llm.ToolResultMessage(call.ID, call.Function.Name, "approved"),
	}); err != nil {
		t.Fatal(err)
	}
	rt.finishTurnIdle(turn.StepOutcome{})
	if rt.lastHumanInteraction.IsZero() || time.Since(rt.lastHumanInteraction) > time.Minute {
		t.Fatalf("terminal resumed human interaction did not refresh idle clock: %s", rt.lastHumanInteraction)
	}
}

func TestIdleConversationBoundaryArchiveFailureKeepsContext(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	stateFile := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(stateFile, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(root, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	mgr := NewManager("agent-1", stream.NewHub(8, logx.Discard()), nil, nil, nil, st,
		TurnOptions{IdleNewConversationSeconds: 1, WorkspaceStateRoot: stateFile}, logx.Discard())
	defer mgr.Stop()
	sess, _, err := mgr.Create("archive-failure")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	rt.mu.Lock()
	rt.messages = []llm.Message{llm.UserMessage("请求", llm.UserNameHuman), {Role: "assistant", Content: "答复"}}
	rt.historyRevision = 4
	rt.lastHumanInteraction = time.Now().Add(-2 * time.Second)
	rt.mu.Unlock()
	if err := rt.persist(ctx); err != nil {
		t.Fatal(err)
	}
	if err := rt.maybeStartIdleConversation(ctx, 1); err == nil {
		t.Fatal("expected archive failure")
	}
	if got := rt.messagesSnapshot(); len(got) != 2 || rt.activeContextStart != 0 || rt.lastContextResetID != "" || rt.historyRevision != 4 {
		t.Fatalf("archive failure changed context: messages=%+v start=%d id=%q revision=%d", got, rt.activeContextStart, rt.lastContextResetID, rt.historyRevision)
	}
}

func TestIdleConversationBoundarySkipsAutoAndActiveTurns(t *testing.T) {
	ctx := context.Background()
	for _, auto := range []bool{true, false} {
		root := t.TempDir()
		stateRoot := filepath.Join(root, ".dagents", "agent-1")
		if err := os.MkdirAll(stateRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		st, err := store.Open(filepath.Join(root, "sessions.db"))
		if err != nil {
			t.Fatal(err)
		}
		mgr := NewManager("agent-1", stream.NewHub(8, logx.Discard()), nil, nil, nil, st,
			TurnOptions{AutoAgent: auto, IdleNewConversationSeconds: 1, WorkspaceStateRoot: stateRoot}, logx.Discard())
		sess, _, err := mgr.Create("skip-boundary")
		if err != nil {
			t.Fatal(err)
		}
		rt := mgr.getRuntime(sess.ID)
		rt.mu.Lock()
		rt.messages = []llm.Message{llm.UserMessage("request", llm.UserNameHuman), {Role: "assistant", Content: "reply"}}
		rt.historyRevision = 2
		rt.lastHumanInteraction = time.Now().Add(-2 * time.Second)
		rt.mu.Unlock()
		if !auto {
			if err := rt.lifecycleBeginInputTurn(turn.TurnSourceHuman); err != nil {
				t.Fatal(err)
			}
		}
		if err := rt.maybeStartIdleConversation(ctx, 1); err != nil {
			t.Fatal(err)
		}
		if len(rt.messagesSnapshot()) != 2 || rt.activeContextStart != 0 {
			t.Fatalf("auto=%v active turn should not reset context", auto)
		}
		mgr.Stop()
		_ = st.Close()
	}
}
