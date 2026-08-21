package session

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func TestReplaceWithOptionsFailureKeepsPreviousRuntime(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := tools.NewRegistry(filepath.Join(dir, "runtime"), 30)
	if err != nil {
		t.Fatal(err)
	}
	pol, err := policy.LoadFile("")
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
	pol, err := policy.LoadFile("")
	if err != nil {
		t.Fatal(err)
	}
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
	if _, _, err := mgr.ReplaceWithOptions(sess.ID, TurnOptions{FSRoot: newRoot, SkillsEnabled: false}, reg, pol); err != nil {
		t.Fatal(err)
	}
	current := mgr.getRuntime(sess.ID)
	if current == nil || current == old {
		t.Fatalf("runtime was not swapped: current=%p old=%p", current, old)
	}
	if got, ok := mgr.SessionFSRoot(sess.ID); !ok || got != newRoot {
		t.Fatalf("new runtime fs root = %q, ok=%v", got, ok)
	}
	if got := current.messageCount(); got != 1 {
		t.Fatalf("replacement lost in-memory messages: count=%d", got)
	}
}
