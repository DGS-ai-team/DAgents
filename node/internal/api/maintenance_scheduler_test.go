package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/memory"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

type schedulerBlockingExtractor struct {
	started chan struct{}
	exited  chan struct{}
}

type gatedChatLLM struct {
	llm.Client
	started chan struct{}
	release chan struct{}
}

func (g *gatedChatLLM) StreamChat(ctx context.Context, req llm.ChatRequest, h llm.StreamHandler) (llm.ChatResult, error) {
	close(g.started)
	select {
	case <-g.release:
	case <-ctx.Done():
		return llm.ChatResult{}, ctx.Err()
	}
	return g.Client.StreamChat(ctx, req, h)
}

func (e *schedulerBlockingExtractor) ExtractWithUsage(ctx context.Context, _ memory.ExtractionInput) ([]memory.Candidate, *llm.Usage, error) {
	close(e.started)
	<-ctx.Done()
	close(e.exited)
	return nil, nil, ctx.Err()
}

func newSchedulerFixture(t *testing.T, id string, extractor memory.MaintenanceUsageExtractor, created time.Time) *Server {
	t.Helper()
	cfg := testConfig(t)
	settings, err := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Save(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	settings.Close()
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithMaintenanceExtractor(extractor))
	now := time.Now().UTC()
	if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: id, ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SaveProfile(goals.AutoProfile{AgentID: id, Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 09:00", Timezone: "UTC", MaintenanceTokenBudget: 1000, TotalTokenBudget: 1000}, 0, created); err != nil {
		t.Fatal(err)
	}
	e := turn.NewTurnEventEnvelope("scheduler-fixture", turn.EventTurnCompleted, now)
	e.AgentID, e.TurnID, e.CommandID = id, "turn-fixture", "complete-fixture"
	if _, err := srv.store.AppendTurnEventWithSnapshot(context.Background(), e, []llm.Message{{Role: "user", Content: "snapshot"}}); err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestMaintenanceSchedulerRunOnceProcessesSnapshotsAndDeduplicates(t *testing.T) {
	cfg := testConfig(t)
	settings, err := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Save(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	settings.Close()
	ext := &apiMaintenanceExtractor{}
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithMaintenanceExtractor(ext))
	defer srv.Close()
	now := time.Now().UTC()
	id := "maint-scheduler"
	if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: id, ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SaveProfile(goals.AutoProfile{AgentID: id, Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 09:00", Timezone: "UTC", MaintenanceTokenBudget: 100, TotalTokenBudget: 100}, 0, now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		e := turn.NewTurnEventEnvelope("maintenance-scheduler", turn.EventTurnCompleted, now)
		e.AgentID, e.TurnID, e.CommandID = id, "turn-"+string(rune('0'+i)), "complete-"+string(rune('0'+i))
		if _, err := srv.store.AppendTurnEventWithSnapshot(context.Background(), e, []llm.Message{{Role: "user", Content: "snapshot"}}); err != nil {
			t.Fatal(err)
		}
	}
	srv.maintenanceSched.RunOnceForTest(context.Background(), now)
	if got := ext.calls.Load(); got != 2 {
		t.Fatalf("extract calls=%d", got)
	}
	srv.maintenanceSched.RunOnceForTest(context.Background(), now.Add(time.Minute))
	if got := ext.calls.Load(); got != 2 {
		t.Fatalf("duplicate extract calls=%d", got)
	}
	view, err := srv.goalStore.MaintenanceScheduleStatus(id, now)
	if err != nil || view.Last == nil || view.Last.Status != goals.MaintenanceOccurrenceCompleted {
		t.Fatalf("view=%+v err=%v", view, err)
	}
}

func TestMaintenanceSchedulerStopCancelsBlockingExtractor(t *testing.T) {
	ext := &schedulerBlockingExtractor{started: make(chan struct{}), exited: make(chan struct{})}
	srv := newSchedulerFixture(t, "stop-cancel", ext, time.Now().UTC().Add(-24*time.Hour))
	started := make(chan struct{})
	go func() { srv.maintenanceSched.RunOnceForTest(context.Background(), time.Now().UTC()); close(started) }()
	select {
	case <-ext.started:
	case <-time.After(2 * time.Second):
		t.Fatal("extractor did not start")
	}
	srv.Close()
	select {
	case <-ext.exited:
	case <-time.After(2 * time.Second):
		t.Fatal("extractor not cancelled")
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return")
	}
}

func TestMaintenanceSchedulerDisableCancelsBlockingExtractor(t *testing.T) {
	ext := &schedulerBlockingExtractor{started: make(chan struct{}), exited: make(chan struct{})}
	srv := newSchedulerFixture(t, "disable-cancel", ext, time.Now().UTC().Add(-24*time.Hour))
	go srv.maintenanceSched.RunOnceForTest(context.Background(), time.Now().UTC())
	select {
	case <-ext.started:
	case <-time.After(2 * time.Second):
		t.Fatal("extractor did not start")
	}
	p, _ := srv.goalStore.GetProfile("disable-cancel")
	req := httptest.NewRequest(http.MethodPatch, "/v1/agents/disable-cancel/maintenance", strings.NewReader(`{"expected_revision":`+strconv.FormatInt(p.Revision, 10)+`,"maintenance_enabled":false}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", resp.Code, resp.Body.String())
	}
	select {
	case <-ext.exited:
	case <-time.After(2 * time.Second):
		t.Fatal("extractor not cancelled")
	}
	srv.Close()
}

func TestMaintenanceSchedulerBusyDoesNotClaim(t *testing.T) {
	ext := &apiMaintenanceExtractor{}
	srv := newSchedulerFixture(t, "busy-agent", ext, time.Now().UTC().Add(-24*time.Hour))
	defer srv.Close()
	chat := &gatedChatLLM{Client: &llm.MockClient{}, started: make(chan struct{}), release: make(chan struct{})}
	if _, _, err := srv.sessions.CreateWithOptionsAndLLM("busy-session", srv.sessions.DefaultTurnOptions(), srv.sessions.DefaultTools(), nil, chat, "busy-agent"); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.sessions.EnqueueMessage(context.Background(), "busy-session", "message", "busy", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-chat.started:
	case <-time.After(2 * time.Second):
		t.Fatal("chat did not become active")
	}
	srv.maintenanceSched.RunOnceForTest(context.Background(), time.Now().UTC())
	view, err := srv.goalStore.MaintenanceScheduleStatus("busy-agent", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if view.Last != nil || ext.calls.Load() != 0 {
		t.Fatalf("busy claimed: view=%+v calls=%d", view, ext.calls.Load())
	}
	close(chat.release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pending, active, _, _ := srv.sessions.RuntimeInfo("busy-session")
		if pending == 0 && !active {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	deadline = time.Now().Add(2 * time.Second)
	for ext.calls.Load() == 0 && time.Now().Before(deadline) {
		srv.maintenanceSched.RunOnceForTest(context.Background(), time.Now().UTC())
		time.Sleep(25 * time.Millisecond)
	}
	if ext.calls.Load() == 0 {
		t.Fatalf("maintenance did not run after chat release: %d", ext.calls.Load())
	}
}

func TestMaintenanceSchedulerRestartPendingDoesNotCallExtractor(t *testing.T) {
	now := time.Now().UTC()
	srv := newSchedulerFixture(t, "restart-agent", &apiMaintenanceExtractor{}, now.Add(-24*time.Hour))
	cfg := srv.cfg
	if claim, err := srv.goalStore.ClaimMaintenance("restart-agent", now); err != nil || !claim.Claimed {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	srv.Close()
	ext := &apiMaintenanceExtractor{}
	restarted := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithMaintenanceExtractor(ext))
	defer restarted.Close()
	restarted.maintenanceSched.RunOnceForTest(context.Background(), now.Add(24*time.Hour))
	if ext.calls.Load() != 0 {
		t.Fatalf("restart called extractor=%d", ext.calls.Load())
	}
}

func TestMaintenanceSchedulerAutonomyProfileHTTPChangeCancelsExtractor(t *testing.T) {
	ext := &schedulerBlockingExtractor{started: make(chan struct{}), exited: make(chan struct{})}
	srv := newSchedulerFixture(t, "autonomy-cancel", ext, time.Now().UTC().Add(-24*time.Hour))
	defer srv.Close()
	go srv.maintenanceSched.RunOnceForTest(context.Background(), time.Now().UTC())
	select {
	case <-ext.started:
	case <-time.After(2 * time.Second):
		t.Fatal("extractor did not start")
	}
	p, _ := srv.goalStore.GetProfile("autonomy-cancel")
	body := `{"expected_revision":` + strconv.FormatInt(p.Revision, 10) + `,"profile":{"agent_id":"autonomy-cancel","enabled":true,"maintenance_enabled":true,"maintenance_schedule":"daily 10:00","timezone":"UTC"}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/agents/autonomy-cancel/autonomy", strings.NewReader(body))
	resp := httptest.NewRecorder()
	srv.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("autonomy status=%d body=%s", resp.Code, resp.Body.String())
	}
	select {
	case <-ext.exited:
	case <-time.After(2 * time.Second):
		t.Fatal("extractor not cancelled")
	}
}

func TestMaintenanceSchedulerAutonomyDisableHTTPCancelsExtractor(t *testing.T) {
	ext := &schedulerBlockingExtractor{started: make(chan struct{}), exited: make(chan struct{})}
	srv := newSchedulerFixture(t, "autonomy-disable", ext, time.Now().UTC().Add(-24*time.Hour))
	defer srv.Close()
	go srv.maintenanceSched.RunOnceForTest(context.Background(), time.Now().UTC())
	select {
	case <-ext.started:
	case <-time.After(2 * time.Second):
		t.Fatal("extractor did not start")
	}
	p, _ := srv.goalStore.GetProfile("autonomy-disable")
	body := `{"expected_revision":` + strconv.FormatInt(p.Revision, 10) + `,"profile":{"agent_id":"autonomy-disable","enabled":true,"maintenance_enabled":false,"maintenance_schedule":"daily 09:00","timezone":"UTC"}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/agents/autonomy-disable/autonomy", strings.NewReader(body))
	resp := httptest.NewRecorder()
	srv.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("autonomy disable status=%d body=%s", resp.Code, resp.Body.String())
	}
	select {
	case <-ext.exited:
	case <-time.After(2 * time.Second):
		t.Fatal("extractor not cancelled")
	}
}

func TestMaintenanceSchedulerAgentTypeChangeHTTPCancelsExtractor(t *testing.T) {
	ext := &schedulerBlockingExtractor{started: make(chan struct{}), exited: make(chan struct{})}
	srv := newSchedulerFixture(t, "type-cancel", ext, time.Now().UTC().Add(-24*time.Hour))
	defer srv.Close()
	go srv.maintenanceSched.RunOnceForTest(context.Background(), time.Now().UTC())
	select {
	case <-ext.started:
	case <-time.After(2 * time.Second):
		t.Fatal("extractor did not start")
	}
	req := httptest.NewRequest(http.MethodPatch, "/v1/agents/type-cancel", strings.NewReader(`{"agent_type":"normal"}`))
	resp := httptest.NewRecorder()
	srv.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("type status=%d body=%s", resp.Code, resp.Body.String())
	}
	select {
	case <-ext.exited:
	case <-time.After(2 * time.Second):
		t.Fatal("extractor not cancelled")
	}
}

func TestMaintenanceSchedulerContinuesBoundedSnapshotBacklog(t *testing.T) {
	ext := &apiMaintenanceExtractor{}
	now := time.Now().UTC()
	srv := newSchedulerFixture(t, "backlog-agent", ext, now.Add(-24*time.Hour))
	defer srv.Close()
	for i := 2; i <= 9; i++ {
		e := turn.NewTurnEventEnvelope("backlog-session", turn.EventTurnCompleted, now)
		e.AgentID, e.TurnID, e.CommandID = "backlog-agent", "turn-"+string(rune('0'+i)), "complete-"+string(rune('0'+i))
		if _, err := srv.store.AppendTurnEventWithSnapshot(context.Background(), e, []llm.Message{{Role: "user", Content: "snapshot"}}); err != nil {
			t.Fatal(err)
		}
	}
	srv.maintenanceSched.RunOnceForTest(context.Background(), now)
	if got := ext.calls.Load(); got != 8 {
		t.Fatalf("first batch calls=%d", got)
	}
	view, _ := srv.goalStore.MaintenanceScheduleStatus("backlog-agent", now)
	if view.Last == nil || view.Last.Status != goals.MaintenanceOccurrencePending {
		t.Fatalf("first status=%+v", view.Last)
	}
	srv.maintenanceSched.RunOnceForTest(context.Background(), now.Add(time.Minute))
	if got := ext.calls.Load(); got != 9 {
		t.Fatalf("continued calls=%d", got)
	}
	view, _ = srv.goalStore.MaintenanceScheduleStatus("backlog-agent", now.Add(time.Minute))
	if view.Last == nil || view.Last.Status != goals.MaintenanceOccurrenceCompleted {
		t.Fatalf("final status=%+v", view.Last)
	}
}
