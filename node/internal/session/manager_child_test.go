package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/childagent"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// newManagerWithChildAgents 创建启用子 Agent 的 session Manager 与 Hub。
func newManagerWithChildAgents(t *testing.T, llmClient llm.Client) (*Manager, *childagent.Manager, *stream.Hub) {
	t.Helper()
	hub := stream.NewHub(64, logx.Discard())
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	pol := policy.NewDefaultEngine()
	mgr := NewManager("agent-1", hub, llmClient, reg, pol, nil, TurnOptions{SkillsEnabled: false, CompressionBlocking: 0}, logx.Discard())
	cm := childagent.NewManager(childagent.Config{Enabled: true}, hub, "agent-1", nil)
	mgr.SetChildAgentManager(cm)
	return mgr, cm, hub
}

// TestChildAgentParentTurnSynchronous 父 turn 通过同步 create 工具完成子任务并收到 SSE。
func TestChildAgentParentTurnSynchronous(t *testing.T) {
	mock := &llm.ChildAgentFlowMock{FinalReply: "委派完成"}
	mgr, _, hub := newManagerWithChildAgents(t, mock)
	defer mgr.Stop()

	parent, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}

	ch := hub.Subscribe(0)
	defer hub.Unsubscribe(ch)

	ctx := context.Background()
	if _, err := mgr.EnqueueMessage(ctx, parent.ID, "message", "请委派子任务检查 README", nil, nil, ""); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(8 * time.Second)
	var gotCreated, gotCompleted, gotDone bool
	var approved bool
	var childID string
	for !(gotCreated && gotCompleted && gotDone) {
		select {
		case ev := <-ch:
			t.Logf("sse type=%s data=%v", ev.Type, ev.Data)
			switch ev.Type {
			case "hitl_required":
				if !approved {
					approved = true
					if _, err := mgr.EnqueueMessage(ctx, parent.ID, "resume", "", nil, map[string]any{
						"type": "selection", "approved": []any{"call-create-child-1"}, "rejected": []any{},
					}, ""); err != nil {
						t.Fatal(err)
					}
				}
			case "temporary_agent_created":
				gotCreated = true
				childID, _ = ev.Data["child_agent_id"].(string)
			case "temporary_agent_completed":
				gotCompleted = true
				if s, ok := ev.Data["summary"].(string); ok && !strings.Contains(s, "README") {
					t.Fatalf("unexpected summary: %q", s)
				}
			case "turn_finished":
				gotDone = true
			}
		case <-deadline:
			t.Fatalf("timeout created=%v completed=%v done=%v child=%q", gotCreated, gotCompleted, gotDone, childID)
		}
	}
	if childID == "" {
		t.Fatal("empty child_agent_id")
	}
}

// TestChildAgentCreateIsSynchronous 创建工具返回前必须已经拿到子 Agent 终态。
func TestChildAgentCreateIsSynchronous(t *testing.T) {
	mock := &llm.MockClient{}
	mgr, cm, _ := newManagerWithChildAgents(t, mock)
	defer mgr.Stop()

	parent, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	createOut, err := cm.HandleCreate(ctx, parent.ID, `{"task":"列出目录","purpose":"sync test"}`)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(createOut), &result); err != nil {
		t.Fatal(err)
	}
	childID, _ := result["child_agent_id"].(string)
	if childID == "" {
		t.Fatalf("missing child id: %s", createOut)
	}

	if result["kind"] != "result" || result["status"] != string(childagent.StatusCompleted) {
		t.Fatalf("unexpected synchronous result: %s", createOut)
	}
}

func TestHydrateIncludesActiveChildProgressAndParentToolCall(t *testing.T) {
	mock := &delayedEchoMock{delay: 2 * time.Second}
	mgr, cm, _ := newManagerWithChildAgents(t, mock)
	defer mgr.Stop()

	parent, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	created := make(chan error, 1)
	go func() {
		_, err := cm.HandleParentTool(context.Background(), parent.ID, childagent.ToolCreateTemporaryAgent,
			`{"task":"slow job","purpose":"hydrate test"}`, "call-parent-child-1")
		created <- err
	}()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		view, _ := mgr.GetHydrateView(parent.ID)
		if len(view.ChildAgents) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	view, err := mgr.GetHydrateView(parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.ChildAgents) != 1 {
		t.Fatalf("hydrate child agents = %+v", view.ChildAgents)
	}
	child := view.ChildAgents[0]
	if child.ToolCallID != "call-parent-child-1" || child.Purpose != "hydrate test" {
		t.Fatalf("hydrate child = %+v", child)
	}
	if child.Progress.Status != childagent.StatusActive || child.Progress.MaxTurns <= 0 {
		t.Fatalf("hydrate progress = %+v", child.Progress)
	}
	if err := <-created; err != nil {
		t.Fatal(err)
	}
}

type delayedEchoMock struct {
	delay time.Duration
}

// StreamChat 延迟后 echo，用于 cancel 竞态测试。
func (d *delayedEchoMock) StreamChat(ctx context.Context, req llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	select {
	case <-ctx.Done():
		return llm.ChatResult{}, ctx.Err()
	case <-time.After(d.delay):
	}
	return (&llm.MockClient{}).StreamChat(ctx, req, handler)
}

func (d *delayedEchoMock) CompleteText(ctx context.Context, req llm.CompleteRequest) (string, error) {
	return (&llm.MockClient{}).CompleteText(ctx, req)
}

func (d *delayedEchoMock) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return (&llm.MockClient{}).NormalizeAssistant(existing, msg)
}

