package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

type gatedPolicyToolLLM struct {
	started chan struct{}
	release chan struct{}
	call    llm.ToolCall
}

func (c *gatedPolicyToolLLM) StreamChat(ctx context.Context, _ llm.ChatRequest, _ llm.StreamHandler) (llm.ChatResult, error) {
	close(c.started)
	select {
	case <-c.release:
	case <-ctx.Done():
		return llm.ChatResult{}, ctx.Err()
	}
	return llm.ChatResult{ToolCalls: []llm.ToolCall{c.call}, FinishReason: "tool_calls"}, nil
}

func (c *gatedPolicyToolLLM) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "summary", nil
}

func (c *gatedPolicyToolLLM) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func waitForHITL(t *testing.T, ch <-chan stream.Event) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case ev := <-ch:
			if ev.Type == "hitl_required" {
				return
			}
		case <-deadline.C:
			t.Fatal("timed out waiting for awaiting_hitl")
		}
	}
}

func TestManagerPolicyRevokeBeforeToolCallAndAfterRestart(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	workspace := t.TempDir()
	agentID := "agent-policy-integration"
	target := filepath.Join(workspace, "must-not-write.txt")
	callArgs, err := json.Marshal(map[string]any{"path": target, "content": "should not land"})
	if err != nil {
		t.Fatal(err)
	}
	call := llm.ToolCall{ID: "policy-call", Type: "function", Function: llm.ToolCallFunction{Name: "write_file", Arguments: string(callArgs)}}
	grant := policy.Grant{ID: "temporary", Tools: []string{"write_file"}, Workspace: workspace, ExpiresAt: time.Now().Add(time.Hour)}
	grantRecord := store.AgentPolicyRecord{AgentID: agentID, Tools: map[string]string{"write_file": "rule"}, Grants: []policy.Grant{grant}}
	revokedRecord := store.AgentPolicyRecord{AgentID: agentID, Tools: map[string]string{"write_file": "rule"}}

	agentStore, err := store.OpenAgents(filepath.Join(root, "agents.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := agentStore.SaveAgentPolicy(ctx, grantRecord); err != nil {
		t.Fatal(err)
	}
	initial, err := agentStore.LoadAgentPolicyEngine(ctx, agentID)
	if err != nil {
		t.Fatal(err)
	}
	if got := initial.DecideTool("write_file", map[string]any{"path": target}); got != policy.ActionAuto {
		t.Fatalf("initial grant decision = %s, want auto", got)
	}

	sessionStore, err := store.Open(filepath.Join(root, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := &gatedPolicyToolLLM{started: make(chan struct{}), release: make(chan struct{}), call: call}
	reg, err := tools.NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	hub := stream.NewHub(64, logx.Discard())
	mgr := NewManager(agentID, hub, client, reg, initial, sessionStore, TurnOptions{SkillsEnabled: false}, logx.Discard())
	defer mgr.Stop()
	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	events := hub.Subscribe(0)
	defer hub.Unsubscribe(events)
	if _, err := mgr.EnqueueMessage(ctx, sess.ID, "message", "write a file", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-client.started:
	case <-time.After(3 * time.Second):
		t.Fatal("model did not reach gate")
	}
	if err := agentStore.SaveAgentPolicy(ctx, revokedRecord); err != nil {
		t.Fatal(err)
	}
	revoked, err := agentStore.LoadAgentPolicyEngine(ctx, agentID)
	if err != nil {
		t.Fatal(err)
	}
	if got := revoked.DecideTool("write_file", map[string]any{"path": target}); got != policy.ActionRequireApproval {
		t.Fatalf("revoked decision = %s, want approval", got)
	}
	mgr.SetAgentPolicy(agentID, revoked)
	close(client.release)
	waitForHITL(t, events)
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("revoked queued write landed: %v", err)
	}
	mgr.Stop()
	if err := sessionStore.Close(); err != nil {
		t.Fatal(err)
	}
	if err := agentStore.Close(); err != nil {
		t.Fatal(err)
	}

	// Reload the policy SQLite store and construct a fresh production Manager
	// from the persisted revoked policy. A new model tool call must still pause.
	agentStore2, err := store.OpenAgents(filepath.Join(root, "agents.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer agentStore2.Close()
	reloaded, err := agentStore2.LoadAgentPolicyEngine(ctx, agentID)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.DecideTool("write_file", map[string]any{"path": target}); got != policy.ActionRequireApproval {
		t.Fatalf("reloaded decision = %s, want approval", got)
	}
	sessionStore2, err := store.Open(filepath.Join(root, "sessions-reopened.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sessionStore2.Close()
	client2 := &gatedPolicyToolLLM{started: make(chan struct{}), release: make(chan struct{}), call: call}
	reg2, err := tools.NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	hub2 := stream.NewHub(64, logx.Discard())
	mgr2 := NewManager(agentID, hub2, client2, reg2, reloaded, sessionStore2, TurnOptions{SkillsEnabled: false}, logx.Discard())
	defer mgr2.Stop()
	sess2, _, err := mgr2.Create("")
	if err != nil {
		t.Fatal(err)
	}
	events2 := hub2.Subscribe(0)
	defer hub2.Unsubscribe(events2)
	if _, err := mgr2.EnqueueMessage(ctx, sess2.ID, "message", "write again", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-client2.started:
	case <-time.After(3 * time.Second):
		t.Fatal("reopened model did not reach gate")
	}
	close(client2.release)
	waitForHITL(t, events2)
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("reopened revoked write landed: %v", err)
	}
}
