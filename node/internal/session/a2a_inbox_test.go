package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func TestRunInboxConsultation_streamsAssistant(t *testing.T) {
	hub := stream.NewHub(32, logx.Discard())
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	pol, _ := policy.LoadFile("")
	mock := &llm.MockClient{FixedReply: "APPROVED | rule=R-ANON-01 | ok"}
	mgr := NewManager("node-a", hub, mock, reg, pol, nil, TurnOptions{}, logx.Discard())
	defer mgr.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got, err := mgr.RunInboxConsultation(ctx, "task-1", "【合规咨询】脱敏统计 CHG-2026-0142")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "APPROVED") || !strings.Contains(got, "R-ANON-01") {
		t.Fatalf("result=%q", got)
	}
}

func TestInboxSessionID(t *testing.T) {
	if got := inboxSessionID("a2a-task-abc123"); got != "a2a-a2a-task-abc123" {
		t.Fatalf("got %q", got)
	}
}
