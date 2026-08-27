package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

func TestRuntimeSkillCatalogViewBlocksMidTurnBodyChangeUntilNextHumanTurn(t *testing.T) {
	fsRoot := t.TempDir()
	skillsRoot := filepath.Join(fsRoot, "skills")
	if err := os.MkdirAll(filepath.Join(skillsRoot, "writer"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillsRoot, "writer", "SKILL.md")
	writeRuntimeSkill(t, skillPath, "v1", "old body")

	client := &runtimeSkillBoundaryClient{skillPath: skillPath}
	hub := stream.NewHub(32, logx.Discard())
	rt := newRuntimeWithPublisher(
		"session-skills", "agent-1", hub, hub, client, nil, nil, nil, logx.Discard(),
		nil, nil, nil, 0, nil, false, 0, 0,
		TurnOptions{FSRoot: fsRoot, SkillsRoot: skillsRoot, SkillsEnabled: true, SkillsMaxInPrompt: 2}, nil,
	)
	// This test drives the runtime's model-facing catalog path directly; the
	// lifecycle projection is covered by the existing runtime lifecycle suite.
	rt.orch.SetLifecycleCommandSink(nil)

	history := []llm.Message{}
	outcome := rt.orch.RunHumanMessageTurn(context.Background(), rt.session.ID, &history, llm.UserMessage("使用写作 skill", llm.UserNameHuman))
	if outcome.Err != nil {
		t.Fatal(outcome.Err)
	}
	if !outcome.ScheduleToolResult {
		t.Fatalf("first outcome = %+v, want tool continuation", outcome)
	}
	outcome = rt.orch.RunToolMessageTurn(context.Background(), rt.session.ID, &history)
	if outcome.Err != nil {
		t.Fatal(outcome.Err)
	}
	if len(client.requests) != 2 {
		t.Fatalf("first human turn requests = %d, want tool result continuation", len(client.requests))
	}
	if !strings.Contains(client.requests[0].SystemPrompt, "v1") || strings.Contains(client.requests[0].SystemPrompt, "v2") {
		t.Fatalf("first prompt did not use boundary metadata: %q", client.requests[0].SystemPrompt)
	}
	if !containsToolMessageText(client.requests[1].Messages, "catalog_changed") {
		t.Fatalf("model did not receive catalog_changed result: %#v", client.requests[1].Messages)
	}

	// The external edit is picked up only when the runtime observes a new
	// human-Turn boundary. The next prompt must use the new metadata.
	rt.observeSkillCatalogChange()
	outcome = rt.orch.RunHumanMessageTurn(context.Background(), rt.session.ID, &history, llm.UserMessage("继续", llm.UserNameHuman))
	if outcome.Err != nil {
		t.Fatal(outcome.Err)
	}
	if len(client.requests) != 3 {
		t.Fatalf("second human turn requests = %d, want 3 total", len(client.requests))
	}
	if !strings.Contains(client.requests[2].SystemPrompt, "v2") {
		t.Fatalf("next human prompt did not use new catalog view: %q", client.requests[2].SystemPrompt)
	}
}

func writeRuntimeSkill(t *testing.T, path, version, body string) {
	t.Helper()
	content := "---\nname: writer\ndescription: " + version + "\n---\n" + body + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func containsToolMessageText(messages []llm.Message, text string) bool {
	for _, message := range messages {
		if message.Role == "tool" && strings.Contains(message.Content, text) {
			return true
		}
	}
	return false
}

type runtimeSkillBoundaryClient struct {
	skillPath string
	requests  []llm.ChatRequest
	calls     int
}

func (c *runtimeSkillBoundaryClient) StreamChat(_ context.Context, req llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	c.calls++
	c.requests = append(c.requests, req)
	if c.calls == 1 {
		// Same-size replacement: the frozen view must reject it even if the
		// filesystem timestamp granularity would otherwise hide the change.
		if err := os.WriteFile(c.skillPath, []byte("---\nname: writer\ndescription: v2\n---\nnew body\n"), 0o644); err != nil {
			return llm.ChatResult{}, err
		}
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{
			ID: "load-writer",
			Function: llm.ToolCallFunction{
				Name:      "load_skills",
				Arguments: `{"skill_names":["writer"]}`,
			},
		}}}, nil
	}
	if handler.OnDelta != nil {
		handler.OnDelta("已完成")
	}
	return llm.ChatResult{Content: "已完成", FinishReason: "stop"}, nil
}

func (c *runtimeSkillBoundaryClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "摘要", nil
}

func (c *runtimeSkillBoundaryClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

var _ llm.Client = (*runtimeSkillBoundaryClient)(nil)
