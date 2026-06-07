package childagent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

// TestHandleWaitAfterActiveUnregistered 异步临时 Agent 交付并 unregisterActive 后 wait_temporary_agents 仍可读终态。
func TestHandleWaitAfterActiveUnregistered(t *testing.T) {
	hub := stream.NewHub(16, nil)
	m := NewManager(Config{Enabled: true}, hub, "agent-1", nil)
	host := &fakeHost{parentOK: true}
	m.BindHost(host)

	parentID := "sess-parent"
	out, err := m.HandleCreate(context.Background(), parentID, `{"task":"work","purpose":"wait test","wait":false}`)
	if err != nil {
		t.Fatal(err)
	}
	childID := extractChildID(t, out)

	m.finishWithEvent(childID, StatusCompleted, "all done", "", false, "")

	waitOut, err := m.HandleWait(context.Background(), parentID, `{"child_session_ids":["`+childID+`"],"timeout_seconds":0}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(waitOut, "ERROR:") {
		t.Fatalf("unexpected error output: %s", waitOut)
	}
	if !strings.Contains(waitOut, `"status":"completed"`) {
		t.Fatalf("expected completed status: %s", waitOut)
	}
	if !strings.Contains(waitOut, "all done") {
		t.Fatalf("expected summary in wait output: %s", waitOut)
	}
}

// TestHandleWaitRejectsWrongParent 已交付子 Agent 仍校验 parent 归属。
func TestHandleWaitRejectsWrongParent(t *testing.T) {
	hub := stream.NewHub(16, nil)
	m := NewManager(Config{Enabled: true}, hub, "agent-1", nil)
	host := &fakeHost{parentOK: true}
	m.BindHost(host)

	parentID := "sess-parent"
	out, err := m.HandleCreate(context.Background(), parentID, `{"task":"work","purpose":"owner","wait":false}`)
	if err != nil {
		t.Fatal(err)
	}
	childID := extractChildID(t, out)
	m.finishWithEvent(childID, StatusCompleted, "ok", "", false, "")

	waitOut, err := m.HandleWait(context.Background(), "other-parent", `{"child_session_ids":["`+childID+`"],"timeout_seconds":0}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(waitOut, "ERROR:") || !strings.Contains(waitOut, "not owned") {
		t.Fatalf("expected ownership error: %s", waitOut)
	}
}

// TestGetResultFromSettledCache unregisterActive 后 GetResult 仍从 settledResults 返回快照。
func TestGetResultFromSettledCache(t *testing.T) {
	hub := stream.NewHub(16, nil)
	m := NewManager(Config{Enabled: true}, hub, "agent-1", nil)
	host := &fakeHost{parentOK: true}
	m.BindHost(host)

	out, err := m.HandleCreate(context.Background(), "p1", `{"task":"x","purpose":"cache","wait":false}`)
	if err != nil {
		t.Fatal(err)
	}
	childID := extractChildID(t, out)
	m.finishWithEvent(childID, StatusCompleted, "cached summary", "", false, "")

	res, err := m.GetResult(childID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusCompleted || res.Summary != "cached summary" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

// TestHandleWaitPollsUntilTerminal 活跃子 Agent 在超时内进入终态时 wait 返回 completed。
func TestHandleWaitPollsUntilTerminal(t *testing.T) {
	hub := stream.NewHub(16, nil)
	m := NewManager(Config{Enabled: true}, hub, "agent-1", nil)
	host := &fakeHost{parentOK: true}
	m.BindHost(host)

	out, err := m.HandleCreate(context.Background(), "p1", `{"task":"slow","purpose":"poll","wait":false}`)
	if err != nil {
		t.Fatal(err)
	}
	childID := extractChildID(t, out)

	go func() {
		time.Sleep(300 * time.Millisecond)
		m.finishWithEvent(childID, StatusCompleted, "polled", "", false, "")
	}()

	waitOut, err := m.HandleWait(context.Background(), "p1", `{"child_session_ids":["`+childID+`"],"timeout_seconds":5}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(waitOut, `"status":"completed"`) {
		t.Fatalf("expected completed after poll: %s", waitOut)
	}
}

func extractChildID(t *testing.T, createOut string) string {
	t.Helper()
	// 轻量解析 JSON handle 中的 child_session_id。
	const key = `"child_session_id":"`
	idx := strings.Index(createOut, key)
	if idx < 0 {
		t.Fatalf("missing child_session_id in: %s", createOut)
	}
	rest := createOut[idx+len(key):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatalf("malformed json: %s", createOut)
	}
	return rest[:end]
}
