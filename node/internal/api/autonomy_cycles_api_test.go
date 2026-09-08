package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

func cycleRequest(t *testing.T, srv *Server, method, path string, v any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(v)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewReader(b)))
	return w
}

func TestAutonomyCyclesHTTPTerminalRotationAndHistory(t *testing.T) {
	srv, _ := autonomyRegressionServer(t)
	put := putAutonomy(t, srv, map[string]any{"objective": "first", "acceptance": "proof", "enabled": false})
	if put.Code != http.StatusOK {
		t.Fatalf("initial PUT=%d %s", put.Code, put.Body.String())
	}
	p, ok := srv.goalStore.GetProfile("auto-reg")
	if !ok {
		t.Fatal("profile missing")
	}
	post := cycleRequest(t, srv, http.MethodPost, "/v1/agents/auto-reg/autonomy/cycles", map[string]any{"idempotency_key": "cycle-2", "expected_profile_revision": p.Revision, "objective": "second", "acceptance": "proof", "enabled": false})
	if post.Code != http.StatusConflict {
		t.Fatalf("active cycle should conflict=%d %s", post.Code, post.Body.String())
	}
	firstID := p.CurrentGoalID
	if _, err := srv.goalStore.SetStatus(firstID, goals.StatusCompleted, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	p, _ = srv.goalStore.GetProfile("auto-reg")
	post = cycleRequest(t, srv, http.MethodPost, "/v1/agents/auto-reg/autonomy/cycles", map[string]any{"idempotency_key": "cycle-2", "expected_profile_revision": p.Revision, "objective": "second", "acceptance": "proof", "enabled": false})
	if post.Code != http.StatusCreated {
		t.Fatalf("rotation=%d %s", post.Code, post.Body.String())
	}
	var g goals.Goal
	if err := json.Unmarshal(post.Body.Bytes(), &g); err != nil {
		t.Fatal(err)
	}
	if g.ID == firstID || g.CycleSequence < 2 {
		t.Fatalf("bad new cycle=%+v", g)
	}
	list := cycleRequest(t, srv, http.MethodGet, "/v1/agents/auto-reg/autonomy/cycles?page=1&page_size=100", nil)
	if list.Code != http.StatusOK || !bytes.Contains(list.Body.Bytes(), []byte(firstID)) {
		t.Fatalf("history=%d %s", list.Code, list.Body.String())
	}
}

func TestAutonomyCyclesHTTPConcurrentSameIdempotency(t *testing.T) {
	srv, _ := autonomyRegressionServer(t)
	put := putAutonomy(t, srv, map[string]any{"objective": "first", "acceptance": "proof", "enabled": false})
	if put.Code != http.StatusOK {
		t.Fatalf("PUT=%d", put.Code)
	}
	if _, err := srv.goalStore.SetStatus(srv.goalStore.List()[0].ID, goals.StatusCompleted, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	p, _ := srv.goalStore.GetProfile("auto-reg")
	body := map[string]any{"idempotency_key": "same", "expected_profile_revision": p.Revision, "objective": "next", "acceptance": "proof", "enabled": false}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- cycleRequest(t, srv, http.MethodPost, "/v1/agents/auto-reg/autonomy/cycles", body).Code
		}()
	}
	wg.Wait()
	close(codes)
	for c := range codes {
		if c != http.StatusCreated && c != http.StatusConflict {
			t.Fatalf("unexpected status=%d", c)
		}
	}
	count := 0
	for _, g := range srv.goalStore.List() {
		if g.Managed && g.IdempotencyKey == "same" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("cycles=%d", count)
	}
}

