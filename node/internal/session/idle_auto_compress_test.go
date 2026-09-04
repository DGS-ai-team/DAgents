package session

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func TestIdleAutoCompressMarksAndSkipsRescan(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sessions.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	hub := stream.NewHub(8, logx.Discard())
	reg, err := tools.NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	pol := policy.NewDefaultEngine()
	llmClient := &idleCompressMockLLM{}
	mgr := NewManager("agent-1", hub, llmClient, reg, pol, st, TurnOptions{
		WorkspaceRoot:               dir,
		SkillsEnabled:               false,
		CompressionSilent:           0,
		CompressionBlocking:         0,
		IdleAutoCompressSeconds:     1,
		IdleAutoCompressPollSeconds: 1,
	}, logx.Discard())
	t.Cleanup(mgr.Stop)

	sess, _, err := mgr.Create("sess-idle-1")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	if rt == nil {
		t.Fatal("runtime missing")
	}
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
		t.Fatal("expected idle auto compress mark in DB after scan")
	}
	if mgr.getRuntime(sess.ID) != nil {
		t.Fatal("expected session evicted after maintenance scan")
	}
	before := llmClient.streamCalls.Load()
	mgr.scanIdleSessionMaintenance(context.Background())
	if llmClient.streamCalls.Load() != before {
		t.Fatalf("expected no second compress, stream calls %d -> %d", before, llmClient.streamCalls.Load())
	}
}

func TestIdleAutoCompressSkipsBelowMinTokens(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sessions.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	hub := stream.NewHub(8, logx.Discard())
	reg, err := tools.NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	pol := policy.NewDefaultEngine()
	llmClient := &idleCompressMockLLM{}
	mgr := NewManager("agent-1", hub, llmClient, reg, pol, st, TurnOptions{
		WorkspaceRoot:               dir,
		SkillsEnabled:               false,
		CompressionSilent:           0,
		CompressionBlocking:         0,
		IdleAutoCompressSeconds:     1,
		IdleAutoCompressMinTokens:   1_000_000,
		IdleAutoCompressPollSeconds: 1,
	}, logx.Discard())
	t.Cleanup(mgr.Stop)

	sess, _, err := mgr.Create("sess-idle-3")
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
	if err != nil || rec == nil {
		t.Fatal(err)
	}
	if rec.RuntimeState.IdleAutoCompressApplied {
		t.Fatal("expected no compress mark when below min tokens")
	}
	if mgr.getRuntime(sess.ID) != nil {
		t.Fatal("expected session evicted even without compress")
	}
	if llmClient.streamCalls.Load() != 0 {
		t.Fatalf("expected no LLM compress call, got %d", llmClient.streamCalls.Load())
	}
}

func TestIdleAutoCompressClearsMarkOnUserMessage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sessions.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	hub := stream.NewHub(8, logx.Discard())
	reg, err := tools.NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	pol := policy.NewDefaultEngine()
	llmClient := &idleCompressMockLLM{humanReply: "ok"}
	mgr := NewManager("agent-1", hub, llmClient, reg, pol, st, TurnOptions{
		WorkspaceRoot:           dir,
		SkillsEnabled:           false,
		CompressionSilent:       0,
		CompressionBlocking:     0,
		IdleAutoCompressSeconds: 1,
	}, logx.Discard())
	t.Cleanup(mgr.Stop)

	sess, _, err := mgr.Create("sess-idle-2")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	rt.mu.Lock()
	rt.messages = compressSampleMessages()
	rt.idleAutoCompressApplied = true
	rt.mu.Unlock()
	rt.persist(context.Background())

	if _, err := mgr.EnqueueMessage(context.Background(), sess.ID, "message", "hello", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rt.mu.Lock()
		marked := rt.idleAutoCompressApplied
		rt.mu.Unlock()
		if !marked {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	rt.mu.Lock()
	stillMarked := rt.idleAutoCompressApplied
	rt.mu.Unlock()
	if stillMarked {
		t.Fatal("expected idle auto compress mark cleared after user message")
	}
}

func compressSampleMessages() []llm.Message {
	long := strings.Repeat("word ", 120)
	return []llm.Message{
		{Role: "user", Content: long},
		{Role: "assistant", Content: "收到"},
		{Role: "user", Content: "继续"},
		{Role: "assistant", Content: "好的"},
	}
}

type idleCompressMockLLM struct {
	streamCalls atomic.Int32
	humanReply  string
}

func (m *idleCompressMockLLM) StreamChat(_ context.Context, req llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	m.streamCalls.Add(1)
	last := req.Messages[len(req.Messages)-1]
	if last.Role == "user" && last.Name != llm.UserNameCompression {
		reply := m.humanReply
		if reply == "" {
			reply = "收到"
		}
		if handler.OnDelta != nil {
			handler.OnDelta(reply)
		}
		return llm.ChatResult{Content: reply}, nil
	}
	content := "[示例母任务]\n[示例子任务]\n任务目标：t；阶段性总结论：c；修改过的文件和资源：无"
	if handler.OnDelta != nil {
		handler.OnDelta(content)
	}
	if handler.OnUsage != nil {
		handler.OnUsage(llm.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120})
	}
	return llm.ChatResult{Content: content}, nil
}

func (m *idleCompressMockLLM) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

func (m *idleCompressMockLLM) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}
