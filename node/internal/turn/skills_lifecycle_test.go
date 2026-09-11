package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

func TestLoadSkillsResultDescribesStateAndContextBoundaries(t *testing.T) {
	root := t.TempDir()
	writeLifecycleSkill(t, root, "writer", "---\nname: writer\ndescription: Write docs\n---\nWrite clearly.\n")
	catalog := skills.NewCatalog(root, true, 2)
	loaded := []skills.LoadedSkill{}
	o := NewOrchestrator("agent-1", t.TempDir(), stream.NewHub(8, logx.Discard()), &llm.MockClient{}, nil, nil, SkillAccess{
		Catalog: catalog,
		Get:     func() []skills.LoadedSkill { return loaded },
		Set:     func(items []skills.LoadedSkill) { loaded = append([]skills.LoadedSkill(nil), items...) },
	}, nil, nil, hooks.RuntimeConfig{}, logx.Discard())

	history := []llm.Message{}
	call := llm.ToolCall{ID: "skill-call", Function: llm.ToolCallFunction{
		Name:      "load_skills",
		Arguments: `{"skill_names":["writer","missing"]}`,
	}}
	if err := o.executeSkillTool("session-1", &history, call); err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Role != "tool" {
		t.Fatalf("history = %+v", history)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(history[0].Content), &result); err != nil {
		t.Fatalf("tool result is not JSON: %v; content=%q", err, history[0].Content)
	}
	if result["session_state_applied_boundary"] != "immediate" || result["model_context_applied_boundary"] != "next_model_step" {
		t.Fatalf("boundary result = %+v", result)
	}
	if result["hooks_status"] != "synchronized" {
		t.Fatalf("hooks status = %+v", result["hooks_status"])
	}
	if got, ok := result["hooks_loaded"].([]any); !ok || got == nil || len(got) != 0 {
		t.Fatalf("hooks_loaded = %#v, want empty array", result["hooks_loaded"])
	}
	if got, ok := result["hooks_failed"].([]any); !ok || got == nil || len(got) != 0 {
		t.Fatalf("hooks_failed = %#v, want empty array", result["hooks_failed"])
	}
	if len(loaded) != 1 || loaded[0].SkillName != "writer" {
		t.Fatalf("loaded = %+v", loaded)
	}
	if err := o.executeSkillTool("session-1", &history, call); err != nil {
		t.Fatal(err)
	}
	var unchanged map[string]any
	if err := json.Unmarshal([]byte(history[len(history)-1].Content), &unchanged); err != nil {
		t.Fatalf("unchanged tool result is not JSON: %v", err)
	}
	if unchanged["model_context_applied_boundary"] != "unchanged" {
		t.Fatalf("unchanged skill mutation boundary = %+v", unchanged["model_context_applied_boundary"])
	}
}

func TestListAvailableSkillsIsMetadataOnlyAndDoesNotMutateContext(t *testing.T) {
	root := t.TempDir()
	writeLifecycleSkill(t, root, "writer", "---\nname: writer\ndescription: Write docs\n---\nSECRET BODY\n")
	catalog := skills.NewCatalog(root, true, 2)
	o := NewOrchestrator("agent-1", t.TempDir(), stream.NewHub(8, logx.Discard()), &llm.MockClient{}, nil, nil, SkillAccess{
		Catalog: catalog,
	}, nil, nil, hooks.RuntimeConfig{}, logx.Discard())

	history := []llm.Message{}
	if err := o.executeSkillTool("session-1", &history, llm.ToolCall{ID: "list-call", Function: llm.ToolCallFunction{
		Name:      "list_available_skills",
		Arguments: `{"query":"writer","limit":20}`,
	}}); err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("history len = %d", len(history))
	}
	o.contextMutationMu.Lock()
	_, refreshed := o.contextMutations["session-1"]
	o.contextMutationMu.Unlock()
	if refreshed {
		t.Fatal("list query must not request context refresh")
	}
	if strings.Contains(history[0].Content, "SECRET BODY") {
		t.Fatalf("list result leaked skill body: %q", history[0].Content)
	}
	var result struct {
		Status string `json:"status"`
		Skills []struct {
			SkillName string `json:"skill_name"`
			Body      string `json:"content"`
		} `json:"skills"`
	}
	if err := json.Unmarshal([]byte(history[0].Content), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" || len(result.Skills) != 1 || result.Skills[0].SkillName != "writer" {
		t.Fatalf("list result = %+v", result)
	}
	if result.Skills[0].Body != "" {
		t.Fatalf("metadata result unexpectedly has body: %+v", result.Skills[0])
	}
}