func TestAutonomyCyclesHTTPNormalRejected(t *testing.T) {
	srv, as := autonomyRegressionServer(t)
	now := time.Now().UTC()
	if err := as.Save(context.Background(), store.AgentRecord{AgentID: "normal-reg", DisplayName: "normal", ConfigSnapshot: json.RawMessage(`{"agent_type":"normal"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	w := cycleRequest(t, srv, http.MethodPost, "/v1/agents/normal-reg/autonomy/cycles", map[string]any{"idempotency_key": "x", "expected_profile_revision": 0, "objective": "x", "acceptance": "y"})
	if w.Code != http.StatusConflict {
		t.Fatalf("normal status=%d", w.Code)
	}
}

func TestAutonomyCyclesHTTPEmptyHistoryHugePageAndMissingProfile(t *testing.T) {
	srv, _ := autonomyRegressionServer(t)
	w := cycleRequest(t, srv, http.MethodGet, "/v1/agents/auto-reg/autonomy/cycles?page=9223372036854775807&page_size=100", nil)
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(`"total":0`)) {
		t.Fatalf("empty page=%d %s", w.Code, w.Body.String())
	}
	w = cycleRequest(t, srv, http.MethodPost, "/v1/agents/auto-reg/autonomy/cycles", map[string]any{"idempotency_key": "no-profile", "expected_profile_revision": 0, "objective": "x", "acceptance": "y"})
	if w.Code != http.StatusConflict {
		t.Fatalf("missing profile=%d %s", w.Code, w.Body.String())
	}
	if _, ok := srv.goalStore.GetProfile("auto-reg"); ok {
		t.Fatal("missing profile request created profile")
	}
}

func TestAutonomyCyclesHTTPIdempotencyIncludesEnabledAndPausedReplay(t *testing.T) {
	srv, _ := autonomyRegressionServer(t)
	if putAutonomy(t, srv, map[string]any{"objective": "first", "acceptance": "proof", "enabled": false}).Code != http.StatusOK {
		t.Fatal("initial PUT failed")
	}
	if _, err := srv.goalStore.SetStatus(srv.goalStore.List()[0].ID, goals.StatusCompleted, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	p, _ := srv.goalStore.GetProfile("auto-reg")
	v := map[string]any{"idempotency_key": "enabled-key", "expected_profile_revision": p.Revision, "objective": "next", "acceptance": "proof", "enabled": false}
	w := cycleRequest(t, srv, http.MethodPost, "/v1/agents/auto-reg/autonomy/cycles", v)
	if w.Code != http.StatusCreated {
		t.Fatalf("first=%d %s", w.Code, w.Body.String())
	}
	w2 := cycleRequest(t, srv, http.MethodPost, "/v1/agents/auto-reg/autonomy/cycles", v)
	if w2.Code != http.StatusCreated || w2.Body.String() != w.Body.String() {
		t.Fatalf("paused replay first=%d second=%d", w.Code, w2.Code)
	}
	v["enabled"] = true
	w = cycleRequest(t, srv, http.MethodPost, "/v1/agents/auto-reg/autonomy/cycles", v)
	if w.Code != http.StatusConflict {
		t.Fatalf("enabled conflict=%d %s", w.Code, w.Body.String())
	}
}

func TestAutonomyCyclesHTTPProvisionDiskFailureRetryKeepsResources(t *testing.T) {
	srv, _ := autonomyRegressionServer(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "goals.json")
	st, err := goals.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	srv.goalStore = st
	now := time.Now().UTC()
	p, err := st.SaveProfile(goals.AutoProfile{AgentID: "auto-reg", PlanMode: "one_shot", Enabled: true}, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	req := map[string]any{"idempotency_key": "disk-retry", "expected_profile_revision": p.Revision, "objective": "retry", "acceptance": "proof", "enabled": true}
	w := cycleRequest(t, srv, http.MethodPost, "/v1/agents/auto-reg/autonomy/cycles", req)
	if w.Code != http.StatusCreated {
		t.Fatalf("initial provision=%d %s", w.Code, w.Body.String())
	}
	var g goals.Goal
	if err := json.Unmarshal(w.Body.Bytes(), &g); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetProvisionStatus(g.ID, "failed", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	backup := path + ".bak"
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	w = cycleRequest(t, srv, http.MethodPost, "/v1/agents/auto-reg/autonomy/cycles", req)
	if w.Code != http.StatusConflict {
		t.Fatalf("failed provision=%d %s", w.Code, w.Body.String())
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, path); err != nil {
		t.Fatal(err)
	}
	w = cycleRequest(t, srv, http.MethodPost, "/v1/agents/auto-reg/autonomy/cycles", req)
	if w.Code != http.StatusCreated {
		t.Fatalf("retry=%d %s", w.Code, w.Body.String())
	}
	got, _ := st.Get(g.ID)
	if got.ProvisionStatus != "ready" || got.TriggerID == "" || got.SessionID == "" {
		t.Fatalf("provision=%+v", got)
	}
	count := 0
	for _, tr := range srv.triggerStore.ListTriggers() {
		if tr.ManagedGoalID == g.ID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("managed triggers=%d", count)
	}
}