// TestChildAgentCancelBeforeComplete 取消进行中的子 Agent。
func TestChildAgentCancelBeforeComplete(t *testing.T) {
	mock := &delayedEchoMock{delay: 2 * time.Second}
	mgr, cm, hub := newManagerWithChildAgents(t, mock)
	defer mgr.Stop()

	parent, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}

	ch := hub.Subscribe(0)
	defer hub.Unsubscribe(ch)

	ctx := context.Background()
	created := make(chan string, 1)
	go func() {
		out, err := cm.HandleCreate(ctx, parent.ID, `{"task":"slow job","purpose":"cancel test"}`)
		if err != nil {
			created <- ""
			return
		}
		var result map[string]any
		_ = json.Unmarshal([]byte(out), &result)
		childID, _ := result["child_agent_id"].(string)
		created <- childID
	}()
	var childID string
	deadline := time.Now().Add(time.Second)
	for childID == "" && time.Now().Before(deadline) {
		items, _ := mgr.ListChildAgents(parent.ID)
		if len(items) > 0 {
			childID = items[0].ChildAgentID
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if childID == "" {
		t.Fatal("child was not created")
	}

	cancelOut, err := cm.HandleCancelTool(parent.ID, `{"child_agent_id":"`+childID+`","reason":"test cancel"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cancelOut, `"status":"cancelled"`) {
		t.Fatalf("unexpected cancel: %s", cancelOut)
	}
	<-created

	cancelDeadline := time.After(3 * time.Second)
	gotCancelled := false
	for !gotCancelled {
		select {
		case ev := <-ch:
			if ev.Type == "temporary_agent_cancelled" {
				gotCancelled = true
			}
		case <-cancelDeadline:
			t.Fatal("timeout waiting for temporary_agent_cancelled")
		}
	}
}

// TestEnqueueResumeParentDoesNotDoubleEnqueue 启用子 Agent 时父 session resume 只入队一次。
func TestEnqueueResumeParentDoesNotDoubleEnqueue(t *testing.T) {
	mgr, _, _ := newManagerWithChildAgents(t, &llm.MockClient{})
	defer mgr.Stop()

	parent, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(parent.ID)
	if rt == nil {
		t.Fatal("parent runtime missing")
	}
	setTestPendingHITL(t, rt, &turn.PendingHITL{
		Items: []turn.PendingHITLItem{{
			ToolCall: llm.ToolCall{
				ID: "call-test-1",
				Function: llm.ToolCallFunction{
					Name:      "ask_user_information",
					Arguments: `{"question":"q"}`,
				},
			},
		}},
	})

	resume := map[string]any{
		"type":         "user_information",
		"tool_call_id": "call-test-1",
		"answer":       "yes",
	}
	before := rt.queue.TotalEnqueued()
	if _, err := mgr.EnqueueMessage(context.Background(), parent.ID, "resume", "", nil, resume, ""); err != nil {
		t.Fatal(err)
	}
	if got := rt.queue.TotalEnqueued() - before; got != 1 {
		t.Fatalf("resume enqueue count = %d, want 1", got)
	}
}

func writeChildTestSkill(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSpawnChildPreloadsSkills(t *testing.T) {
	skillsRoot := t.TempDir()
	writeChildTestSkill(t, skillsRoot, "writer", "---\nname: writer\ndescription: Write docs\n---\nWrite clearly.\n")

	hub := stream.NewHub(64, logx.Discard())
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	pol := policy.NewDefaultEngine()
	mgr := NewManager("agent-1", hub, &llm.MockClient{}, reg, pol, nil, TurnOptions{
		SkillsRoot:          skillsRoot,
		SkillsEnabled:       true,
		SkillsMaxInPrompt:   3,
		CompressionBlocking: 0,
	}, logx.Discard())
	cm := childagent.NewManager(childagent.Config{Enabled: true}, hub, "agent-1", nil)
	mgr.SetChildAgentManager(cm)
	defer mgr.Stop()

	parent, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	out, err := cm.HandleCreate(ctx, parent.ID, `{"task":"write summary","purpose":"docs","skill_names":["writer"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(out, "ERROR:") {
		t.Fatalf("create failed: %s", out)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	childID, _ := payload["child_agent_id"].(string)
	if childID == "" {
		t.Fatalf("missing child_agent_id in %v", payload)
	}
	items, err := mgr.ListChildAgents(parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || len(items[0].LoadedSkills) != 1 || items[0].LoadedSkills[0] != "writer" {
		t.Fatalf("loaded skills = %+v", items)
	}
}

func TestSpawnChildRejectsUnknownSkill(t *testing.T) {
	skillsRoot := t.TempDir()
	hub := stream.NewHub(64, logx.Discard())
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	pol := policy.NewDefaultEngine()
	mgr := NewManager("agent-1", hub, &llm.MockClient{}, reg, pol, nil, TurnOptions{
		SkillsRoot:        skillsRoot,
		SkillsEnabled:     true,
		SkillsMaxInPrompt: 3,
	}, logx.Discard())
	cm := childagent.NewManager(childagent.Config{Enabled: true}, hub, "agent-1", nil)
	mgr.SetChildAgentManager(cm)
	defer mgr.Stop()

	parent, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	out, err := cm.HandleCreate(context.Background(), parent.ID, `{"task":"x","purpose":"y","skill_names":["missing"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "unknown skill") {
		t.Fatalf("expected unknown skill error, got %q", out)
	}
}