func TestListAvailableSkillsIsAvailableWhenSkillsAreEnabled(t *testing.T) {
	root := t.TempDir()
	writeLifecycleSkill(t, root, "writer", "---\nname: writer\ndescription: Write docs\n---\nBody\n")
	o := NewOrchestrator("agent-1", t.TempDir(), stream.NewHub(8, logx.Discard()), &llm.MockClient{}, nil, nil, SkillAccess{
		Catalog: skills.NewCatalog(root, true, 2),
	}, nil, nil, hooks.RuntimeConfig{}, logx.Discard())
	history := []llm.Message{}
	if err := o.executeSkillTool("session-1", &history, llm.ToolCall{ID: "list-call", Function: llm.ToolCallFunction{
		Name:      "list_available_skills",
		Arguments: `{}`,
	}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(history[0].Content, "experiment_disabled") || !strings.Contains(history[0].Content, `"status":"succeeded"`) {
		t.Fatalf("list result = %q", history[0].Content)
	}
}

func TestListAvailableSkillsUsesLiveCatalogWithoutRewritingPromptMetadata(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "writer", "SKILL.md")
	writeLifecycleSkill(t, root, "writer", "---\nname: writer\ndescription: v1\n---\nBody\n")
	live := skills.NewCatalog(root, true, 2)
	frozen := live.NewTurnView()
	o := NewOrchestrator("agent-1", t.TempDir(), stream.NewHub(8, logx.Discard()), &llm.MockClient{}, nil, nil, SkillAccess{
		Catalog:     frozen,
		LiveCatalog: live,
	}, nil, nil, hooks.RuntimeConfig{}, nil)
	initialPrompt := o.buildSystemPrompt("session-1")
	if !strings.Contains(initialPrompt, "writer: v1") {
		t.Fatalf("initial prompt missing frozen metadata: %q", initialPrompt)
	}
	if err := os.WriteFile(path, []byte("---\nname: writer\ndescription: v2\n---\nBody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	history := []llm.Message{}
	if err := o.executeSkillTool("session-1", &history, llm.ToolCall{ID: "list-live", Function: llm.ToolCallFunction{
		Name:      "list_available_skills",
		Arguments: `{}`,
	}}); err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || !strings.Contains(history[0].Content, `"description":"v2"`) {
		t.Fatalf("live discovery did not return latest metadata: %+v", history)
	}
	unchangedPrompt := o.buildSystemPrompt("session-1")
	if unchangedPrompt != initialPrompt || strings.Contains(unchangedPrompt, "v2") {
		t.Fatalf("list discovery rewrote frozen system prompt metadata: %q", unchangedPrompt)
	}
}

func TestListAvailableSkillsLiveCatalogCannotWidenVisibility(t *testing.T) {
	root := t.TempDir()
	writeLifecycleSkill(t, root, "allowed", "---\nname: allowed\ndescription: Allowed\n---\nBody\n")
	writeLifecycleSkill(t, root, "hidden", "---\nname: hidden\ndescription: Hidden\n---\nSecret\n")
	policyCatalog := skills.NewCatalog(root, true, 2).RestrictVisible([]string{"allowed"})
	liveCatalog := skills.NewCatalog(root, true, 2)
	o := NewOrchestrator("agent-1", t.TempDir(), stream.NewHub(8, logx.Discard()), &llm.MockClient{}, nil, nil, SkillAccess{
		Catalog:     policyCatalog,
		LiveCatalog: liveCatalog,
	}, nil, nil, hooks.RuntimeConfig{}, logx.Discard())

	history := []llm.Message{}
	if err := o.executeSkillTool("session-1", &history, llm.ToolCall{ID: "list-call", Function: llm.ToolCallFunction{
		Name:      "list_available_skills",
		Arguments: `{}`,
	}}); err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("history len = %d", len(history))
	}
	if strings.Contains(history[0].Content, `"skill_name":"hidden"`) || !strings.Contains(history[0].Content, `"skill_name":"allowed"`) {
		t.Fatalf("list result crossed visibility boundary: %s", history[0].Content)
	}
}

func TestLoadSkillsBodyBecomesVisibleOnNextModelStep(t *testing.T) {
	root := t.TempDir()
	writeLifecycleSkill(t, root, "writer", "---\nname: writer\ndescription: Write docs\n---\nWrite clearly.\n")
	catalog := skills.NewCatalog(root, true, 2)
	loaded := []skills.LoadedSkill{}
	client := &skillBoundaryClient{}
	o := NewOrchestrator("agent-1", t.TempDir(), stream.NewHub(16, logx.Discard()), client, nil, nil, SkillAccess{
		Catalog: catalog,
		Get:     func() []skills.LoadedSkill { return loaded },
		Set:     func(items []skills.LoadedSkill) { loaded = append([]skills.LoadedSkill(nil), items...) },
	}, nil, nil, hooks.RuntimeConfig{}, logx.Discard())

	history := []llm.Message{}
	if _, _, err := runMessageTurnInline(t, o, context.Background(), "session-1", &history, "使用写作 skill", nil); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests after load turn = %d", len(client.requests))
	}
	if client.requests[0].SystemPrompt != client.requests[1].SystemPrompt {
		t.Fatal("skill activation should not rewrite the stable system prompt")
	}
	if !hasSkillBodyMessage(client.requests[1].Messages, "Write clearly.") {
		t.Fatalf("loaded skill body is missing from the next model step: %+v", client.requests[1].Messages)
	}

	outcome := o.RunHumanMessageTurn(
		WithExecutionContext(context.Background(), TurnExecutionContext{SessionID: "session-1", StepIndex: 1}),
		"session-1", &history, llm.UserMessage("继续写作", llm.UserNameHuman),
	)
	if outcome.Err != nil {
		t.Fatal(outcome.Err)
	}
	if len(client.requests) != 3 || !hasSkillBodyMessage(client.requests[2].Messages, "Write clearly.") || strings.Contains(client.requests[2].SystemPrompt, "Write clearly.") {
		t.Fatalf("next human request did not use durable skill body: requests=%d request=%+v", len(client.requests), client.requests[2])
	}
}

func hasSkillBodyMessage(messages []llm.Message, body string) bool {
	for _, message := range messages {
		if message.Role == "user" && message.Name == llm.UserNameSkill && strings.Contains(message.Content, body) {
			return true
		}
	}
	return false
}

func TestSkillSnapshotRecordsLoadedBodyDigestSeparately(t *testing.T) {
	root := t.TempDir()
	writeLifecycleSkill(t, root, "writer", "---\nname: writer\ndescription: Write docs\n---\nWrite clearly.\n")
	catalog := skills.NewCatalog(root, true, 2)
	loaded := catalog.SetLoadedSkills([]string{"writer"})
	o := NewOrchestrator("agent-1", t.TempDir(), stream.NewHub(8, logx.Discard()), &llm.MockClient{}, nil, nil, SkillAccess{
		Catalog: catalog,
		Get:     func() []skills.LoadedSkill { return loaded },
	}, nil, nil, hooks.RuntimeConfig{}, logx.Discard())

	snapshot := NewModelContextSnapshot("prompt", nil, 1, "runtime")
	o.attachSkillsSnapshotMetadata(snapshot)
	if snapshot.LoadedSkillsDigest == "" || snapshot.LoadedSkillsContentDigest == "" {
		t.Fatalf("skill snapshot digests = %+v", snapshot)
	}
	if snapshot.LoadedSkillsDigest == snapshot.LoadedSkillsContentDigest {
		t.Fatalf("metadata and body digests should identify different inputs: %+v", snapshot)
	}
	observed := snapshot.observability()
	if observed["loaded_skills_content_digest"] != snapshot.LoadedSkillsContentDigest {
		t.Fatalf("content digest missing from observability: %+v", observed)
	}
}

func TestSkillToolReportsHookSyncFailures(t *testing.T) {
	root := t.TempDir()
	writeLifecycleSkill(t, root, "writer", "---\nname: writer\ndescription: Write docs\n---\nWrite clearly.\n")
	catalog := skills.NewCatalog(root, true, 2)
	loaded := []skills.LoadedSkill{}
	o := NewOrchestrator("agent-1", t.TempDir(), stream.NewHub(8, logx.Discard()), &llm.MockClient{}, nil, nil, SkillAccess{
		Catalog: catalog,
		Get:     func() []skills.LoadedSkill { return loaded },
		SetWithHookStatus: func(items []skills.LoadedSkill) SkillHooksSyncResult {
			loaded = append([]skills.LoadedSkill(nil), items...)
			return SkillHooksSyncResult{
				Status: "partial",
				Failed: []SkillHookSyncFailure{{SkillName: "writer", Error: "plugin load failed"}},
			}
		},
	}, nil, nil, hooks.RuntimeConfig{}, logx.Discard())

	history := []llm.Message{}
	if err := o.executeSkillTool("session-1", &history, llm.ToolCall{ID: "skill-call", Function: llm.ToolCallFunction{
		Name: "load_skills", Arguments: `{"skill_names":["writer"]}`,
	}}); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(history[0].Content), &result); err != nil {
		t.Fatal(err)
	}
	if result["hooks_status"] != "partial" || !strings.Contains(history[0].Content, "plugin load failed") {
		t.Fatalf("hook failure result = %+v", result)
	}
}

