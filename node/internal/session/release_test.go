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

func TestReleaseEvictsFromMemoryKeepsDB(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	mgr := newIdleMaintenanceTestManager(t, dir, st, 1, 0)
	sess, _, err := mgr.Create("sess-release-1")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	rt.mu.Lock()
	rt.messages = compressSampleMessages()
	rt.mu.Unlock()
	rt.persist(context.Background())

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

func TestIdleSessionMaintenanceEvictsAfterCompress(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	llmClient := &idleCompressMockLLM{}
	mgr := newIdleMaintenanceTestManager(t, dir, st, 1, 0, llmClient)
	sess, _, err := mgr.Create("sess-evict-1")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	rt.mu.Lock()
	rt.messages = compressSampleMessages()
	rt.mu.Unlock()
	rt.persist(context.Background())
	if err := st.BackdateUpdatedAt(context.Background(), sess.ID, time.Now().Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	mgr.scanIdleSessionMaintenance(context.Background())
	rec, err := st.Load(context.Background(), sess.ID)
	if err != nil || rec == nil || !rec.RuntimeState.IdleAutoCompressApplied {
		t.Fatalf("expected compress mark in DB, rec = %#v err = %v", rec, err)
	}
	if mgr.getRuntime(sess.ID) != nil {
		t.Fatal("expected session evicted")
	}
	if llmClient.streamCalls.Load() != 1 {
		t.Fatalf("compress calls = %d", llmClient.streamCalls.Load())
	}
}

func TestIdleSessionMaintenanceEvictsWithoutCompressBelowMinTokens(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	llmClient := &idleCompressMockLLM{}
	mgr := NewManager("agent-1", stream.NewHub(8, logx.Discard()), llmClient, mustRegistry(t, dir), mustPolicy(t), st, TurnOptions{
		FSRoot:                      dir,
		SkillsEnabled:               false,
		IdleAutoCompressSeconds:     1,
		IdleAutoCompressMinTokens:   1_000_000,
		IdleAutoCompressPollSeconds: 1,
	}, logx.Discard())
	sess, _, err := mgr.Create("sess-evict-2")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	rt.mu.Lock()
	rt.messages = compressSampleMessages()
	rt.mu.Unlock()
	rt.persist(context.Background())
	if err := st.BackdateUpdatedAt(context.Background(), sess.ID, time.Now().Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	mgr.scanIdleSessionMaintenance(context.Background())
	if mgr.getRuntime(sess.ID) != nil {
		t.Fatal("expected evict without compress")
	}
	if llmClient.streamCalls.Load() != 0 {
		t.Fatalf("expected no compress, got %d", llmClient.streamCalls.Load())
	}
}

func TestIdleSessionMaintenanceEvictsPendingHITL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	llmClient := &idleCompressMockLLM{}
	mgr := newIdleMaintenanceTestManager(t, dir, st, 1, 0, llmClient)
	sess, _, err := mgr.Create("sess-evict-hitl")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	rt.mu.Lock()
	rt.messages = compressSampleMessages()
	rt.mu.Unlock()
	setTestPendingHITL(t, rt, &turn.PendingHITL{Items: []turn.PendingHITLItem{{ToolCall: llm.ToolCall{ID: "call-1"}}}})
	rt.persist(context.Background())
	if err := st.BackdateUpdatedAt(context.Background(), sess.ID, time.Now().Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	mgr.scanIdleSessionMaintenance(context.Background())
	rec, err := st.Load(context.Background(), sess.ID)
	if err != nil || rec == nil || rec.RuntimeState.Pending == nil {
		t.Fatalf("pending should remain in DB: %#v err=%v", rec, err)
	}
	if mgr.getRuntime(sess.ID) != nil {
		t.Fatal("expected pending HITL session evicted")
	}
	if llmClient.streamCalls.Load() != 0 {
		t.Fatalf("expected no compress with pending HITL, got %d", llmClient.streamCalls.Load())
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

	mgr := newIdleMaintenanceTestManager(t, dir, st, 1, 0)
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

func newIdleMaintenanceTestManager(t *testing.T, dir string, st *store.SQLiteStore, idleSec, minTokens int, llmClient ...llm.Client) *Manager {
	t.Helper()
	var client llm.Client = &idleCompressMockLLM{}
	if len(llmClient) > 0 && llmClient[0] != nil {
		client = llmClient[0]
	}
	return NewManager("agent-1", stream.NewHub(8, logx.Discard()), client, mustRegistry(t, dir), mustPolicy(t), st, TurnOptions{
		FSRoot:                      dir,
		SkillsEnabled:               false,
		CompressionSilent:           0,
		CompressionBlocking:         0,
		IdleAutoCompressSeconds:     idleSec,
		IdleAutoCompressMinTokens:   minTokens,
		IdleAutoCompressPollSeconds: 1,
	}, logx.Discard())
}

func mustRegistry(t *testing.T, dir string) *tools.Registry {
	t.Helper()
	reg, err := tools.NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func mustPolicy(t *testing.T) *policy.Engine {
	t.Helper()
	pol, err := policy.LoadFile("")
	if err != nil {
		t.Fatal(err)
	}
	return pol
}
