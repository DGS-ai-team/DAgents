package session

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

func TestReleaseEvictsFromMemoryKeepsDB(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	mgr := NewManager("agent-1", stream.NewHub(8, logx.Discard()), nil, nil, nil, st,
		TurnOptions{WorkspaceRoot: dir, SkillsEnabled: false}, logx.Discard())
	sess, _, err := mgr.Create("sess-release-1")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	rt.mu.Lock()
	rt.messages = []llm.Message{{Role: "user", Content: "persisted history"}}
	rt.mu.Unlock()
	if err := rt.persist(context.Background()); err != nil {
		t.Fatal(err)
	}
	released, err := mgr.Release(sess.ID)
	if err != nil || !released {
		t.Fatalf("Release = %v err = %v", released, err)
	}
	if mgr.getRuntime(sess.ID) != nil {
		t.Fatal("expected runtime removed")
	}
	rec, err := st.Load(context.Background(), sess.ID)
	if err != nil || rec == nil || len(rec.Messages) == 0 {
		t.Fatalf("db record = %#v err = %v", rec, err)
	}
}

func TestCreateRestoresEvictedSession(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	mgr := NewManager("agent-1", stream.NewHub(8, logx.Discard()), nil, nil, nil, st,
		TurnOptions{WorkspaceRoot: dir, SkillsEnabled: false}, logx.Discard())
	sess, _, err := mgr.Create("sess-restore")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Release(sess.ID); err != nil {
		t.Fatal(err)
	}
	restored, created, err := mgr.Create(sess.ID)
	if err != nil || created {
		t.Fatalf("Create restore err=%v created=%v", err, created)
	}
	if restored == nil || mgr.getRuntime(sess.ID) == nil {
		t.Fatal("expected session back in memory")
	}
}
