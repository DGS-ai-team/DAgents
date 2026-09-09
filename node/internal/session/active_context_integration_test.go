package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

type dreamingContextClient struct {
	mu       sync.Mutex
	calls    int
	requests []llm.ChatRequest
	seen     chan struct{}
}

func (c *dreamingContextClient) StreamChat(_ context.Context, req llm.ChatRequest, _ llm.StreamHandler) (llm.ChatResult, error) {
	c.mu.Lock()
	c.calls++
	call := c.calls
	c.requests = append(c.requests, req)
	c.mu.Unlock()
	select {
	case c.seen <- struct{}{}:
	default:
	}
	if call%2 == 1 {
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: fmt.Sprintf("write-%d", call), Type: "function", Function: llm.ToolCallFunction{
			Name: "write_file", Arguments: fmt.Sprintf(`{"path":"handbook/round-%d.md","content":"round-%d","call_purpose":"maintain"}`, call, call),
		}}}, FinishReason: "tool_calls"}, nil
	}
	return llm.ChatResult{Content: fmt.Sprintf("experience-%d", call), FinishReason: "stop"}, nil
}
func (*dreamingContextClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*dreamingContextClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func TestDreamingActiveContextResetHydratesAndKeepsChatUsable(t *testing.T) {
	db := filepath.Join(t.TempDir(), "sessions.db")
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
		"read_file": policy.ModeNever, "write_file": policy.ModeNever,
		"search_replace": policy.ModeNever, "glob_files": policy.ModeNever,
		"grep_file": policy.ModeNever, "grep_files": policy.ModeNever,
	}})
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	client := &dreamingContextClient{seen: make(chan struct{}, 16)}
	mgr := NewManager("agent-1", stream.NewHub(16, logx.Discard()), client, reg, allow, st, TurnOptions{AutoAgent: true}, logx.Discard())
	rt, _, err := mgr.CreateWithOptionsAndLLM("dreaming-main", TurnOptions{AutoAgent: true}, reg, nil, client, "agent-1")
	if err != nil {
		st.Close()
		t.Fatal(err)
	}

	reset := func(commitID string) {
		ctx, release, ok, err := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1")
		if err != nil || !ok {
			t.Fatalf("lease: ok=%v err=%v", ok, err)
		}
		defer release()
		boundary, err := mgr.CaptureActiveContextBoundary(ctx, rt.ID)
		if err != nil || boundary == "" {
			t.Fatalf("capture boundary: %q err=%v", boundary, err)
		}
		changed, err := mgr.ResetActiveContext(ctx, rt.ID, boundary, commitID)
		if err != nil || !changed {
			t.Fatalf("reset: changed=%v err=%v", changed, err)
		}
		attempt, found, err := mgr.GetDreamingAttempt(rt.ID)
		if err != nil || !found {
			t.Fatalf("completed dreaming attempt missing before ack: found=%v err=%v", found, err)
		}
		if err := mgr.AckDreamingAttempt(ctx, rt.ID, attempt.TurnID); err != nil {
			t.Fatalf("ack dreaming attempt: %v", err)
		}
	}
	run := func(prompt string) {
		ctx, release, ok, err := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1")
		if err != nil || !ok {
			t.Fatalf("lease: ok=%v err=%v", ok, err)
		}
		defer release()
		result, err := mgr.RunDreaming(ctx, rt.ID, prompt, 2)
		if err != nil || result.Content == "" || !result.Changed {
			t.Fatalf("dreaming result=%+v err=%v", result, err)
		}
	}

	run("first experience")
	reset("dream-reset-1")
	run("second experience")
	reset("dream-reset-2")
	mgr.Stop()
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reg2, err := tools.NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg2.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"})); err != nil {
		t.Fatal(err)
	}
	if err := reg2.SetHandbookRoot(handbook); err != nil {
		t.Fatal(err)
	}
	st2, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	mgr2 := NewManager("agent-1", stream.NewHub(16, logx.Discard()), client, reg2, allow, st2, TurnOptions{AutoAgent: true}, logx.Discard())
	defer mgr2.Stop()
	if _, _, err := mgr2.CreateWithOptionsAndLLM(rt.ID, TurnOptions{AutoAgent: true}, reg2, nil, client, "agent-1"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := mgr2.GetDreamingAttempt(rt.ID); err != nil || found {
		t.Fatalf("acked dreaming attempt still present: found=%v err=%v", found, err)
	}
	ctx, release, ok, err := mgr2.TryAcquireMaintenanceContext(context.Background(), "agent-1")
	if err != nil || !ok {
		t.Fatalf("reopened lease: ok=%v err=%v", ok, err)
	}
	result, err := mgr2.RunDreaming(ctx, rt.ID, "third experience", 2)
	release()
	if err != nil || result.Content == "" || !result.Changed {
		t.Fatalf("reopened dreaming result=%+v err=%v", result, err)
	}

	client.mu.Lock()
	requests := append([]llm.ChatRequest(nil), client.requests...)
	client.mu.Unlock()
	if len(requests) < 6 {
		t.Fatalf("model requests=%d, want three tool rounds and three summaries", len(requests))
	}
	for i, req := range requests[:6] {
		if err := llm.ValidateToolProtocol(req.Messages); err != nil {
			t.Fatalf("request %d has invalid tool protocol: %v", i+1, err)
		}
		pending := map[string]bool{}
		for _, msg := range req.Messages {
			if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
				if err := llm.ValidateAssistantMessage(msg); err != nil {
					t.Fatalf("request %d has invalid assistant tool call: %v", i+1, err)
				}
				for _, call := range msg.ToolCalls {
					pending[call.ID] = true
				}
			}
			if msg.Role == "tool" {
				if msg.ToolCallID == "" || !pending[msg.ToolCallID] {
					t.Fatalf("request %d has unmatched tool result: %#v", i+1, msg)
				}
				delete(pending, msg.ToolCallID)
			}
		}
		if len(pending) != 0 {
			t.Fatalf("request %d has unresolved tool calls: %#v", i+1, pending)
		}
	}
	for i, req := range requests[:6] {
		if i > 0 {
			for _, msg := range req.Messages {
				if (strings.Contains(msg.Content, "first experience") || strings.Contains(msg.Content, "experience-2")) && i >= 2 {
					t.Fatalf("reopened active context leaked prior reset content at request %d: %#v", i+1, req.Messages)
				}
				if (strings.Contains(msg.Content, "second experience") || strings.Contains(msg.Content, "experience-4")) && i >= 4 {
					t.Fatalf("reopened active context leaked second reset content at request %d: %#v", i+1, req.Messages)
				}
			}
		}
	}
	if raw, err := os.ReadFile(filepath.Join(handbook, "round-5.md")); err != nil || string(raw) != "round-5" {
		t.Fatalf("reopened tool write=%q err=%v", raw, err)
	}
	record, err := st2.Load(context.Background(), rt.ID)
	if err != nil || record == nil {
		t.Fatalf("hydrated session: record=%v err=%v", record, err)
	}
	allText := ""
	for _, msg := range record.Messages {
		allText += "\n" + msg.Content
	}
	if strings.Count(allText, "first experience") != 1 || strings.Count(allText, "second experience") != 1 || strings.Count(allText, "third experience") != 1 ||
		strings.Count(allText, "experience-2") != 1 || strings.Count(allText, "experience-4") != 1 || strings.Count(allText, "experience-6") != 1 {
		t.Fatalf("hydrated history duplicated or lost summaries: %q", allText)
	}
	view, err := mgr2.GetContextView(rt.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range view.Messages {
		if strings.Contains(msg.Content, "first experience") || strings.Contains(msg.Content, "second experience") || strings.Contains(msg.Content, "experience-2") || strings.Contains(msg.Content, "experience-4") {
			t.Fatalf("context view exposed reset history: %#v", view.Messages)
		}
	}
	_, activeSummary, err := mgr2.ContextSummary(rt.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range activeSummary {
		if strings.Contains(msg.Content, "first experience") || strings.Contains(msg.Content, "second experience") || strings.Contains(msg.Content, "experience-2") || strings.Contains(msg.Content, "experience-4") {
			t.Fatalf("context summary exposed reset history: %#v", activeSummary)
		}
	}
	hydrate, err := mgr2.GetHydrateView(rt.ID)
	if err != nil {
		t.Fatal(err)
	}
	hydratedJSON, err := json.Marshal(hydrate.Transcript)
	if err != nil {
		t.Fatal(err)
	}
	hydratedText := string(hydratedJSON)
	for _, want := range []string{"first experience", "second experience", "third experience", "experience-2", "experience-4", "experience-6"} {
		if strings.Count(hydratedText, want) != 1 {
			t.Fatalf("hydrate transcript count for %q=%d: %s", want, strings.Count(hydratedText, want), hydratedText)
		}
	}

	if _, err := mgr2.ClearContext(rt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr2.EnqueueMessage(context.Background(), rt.ID, "message", "after clear", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		client.mu.Lock()
		calls := client.calls
		client.mu.Unlock()
		if calls >= 8 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("post-clear chat did not reach model")
		}
		time.Sleep(20 * time.Millisecond)
	}
	view, err = mgr2.GetContextView(rt.ID)
	if err != nil || view == nil {
		t.Fatalf("context view after clear: %+v err=%v", view, err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		count, _, err := mgr2.ContextSummary(rt.ID)
		if err != nil {
			t.Fatal(err)
		}
		if count > 0 {
			_, messages, err := mgr2.ContextSummary(rt.ID)
			if err != nil {
				t.Fatal(err)
			}
			joined := ""
			for _, msg := range messages {
				joined += "\n" + msg.Content
			}
			if strings.Contains(joined, "after clear") || strings.Contains(joined, "experience-8") {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("summary after clear: count=%d", count)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
