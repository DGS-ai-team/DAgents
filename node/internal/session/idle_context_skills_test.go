package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func TestIdleContextBoundaryRefreshesOnlyLoadedSkills(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	stateRoot := filepath.Join(workspace, ".dagents", "agent-1")
	skillsRoot := filepath.Join(root, "skills")
	writerPath := filepath.Join(skillsRoot, "writer", "SKILL.md")
	analystPath := filepath.Join(skillsRoot, "analyst", "SKILL.md")
	writeIdleContextSkill(t, writerPath, "writer", "v1", "OLD_WRITER_BODY")
	writeIdleContextSkill(t, analystPath, "analyst", "v1", "UNLOADED_ANALYST_BODY")

	st, err := store.Open(filepath.Join(root, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	client := &idleContextSkillsCaptureClient{}
	registry, err := tools.NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	opts := TurnOptions{
		WorkspaceRoot: workspace, WorkspaceStateRoot: stateRoot,
		SkillsRoot: skillsRoot, SkillsEnabled: true, SkillsMaxInPrompt: 5,
		IdleNewConversationSeconds: 1,
	}
	mgr := NewManager("agent-1", stream.NewHub(32, logx.Discard()), client, registry, nil, st, opts, logx.Discard())
	defer mgr.Stop()
	sess, _, err := mgr.Create("idle-skill-boundary")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.LoadSessionSkill(sess.ID, "writer"); err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	if loaded := rt.getLoadedSkills(); len(loaded) != 1 || loaded[0].SkillName != "writer" {
		t.Fatalf("loaded skills = %+v, want only writer", loaded)
	}
	rt.orch.SetLifecycleCommandSink(nil)
	rt.mu.Lock()
	rt.messages = []llm.Message{
		llm.UserMessage("上一段请求", llm.UserNameHuman),
		{Role: "assistant", Content: "上一段答复"},
	}
	rt.historyRevision = 2
	rt.lastHumanInteraction = time.Now().Add(-2 * time.Second)
	rt.mu.Unlock()
	if err := rt.persist(ctx); err != nil {
		t.Fatal(err)
	}
	if err := rt.maybeStartIdleConversation(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if rt.activeContextStart != 2 {
		t.Fatalf("activeContextStart = %d, want boundary after prior exchange", rt.activeContextStart)
	}

	// The active skill remains loaded across the boundary, but its body is
	// deliberately reread from disk when the next provider request is built.
	writeIdleContextSkill(t, writerPath, "writer", "v2", "NEW_WRITER_BODY")
	// The production input path observes catalog changes before starting the
	// next human turn; mirror that boundary when driving the orchestrator here.
	rt.observeSkillCatalogChange()
	history := rt.activeMessagesSnapshot()
	user := llm.UserMessage("本次新请求", llm.UserNameHuman)
	outcome := rt.orch.RunHumanMessageTurn(ctx, sess.ID, &history, user)
	if outcome.Err != nil {
		t.Fatal(outcome.Err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(client.requests))
	}
	var skillBodies []string
	for _, message := range client.requests[0].Messages {
		if llm.IsMessageSource(message, llm.MessageSourcePlugin, llm.MessageFormInstructions, "skills") {
			skillBodies = append(skillBodies, message.Content)
		}
	}
	if len(skillBodies) != 1 {
		t.Fatalf("skill instruction messages = %d, want only explicitly loaded writer; messages=%+v", len(skillBodies), client.requests[0].Messages)
	}
	if !strings.Contains(skillBodies[0], "NEW_WRITER_BODY") || strings.Contains(skillBodies[0], "OLD_WRITER_BODY") {
		t.Fatalf("request did not reread updated writer skill: %q", skillBodies[0])
	}
	if strings.Contains(skillBodies[0], "UNLOADED_ANALYST_BODY") {
		t.Fatalf("visible but unloaded skill body was injected: %q", skillBodies[0])
	}
	if strings.Contains(skillBodies[0], "OLD_WRITER_BODY") {
		t.Fatalf("stale skill body was injected after the boundary: %q", skillBodies[0])
	}
	userMessages := 0
	for _, message := range client.requests[0].Messages {
		if llm.IsMessageSource(message, llm.MessageSourceUser, llm.MessageFormRequest, "human") && message.Content == "本次新请求" {
			userMessages++
		}
	}
	if userMessages != 1 {
		t.Fatalf("current human message copies in provider request = %d, want 1", userMessages)
	}
}

func writeIdleContextSkill(t *testing.T, path, name, version, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	contents := "---\nname: " + name + "\ndescription: " + version + "\n---\n" + body + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

type idleContextSkillsCaptureClient struct {
	requests []llm.ChatRequest
}

func (c *idleContextSkillsCaptureClient) StreamChat(_ context.Context, req llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	c.requests = append(c.requests, req)
	if handler.OnDelta != nil {
		handler.OnDelta("完成")
	}
	return llm.ChatResult{Content: "完成", FinishReason: "stop"}, nil
}

func (*idleContextSkillsCaptureClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "摘要", nil
}

func (*idleContextSkillsCaptureClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

var _ llm.Client = (*idleContextSkillsCaptureClient)(nil)
