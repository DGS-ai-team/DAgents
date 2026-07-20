package turn

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

func TestInjectTodayDateHook_onHumanTurn(t *testing.T) {
	hub := stream.NewHub(8, logx.Discard())
	orch := testOrchestrator(t, hub, &llm.MockClient{})
	today := time.Now().Format("20060102")
	want := hooks.FormatTodayDateMessage(today)

	var history []llm.Message
	_, _, err := runMessageTurnInline(t, orch, context.Background(), "sess-date", &history, "hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) < 3 {
		t.Fatalf("history too short: %+v", history)
	}
	if history[0].Role != "user" || history[0].Content != want || history[0].Name != hooks.TodayDateMessageName {
		t.Fatalf("date msg = %+v, want %q", history[0], want)
	}
	if history[1].Role != "user" || history[1].Content != "hello" {
		t.Fatalf("user msg = %+v", history[1])
	}
	if history[2].Role != "assistant" || history[2].Content != "hello" {
		t.Fatalf("assistant = %+v", history[2])
	}

	// 同日第二轮不再重复插入
	prevLen := len(history)
	_, _, err = runMessageTurnInline(t, orch, context.Background(), "sess-date", &history, "again", nil)
	if err != nil {
		t.Fatal(err)
	}
	dateCount := 0
	for _, m := range history {
		if m.Role == "user" && strings.TrimSpace(m.Content) == want {
			dateCount++
		}
	}
	if dateCount != 1 {
		t.Fatalf("date messages = %d, history=%+v", dateCount, history)
	}
	if len(history) != prevLen+2 { // user + assistant
		t.Fatalf("len %d -> %d, want +2", prevLen, len(history))
	}
}