type skillBoundaryClient struct {
	requests []llm.ChatRequest
	calls    int
}

func (c *skillBoundaryClient) StreamChat(_ context.Context, req llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	c.calls++
	c.requests = append(c.requests, req)
	if c.calls == 1 {
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{
			ID:       "load-writer",
			Function: llm.ToolCallFunction{Name: "load_skills", Arguments: `{"skill_names":["writer"]}`},
		}}}, nil
	}
	text := "已完成"
	if handler.OnDelta != nil {
		handler.OnDelta(text)
	}
	return llm.ChatResult{Content: text, FinishReason: "stop"}, nil
}

func (c *skillBoundaryClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "摘要", nil
}

func (c *skillBoundaryClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func TestContextRefreshKeepsDistinctMutationReasons(t *testing.T) {
	o := NewOrchestrator("agent-1", t.TempDir(), stream.NewHub(8, logx.Discard()), &llm.MockClient{}, nil, nil, SkillAccess{}, nil, nil, hooks.RuntimeConfig{}, logx.Discard())
	o.RequestModelContextRefresh("session-1", "skills_load")
	o.RequestModelContextRefresh("session-1", "context_compression")
	o.RequestModelContextRefresh("session-1", "skills_load")
	if got := o.consumeModelContextRefresh("session-1"); got != "skills_load,context_compression" {
		t.Fatalf("context refresh reasons = %q", got)
	}
}

func writeLifecycleSkill(t *testing.T, root, name, content string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
