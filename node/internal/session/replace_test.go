package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func TestReplaceWithOptionsFailureKeepsPreviousRuntime(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	reg, err := tools.NewRegistry(filepath.Join(dir, "runtime"), 30)
	if err != nil {
		t.Fatal(err)
	}
	pol := policy.NewDefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewManager("agent-1", stream.NewHub(16, logx.Discard()), &llm.MockClient{}, reg, pol, st, TurnOptions{SkillsEnabled: false}, logx.Discard())
	defer mgr.Stop()

	sess, _, err := mgr.Create("replace-me")
	if err != nil {
		t.Fatal(err)
	}
	old := mgr.getRuntime(sess.ID)
	if old == nil {
		t.Fatal("expected existing runtime")
	}
	old.persist(context.Background())
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	if _, _, err := mgr.ReplaceWithOptions(sess.ID, TurnOptions{SkillsEnabled: false}, reg, pol); err == nil {
		t.Fatal("expected replacement load failure")
	}
	if got := mgr.getRuntime(sess.ID); got != old {
		t.Fatalf("previous runtime was not retained: got=%p want=%p", got, old)
	}
}

func TestReplaceWithOptionsSwapsRuntimeAndKeepsInMemoryState(t *testing.T) {
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	pol := policy.NewDefaultEngine()
	mgr := NewManager("agent-1", stream.NewHub(16, logx.Discard()), &llm.MockClient{}, reg, pol, nil, TurnOptions{SkillsEnabled: false}, logx.Discard())
	defer mgr.Stop()

	sess, _, err := mgr.Create("replace-memory")
	if err != nil {
		t.Fatal(err)
	}
	old := mgr.getRuntime(sess.ID)
	old.mu.Lock()
	old.messages = []llm.Message{{Role: "user", Content: "preserve me"}}
	old.mu.Unlock()
	newRoot := t.TempDir()
	if _, _, err := mgr.ReplaceWithOptions(sess.ID, TurnOptions{WorkspaceRoot: newRoot, SkillsEnabled: false}, reg, pol); err != nil {
		t.Fatal(err)
	}
	current := mgr.getRuntime(sess.ID)
	if current == nil || current == old {
		t.Fatalf("runtime was not swapped: current=%p old=%p", current, old)
	}
	if got, ok := mgr.SessionWorkspaceRoot(sess.ID); !ok || got != newRoot {
		t.Fatalf("new runtime fs root = %q, ok=%v", got, ok)
	}
	if got := current.messageCount(); got != 1 {
		t.Fatalf("replacement lost in-memory messages: count=%d", got)
	}
}

func TestReplaceWithOptionsKeepsRecoveredTerminalInteractionClock(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	reg, err := tools.NewRegistry(filepath.Join(dir, "runtime"), 30)
	if err != nil {
		t.Fatal(err)
	}
	pol := policy.NewDefaultEngine()
	mgr := NewManager("agent-1", stream.NewHub(16, logx.Discard()), &llm.MockClient{}, reg, pol, st, TurnOptions{SkillsEnabled: false}, logx.Discard())
	defer mgr.Stop()
	sess, _, err := mgr.Create("replace-clock")
	if err != nil {
		t.Fatal(err)
	}
	old := mgr.getRuntime(sess.ID)
	if err := old.lifecycleBeginHumanTurn(); err != nil {
		t.Fatal(err)
	}
	if err := old.lifecycleAfterModelStep(turn.StepOutcome{}, []llm.Message{{Role: "assistant", Content: "finished"}}, 0); err != nil {
		t.Fatal(err)
	}
	terminal := old.turnCoordinator.Snapshot()
	if terminal.TurnFinishedAt == nil || terminal.Source != turn.TurnSourceHuman {
		t.Fatalf("expected persisted terminal human fact, got %+v", terminal)
	}
	// Model the crash gap: the lifecycle terminal fact is durable, but the
	// runtime checkpoint still has the previous interaction clock.
	old.mu.Lock()
	old.lastHumanInteraction = terminal.TurnFinishedAt.Add(-time.Hour)
	old.mu.Unlock()
	if err := old.persist(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := mgr.ReplaceWithOptions(sess.ID, TurnOptions{SkillsEnabled: false}, reg, pol); err != nil {
		t.Fatal(err)
	}
	replacement := mgr.getRuntime(sess.ID)
	if replacement == nil || !replacement.lastHumanInteraction.Equal(*terminal.TurnFinishedAt) {
		t.Fatalf("replacement lost recovered terminal clock: got=%v want=%v", replacement.lastHumanInteraction, terminal.TurnFinishedAt)
	}
}
