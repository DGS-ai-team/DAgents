package turn

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestBuildContextInjections_includesDateWithoutHistoryMutation(t *testing.T) {
	in := SystemPromptInput{
		AgentID:          "agent-date",
		SessionID:        "session-date",
		TodayDateEnabled: true,
		CurrentDate:      "20260720",
	}
	injections := BuildContextInjections(in)
	if len(injections) != 1 {
		t.Fatalf("injections = %+v", injections)
	}
	if !containsAll(injections[0].Content, "## 当前日期", "当天日期为：20260720") {
		t.Fatalf("date context = %q", injections[0].Content)
	}

	history := []llm.Message{llm.UserMessage("hello", llm.UserNameHuman)}
	request := ApplyContextInjections(history, injections)
	if len(history) != 1 {
		t.Fatalf("history was mutated: %+v", history)
	}
	if len(request) != 2 || request[0].Name != llm.UserNameContext {
		t.Fatalf("request = %+v", request)
	}
	if !llm.IsMessageSource(request[0], llm.MessageSourceRuntime, llm.MessageFormSnapshot, "") {
		t.Fatalf("date context source = %+v", request[0].Source)
	}
	if got := StripContextInjections(request); len(got) != 1 || got[0].Content != "hello" {
		t.Fatalf("stripped request = %+v", got)
	}
}

func TestBuildContextInjections_dateDisabled(t *testing.T) {
	injections := BuildContextInjections(SystemPromptInput{
		TodayDateEnabled: false,
		CurrentDate:      "20260720",
	})
	if len(injections) != 1 {
		t.Fatalf("injections = %+v", injections)
	}
	if contains(injections[0].Content, "当前日期") || contains(injections[0].Content, "20260720") {
		t.Fatalf("disabled date leaked into context = %q", injections[0].Content)
	}
}

func TestBuildChildContextInjections_includesDate(t *testing.T) {
	injections := BuildChildContextInjections(ChildSystemPromptInput{
		AgentID:          "child-agent",
		SessionID:        "child-session",
		TodayDateEnabled: true,
		CurrentDate:      "20260720",
	})
	if len(injections) != 1 || !containsAll(injections[0].Content, "当前日期", "20260720", "child-session") {
		t.Fatalf("child context = %+v", injections)
	}
}

func TestOrchestratorDateConfigIsRequestOnly(t *testing.T) {
	enabled := true
	orch := NewOrchestrator("agent-date", t.TempDir(), nil, nil, nil, nil, SkillAccess{}, nil, nil, hooks.RuntimeConfig{
		InjectTodayDate: hooks.InjectTodayDateConfig{Enabled: &enabled},
	}, nil)
	in := orch.systemPromptInput("session-date")
	if !in.TodayDateEnabled || len(in.CurrentDate) != 8 {
		t.Fatalf("date input = %+v", in)
	}
	if got := orch.buildContextInjectionsWithInput(in); len(got) != 1 || !contains(got[0].Content, in.CurrentDate) {
		t.Fatalf("orchestrator context = %+v", got)
	}
}
