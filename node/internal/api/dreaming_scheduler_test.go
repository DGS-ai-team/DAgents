package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/autonomy"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

type schedulerTestLLM struct {
	calls    int
	requests []llm.ChatRequest
}

func (c *schedulerTestLLM) StreamChat(_ context.Context, request llm.ChatRequest, _ llm.StreamHandler) (llm.ChatResult, error) {
	c.calls++
	c.requests = append(c.requests, request)
	if c.calls == 1 {
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "w", Type: "function", Function: llm.ToolCallFunction{Name: "write_file", Arguments: `{"path":"handbook/dream.md","content":"dreamed","call_purpose":"maintain"}`}}}, FinishReason: "tool_calls"}, nil
	}
	return llm.ChatResult{Content: "dream summary", FinishReason: "stop"}, nil
}
func (*schedulerTestLLM) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*schedulerTestLLM) NormalizeAssistant(e []llm.Message, m llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(e, m)
}

type schedulerWaitingLLM struct{ calls int }

func (c *schedulerWaitingLLM) StreamChat(context.Context, llm.ChatRequest, llm.StreamHandler) (llm.ChatResult, error) {
	c.calls++
	return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "wait", Type: "function", Function: llm.ToolCallFunction{Name: "write_file", Arguments: `{"path":"handbook/wait.md","content":"blocked","call_purpose":"maintain"}`}}}, FinishReason: "tool_calls"}, nil
}
func (*schedulerWaitingLLM) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*schedulerWaitingLLM) NormalizeAssistant(e []llm.Message, m llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(e, m)
}

func TestDreamingSchedulerWaitingAttemptDoesNotStartAnotherTurn(t *testing.T) {
	root := t.TempDir()
	a, err := autonomy.Open(filepath.Join(root, "autonomy.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.PutProfile(autonomy.Profile{AgentID: "auto-wait", MaxToolRounds: 2, DreamingEnabled: true, DreamingTime: "03:00", Timezone: "UTC"}, 0); err != nil {
		t.Fatal(err)
	}
	agents, err := store.OpenAgents(filepath.Join(root, "agents.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer agents.Close()
	now := time.Now().UTC()
	if err := agents.Save(context.Background(), store.AgentRecord{AgentID: "auto-wait", ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	reg, err := tools.NewRegistry(filepath.Join(root, "workspace"), 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"})); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetHandbookRoot(filepath.Join(root, "handbook")); err != nil {
		t.Fatal(err)
	}
	client := &schedulerWaitingLLM{}
	eng := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"write_file": policy.ModeAlways}})
	mgr := session.NewManager("auto-wait", stream.NewHub(8, logx.Discard()), client, reg, eng, nil, session.TurnOptions{AutoAgent: true}, logx.Discard())
	defer mgr.Stop()
	ensure := func(ctx context.Context, id string) error {
		_, _, e := mgr.CreateWithOptionsAndLLM(id, session.TurnOptions{AutoAgent: true}, reg, nil, client, id)
		return e
	}
	d := NewDreamingScheduler(a, agents, mgr, ensure)
	base := time.Date(2026, 9, 9, 4, 0, 0, 0, time.UTC)
	d.now = func() time.Time { return base }
	if err := d.Tick(context.Background()); err != nil {
		t.Fatalf("initial waiting tick=%v", err)
	}
	if client.calls != 1 {
		t.Fatalf("initial calls=%d, want 1", client.calls)
	}
	d.now = func() time.Time { return base.Add(6 * time.Minute) }
	if err := d.Tick(context.Background()); err != nil {
		t.Fatalf("waiting retry tick=%v", err)
	}
	if client.calls != 1 {
		t.Fatalf("waiting attempt started another turn: calls=%d", client.calls)
	}
	if status := d.Status("auto-wait"); status.State != "waiting" {
		t.Fatalf("waiting attempt changed visible state: %+v", status)
	}
	if status := d.CurrentStatus("auto-wait", base.Add(6*time.Minute)); status.State != "waiting" || !status.NextAt.IsZero() {
		t.Fatalf("approval waiting status exposed stale schedule: %+v", status)
	}
}

type schedulerResumeLLM struct{ calls int }

func (c *schedulerResumeLLM) StreamChat(context.Context, llm.ChatRequest, llm.StreamHandler) (llm.ChatResult, error) {
	c.calls++
	if c.calls == 1 {
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "dream-approve", Type: "function", Function: llm.ToolCallFunction{Name: "write_file", Arguments: `{"path":"handbook/dream.md","content":"ok","call_purpose":"maintain"}`}}}, FinishReason: "tool_calls"}, nil
	}
	return llm.ChatResult{Content: "scheduled experience", FinishReason: "stop"}, nil
}
func (*schedulerResumeLLM) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*schedulerResumeLLM) NormalizeAssistant(e []llm.Message, m llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(e, m)
}

