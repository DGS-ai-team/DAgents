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
	if !strings.Contains(client.requests[0].SystemPrompt, "writer: v1") || strings.Contains(client.requests[0].SystemPrompt, "v2") {
		t.Fatalf("initial system prompt should contain the frozen v1 metadata: %q", client.requests[0].SystemPrompt)
	}
	if !containsToolMessageText(client.requests[1].Messages, "catalog_changed") {
		t.Fatalf("model did not receive catalog_changed result: %#v", client.requests[1].Messages)
	}

	// The external edit is picked up only when the runtime observes a new
	// human-Turn boundary. The discovery tool can see the new metadata before
	// then, while the stable system prompt remains unchanged.
	rt.observeSkillCatalogChange()
	if metadata := rt.skillsTurnCatalog.ListMetadata(); len(metadata) != 1 || metadata[0].Description != "v2" {
		t.Fatalf("next human boundary did not refresh catalog metadata: %+v", metadata)
	}
	outcome = rt.orch.RunHumanMessageTurn(context.Background(), rt.session.ID, &history, llm.UserMessage("继续", llm.UserNameHuman))
	if outcome.Err != nil {
		t.Fatal(outcome.Err)
	}
	if len(client.requests) != 3 {
		t.Fatalf("second human turn requests = %d, want 3 total", len(client.requests))
	}
	if !strings.Contains(client.requests[2].SystemPrompt, "writer: v2") || strings.Contains(client.requests[2].SystemPrompt, "writer: v1") {
		t.Fatalf("next human system prompt did not refresh catalog metadata: %q", client.requests[2].SystemPrompt)
	}
}

func TestRuntimeCompressionRefreshesSkillMetadataPromptBoundary(t *testing.T) {
	fsRoot := t.TempDir()
	skillsRoot := filepath.Join(fsRoot, "skills")
	if err := os.MkdirAll(filepath.Join(skillsRoot, "writer"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillsRoot, "writer", "SKILL.md")
	writeRuntimeSkill(t, skillPath, "v1", "body")

	client := &compressionSkillBoundaryClient{skillPath: skillPath}
	rt := newRuntimeWithPublisher(
		"session-compress-skills", "agent-1", stream.NewHub(32, logx.Discard()), nil, client, nil, nil, nil, logx.Discard(),
		[]llm.Message{
			llm.UserMessage("第一轮", llm.UserNameHuman),
			{Role: "assistant", Content: "第一轮完成"},
			llm.UserMessage("第二轮", llm.UserNameHuman),
		}, nil, nil, 0, nil, false, 0, 0,
		TurnOptions{FSRoot: fsRoot, SkillsRoot: skillsRoot, SkillsEnabled: true, SkillsMaxInPrompt: 2, CompressionBlocking: 1}, nil,
	)
	initialPrompt := rt.orch.SystemPromptForSession(rt.session.ID)
	if !strings.Contains(initialPrompt, "writer: v1") {
		t.Fatalf("initial prompt missing v1 metadata: %q", initialPrompt)
	}
	result := rt.compressContext(context.Background())
	if result.Status != "applied" {
		t.Fatalf("compression result = %+v", result)
	}
	latestPrompt := rt.orch.SystemPromptForSession(rt.session.ID)
	if !strings.Contains(latestPrompt, "writer: v2") || strings.Contains(latestPrompt, "writer: v1") {
		t.Fatalf("compression did not refresh the prompt metadata boundary: %q", latestPrompt)
	}
}

func TestRuntimeCompressionInvalidatesActiveModelContext(t *testing.T) {
	fsRoot := t.TempDir()
	skillsRoot := filepath.Join(fsRoot, "skills")
	if err := os.MkdirAll(filepath.Join(skillsRoot, "writer"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillsRoot, "writer", "SKILL.md")
	writeRuntimeSkill(t, skillPath, "v1", "body")

	client := &activeCompressionSkillClient{}
	rt := newRuntimeWithPublisher(
		"session-active-compress", "agent-1", stream.NewHub(32, logx.Discard()), nil, client, nil, nil, nil, logx.Discard(),
		nil, nil, nil, 0, nil, false, 0, 0,
		TurnOptions{FSRoot: fsRoot, SkillsRoot: skillsRoot, SkillsEnabled: true, SkillsMaxInPrompt: 2, CompressionBlocking: 1}, nil,
	)
	rt.orch.SetLifecycleCommandSink(nil)

	history := []llm.Message{}
	outcome := rt.orch.RunHumanMessageTurn(context.Background(), rt.session.ID, &history, llm.UserMessage("加载写作 skill", llm.UserNameHuman))
	if outcome.Err != nil || !outcome.ScheduleToolResult {
		t.Fatalf("initial active turn outcome = %+v", outcome)
	}
	rt.mu.Lock()
	rt.messages = append([]llm.Message(nil), history...)
	rt.mu.Unlock()

	writeRuntimeSkill(t, skillPath, "v2", "body")
	result := rt.compressContext(context.Background())
	if result.Status != "applied" {
		t.Fatalf("active compression result = %+v", result)
	}
	rt.mu.Lock()
	history = append([]llm.Message(nil), rt.messages...)
	rt.mu.Unlock()

	outcome = rt.orch.RunToolMessageTurn(context.Background(), rt.session.ID, &history)
	if outcome.Err != nil {
		t.Fatal(outcome.Err)
	}
	if len(client.prompts) != 2 {
		t.Fatalf("active compression model requests = %d, want 2", len(client.prompts))
	}
	if !strings.Contains(client.prompts[0], "writer: v1") || !strings.Contains(client.prompts[1], "writer: v2") {
		t.Fatalf("compression did not rebuild active model context: first=%q second=%q", client.prompts[0], client.prompts[1])
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

type compressionSkillBoundaryClient struct {
	skillPath string
}

func (c *compressionSkillBoundaryClient) StreamChat(_ context.Context, _ llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	if err := os.WriteFile(c.skillPath, []byte("---\nname: writer\ndescription: v2\n---\nbody\n"), 0o644); err != nil {
		return llm.ChatResult{}, err
	}
	if handler.OnDelta != nil {
		handler.OnDelta("压缩摘要")
	}
	return llm.ChatResult{Content: "压缩摘要"}, nil
}

func (c *compressionSkillBoundaryClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "压缩摘要", nil
}

func (c *compressionSkillBoundaryClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

var _ llm.Client = (*compressionSkillBoundaryClient)(nil)

type activeCompressionSkillClient struct {
	prompts []string
	calls   int
}

func (c *activeCompressionSkillClient) StreamChat(_ context.Context, req llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	isCompression := false
	for _, message := range req.Messages {
		if message.Name == llm.UserNameCompressionSidecar {
			isCompression = true
			break
		}
	}
	if isCompression {
		return llm.ChatResult{Content: "压缩摘要"}, nil
	}
	c.prompts = append(c.prompts, req.SystemPrompt)
	c.calls++
	if c.calls == 1 {
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{
			ID: "load-writer",
			Function: llm.ToolCallFunction{
				Name:      "load_skills",
				Arguments: `{"skill_names":["writer"]}`,
			},
		}}}, nil
	}
	if handler.OnDelta != nil {
		handler.OnDelta("完成")
	}
	return llm.ChatResult{Content: "完成"}, nil
}

func (c *activeCompressionSkillClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "压缩摘要", nil
}

func (c *activeCompressionSkillClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

var _ llm.Client = (*activeCompressionSkillClient)(nil)