func TestDreamingSchedulerASKResumeFinalizesAndAcksAttempt(t *testing.T) {
	root := t.TempDir()
	a, err := autonomy.Open(filepath.Join(root, "autonomy.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.PutProfile(autonomy.Profile{AgentID: "auto-resume", MaxToolRounds: 2, DreamingEnabled: true, DreamingTime: "03:00", Timezone: "UTC"}, 0); err != nil {
		t.Fatal(err)
	}
	agents, err := store.OpenAgents(filepath.Join(root, "agents.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer agents.Close()
	now := time.Now().UTC()
	if err := agents.Save(context.Background(), store.AgentRecord{AgentID: "auto-resume", ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	reg, err := tools.NewRegistry(filepath.Join(root, "workspace"), 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"})); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetHandbookRoot(filepath.Join(root, "handbook")); err != nil {
		t.Fatal(err)
	}
	client := &schedulerResumeLLM{}
	eng := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"write_file": policy.ModeAlways}})
	mgr := session.NewManager("auto-resume", stream.NewHub(16, logx.Discard()), client, reg, eng, nil, session.TurnOptions{AutoAgent: true}, logx.Discard())
	defer mgr.Stop()
	ensure := func(ctx context.Context, id string) error {
		_, _, e := mgr.CreateWithOptionsAndLLM(id, session.TurnOptions{AutoAgent: true}, reg, nil, client, id)
		return e
	}
	d := NewDreamingScheduler(a, agents, mgr, ensure)
	base := time.Date(2026, 9, 9, 4, 0, 0, 0, time.UTC)
	d.now = func() time.Time { return base }
	if err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	attempt, found, err := mgr.GetDreamingAttempt("auto-resume")
	if err != nil || !found || attempt.State != session.DreamingAttemptWaiting {
		t.Fatalf("waiting attempt=%+v found=%v err=%v", attempt, found, err)
	}
	if _, err := mgr.EnqueueMessage(context.Background(), "auto-resume", "resume", "", nil, map[string]any{"type": "selection", "approved": []string{"dream-approve"}, "rejected": []string{}}, ""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		attempt, found, err = mgr.GetDreamingAttempt("auto-resume")
		if err == nil && found && attempt.State == session.DreamingAttemptCompleted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !found || attempt.State != session.DreamingAttemptCompleted {
		t.Fatalf("resume did not complete attempt=%+v found=%v calls=%d", attempt, found, client.calls)
	}
	if err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if exp, ok := a.GetExperience("auto-resume"); !ok || exp.Revision != 1 {
		t.Fatalf("experience=%+v ok=%v", exp, ok)
	}
	commit, ok := a.GetDreamingCommit("auto-resume", "2026-09-09")
	if !ok || !commit.ResetApplied {
		t.Fatalf("commit=%+v ok=%v", commit, ok)
	}
	if _, found, err := mgr.GetDreamingAttempt("auto-resume"); err != nil || found {
		t.Fatalf("attempt was not acked: found=%v err=%v", found, err)
	}
	calls := client.calls
	if err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.calls != calls {
		t.Fatalf("second tick started another model turn: before=%d after=%d", calls, client.calls)
	}
}

func TestDreamingStatusIsIndependentOfWakeFrequencyAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autonomy.json")
	a, err := autonomy.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 9, 4, 0, 0, 0, time.UTC)
	if err := a.PutProfile(autonomy.Profile{AgentID: "auto-1", WakeIntervalSeconds: 0, MaxToolRounds: 2, DreamingEnabled: true, DreamingTime: "03:00", Timezone: "UTC"}, 0); err != nil {
		t.Fatal(err)
	}
	d := NewDreamingScheduler(a, nil, nil, nil)
	status := d.CurrentStatus("auto-1", now)
	if status.State != "waiting" || status.NextAt.IsZero() {
		t.Fatalf("wake-off dreaming status=%+v", status)
	}
	if _, err := a.CommitDreaming(autonomy.DreamingCommitInput{AgentID: "auto-1", LocalDate: "2026-09-09", CommitID: "dreaming:auto-1:2026-09-09", SessionID: "auto-1", Boundary: "boundary", Content: "learned", Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.MarkDreamingResetApplied("auto-1", "2026-09-09", "dreaming:auto-1:2026-09-09"); err != nil {
		t.Fatal(err)
	}
	status = d.CurrentStatus("auto-1", now)
	if status.State != "succeeded" || status.LastSuccess.IsZero() {
		t.Fatalf("completed dreaming status=%+v", status)
	}
	if status.NextAt.Hour() != 3 || status.NextAt.Day() != 10 {
		t.Fatalf("next dreaming time=%v, want 2026-09-10 03:00 UTC", status.NextAt)
	}
	if err := a.PutProfile(autonomy.Profile{AgentID: "auto-1", WakeIntervalSeconds: 0, MaxToolRounds: 2, DreamingEnabled: false, DreamingTime: "03:00", Timezone: "UTC"}, 1); err != nil {
		t.Fatal(err)
	}
	if status := d.CurrentStatus("auto-1", now); status.State != "disabled" {
		t.Fatalf("disabled dreaming status=%+v", status)
	}
}

func TestDreamingSchedulerRunsRealSessionWhenWakeIsOff(t *testing.T) {
	root := t.TempDir()
	a, err := autonomy.Open(filepath.Join(root, "autonomy.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.PutProfile(autonomy.Profile{AgentID: "auto-real", WakeIntervalSeconds: 0, MaxToolRounds: 2, DreamingEnabled: true, DreamingTime: "03:00", Timezone: "UTC"}, 0); err != nil {
		t.Fatal(err)
	}
	agents, err := store.OpenAgents(filepath.Join(root, "agents.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer agents.Close()
	if err := agents.Save(context.Background(), store.AgentRecord{AgentID: "auto-real", ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	handbook := filepath.Join(root, "handbook")
	reg, err := tools.NewRegistry(filepath.Join(root, "workspace"), 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"})); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetHandbookRoot(handbook); err != nil {
		t.Fatal(err)
	}
	client := &schedulerTestLLM{}
	mgr := session.NewManager("auto-real", stream.NewHub(8, logx.Discard()), client, reg, policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"write_file": policy.ModeNever}}), nil, session.TurnOptions{AutoAgent: true}, logx.Discard())
	defer mgr.Stop()
	d := NewDreamingScheduler(a, agents, mgr, func(ctx context.Context, id string) error {
		_, _, err := mgr.CreateWithOptionsAndLLM(id, session.TurnOptions{AutoAgent: true}, reg, nil, client, id)
		return err
	})
	d.now = func() time.Time { return time.Date(2026, 9, 9, 4, 0, 0, 0, time.UTC) }
	results := make(chan error, 2)
	go func() { results <- d.Tick(context.Background()) }()
	go func() { results <- d.Tick(context.Background()) }()
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if client.calls != 2 {
		t.Fatalf("concurrent initial tick made duplicate model calls=%d", client.calls)
	}
	commit, ok := a.GetDreamingCommit("auto-real", "2026-09-09")
	if !ok || !commit.ResetApplied {
		t.Fatalf("commit=%+v ok=%v", commit, ok)
	}
	if raw, err := os.ReadFile(filepath.Join(handbook, "dream.md")); err != nil || string(raw) != "dreamed" {
		t.Fatalf("handbook=%q err=%v", raw, err)
	}
	if len(client.requests) == 0 {
		t.Fatal("scheduler made no model request")
	}
	var prompt string
	for _, message := range client.requests[0].Messages {
		prompt += message.Role + "\n" + message.Content + "\n"
	}
	for _, want := range []string{"当前工具名称", "当前职责以本次请求的 system prompt 为准", "当前手册根目录", "会被系统持久化", "历史消息中的工具名称、路径和规则可能已经退役"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("dreaming prompt missing %q: %s", want, prompt)
		}
	}
}

func TestDreamingSchedulerRecoversBusyPendingEvenWhenDisabled(t *testing.T) {
	root := t.TempDir()
	a, err := autonomy.Open(filepath.Join(root, "autonomy.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.PutProfile(autonomy.Profile{AgentID: "auto-pending", MaxToolRounds: 2, DreamingEnabled: true, DreamingTime: "03:00", Timezone: "UTC"}, 0); err != nil {
		t.Fatal(err)
	}
	agents, err := store.OpenAgents(filepath.Join(root, "agents.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer agents.Close()
	if err := agents.Save(context.Background(), store.AgentRecord{AgentID: "auto-pending", ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	reg, err := tools.NewRegistry(filepath.Join(root, "workspace"), 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"})); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetHandbookRoot(filepath.Join(root, "handbook")); err != nil {
		t.Fatal(err)
	}
	client := &schedulerTestLLM{}
	mgr := session.NewManager("auto-pending", stream.NewHub(8, logx.Discard()), client, reg, policy.NewDefaultEngine(), nil, session.TurnOptions{AutoAgent: true}, logx.Discard())
	defer mgr.Stop()
	ensure := func(ctx context.Context, id string) error {
		_, _, e := mgr.CreateWithOptionsAndLLM(id, session.TurnOptions{AutoAgent: true}, reg, nil, client, id)
		return e
	}
	if err := ensure(context.Background(), "auto-pending"); err != nil {
		t.Fatal(err)
	}
	d := NewDreamingScheduler(a, agents, mgr, ensure)
	d.now = func() time.Time { return time.Date(2026, 9, 9, 4, 0, 0, 0, time.UTC) }
	busyCtx, release, ok, err := mgr.TryAcquireMaintenanceContext(context.Background(), "auto-pending")
	if err != nil || !ok {
		t.Fatalf("lease: %v", err)
	}
	boundary, err := mgr.CaptureActiveContextBoundary(busyCtx, "auto-pending")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CommitDreaming(autonomy.DreamingCommitInput{AgentID: "auto-pending", LocalDate: "2026-09-09", CommitID: "pending-1", SessionID: "auto-pending", Boundary: boundary, Content: "pending", Now: time.Date(2026, 9, 9, 4, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if err := d.Tick(busyCtx); err != nil {
		t.Fatal(err)
	}
	if client.calls != 0 {
		t.Fatalf("busy pending invoked model: %d", client.calls)
	}
	release()
	if err := a.PutProfile(autonomy.Profile{AgentID: "auto-pending", MaxToolRounds: 2, DreamingEnabled: false, DreamingTime: "03:00", Timezone: "UTC"}, 1); err != nil {
		t.Fatal(err)
	}
	mgr.Stop()
	a2, err := autonomy.Open(filepath.Join(root, "autonomy.json"))
	if err != nil {
		t.Fatal(err)
	}
	client2 := &schedulerTestLLM{}
	mgr2 := session.NewManager("auto-pending", stream.NewHub(8, logx.Discard()), client2, reg, policy.NewDefaultEngine(), nil, session.TurnOptions{AutoAgent: true}, logx.Discard())
	defer mgr2.Stop()
	ensure2 := func(ctx context.Context, id string) error {
		_, _, e := mgr2.CreateWithOptionsAndLLM(id, session.TurnOptions{AutoAgent: true}, reg, nil, client2, id)
		return e
	}
	d2 := NewDreamingScheduler(a2, agents, mgr2, ensure2)
	d2.now = d.now
	if err := d2.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	commit, ok := a2.GetDreamingCommit("auto-pending", "2026-09-09")
	if !ok || !commit.ResetApplied || client2.calls != 0 {
		t.Fatalf("reopened pending recovery=%+v calls=%d", commit, client2.calls)
	}
	exp, ok := a2.GetExperience("auto-pending")
	if !ok {
		t.Fatal("recovered commit lost its experience")
	}
	if err := d2.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	expAgain, ok := a2.GetExperience("auto-pending")
	if !ok || expAgain.Revision != exp.Revision {
		t.Fatalf("replayed commit changed experience revision: before=%+v after=%+v", exp, expAgain)
	}
}

func TestDreamingStatusRouteGuardsNonAutoAgent(t *testing.T) {
	s, _ := autonomyV2TestServer(t)
	s.dreamingSched = NewDreamingScheduler(s.autonomyStore, s.agents, s.sessions, func(context.Context, string) error { return nil })
	call := func(id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/v1/agents/"+id+"/dreaming", nil)
		req.SetPathValue("agent_id", id)
		rec := httptest.NewRecorder()
		s.handleDreamingStatus(rec, req)
		return rec
	}
	if rec := call("normal-v2"); rec.Code != 409 {
		t.Fatalf("normal agent dreaming status code=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := call("auto-v2"); rec.Code != 200 {
		t.Fatalf("auto agent dreaming status code=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := call("missing"); rec.Code != 404 {
		t.Fatalf("missing agent dreaming status code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDreamingSchedulerFailureBackoffThenRetries(t *testing.T) {
	root := t.TempDir()
	a, err := autonomy.Open(filepath.Join(root, "autonomy.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.PutProfile(autonomy.Profile{AgentID: "auto-retry", MaxToolRounds: 2, DreamingEnabled: true, DreamingTime: "03:00", Timezone: "UTC"}, 0); err != nil {
		t.Fatal(err)
	}
	agents, err := store.OpenAgents(filepath.Join(root, "agents.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer agents.Close()
	if err := agents.Save(context.Background(), store.AgentRecord{AgentID: "auto-retry", ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	reg, err := tools.NewRegistry(filepath.Join(root, "workspace"), 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"})); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetHandbookRoot(filepath.Join(root, "handbook")); err != nil {
		t.Fatal(err)
	}
	client := &schedulerTestLLM{}
	mgr := session.NewManager("auto-retry", stream.NewHub(8, logx.Discard()), client, reg, policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"write_file": policy.ModeNever}}), nil, session.TurnOptions{AutoAgent: true}, logx.Discard())
	defer mgr.Stop()
	ensureCalls := 0
	ensure := func(ctx context.Context, id string) error {
		ensureCalls++
		if ensureCalls == 1 {
			return fmt.Errorf("temporary runtime failure")
		}
		_, _, e := mgr.CreateWithOptionsAndLLM(id, session.TurnOptions{AutoAgent: true}, reg, nil, client, id)
		return e
	}
	d := NewDreamingScheduler(a, agents, mgr, ensure)
	base := time.Date(2026, 9, 9, 4, 0, 0, 0, time.UTC)
	d.now = func() time.Time { return base }
	if err := d.Tick(context.Background()); err == nil {
		t.Fatal("first failure unexpectedly succeeded")
	}
	if err := d.Tick(context.Background()); err != nil {
		t.Fatalf("backoff tick=%v", err)
	}
	if client.calls != 0 || ensureCalls != 1 {
		t.Fatalf("backoff did not suppress retry: calls=%d ensure=%d", client.calls, ensureCalls)
	}
	d.now = func() time.Time { return base.Add(6 * time.Minute) }
	if err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.calls != 2 || ensureCalls != 2 {
		t.Fatalf("retry calls=%d ensure=%d", client.calls, ensureCalls)
	}
}
