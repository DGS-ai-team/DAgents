package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

func TestAutonomyTypeBoundaryRejectsNormalAndEmptyPUT(t *testing.T) {
	cfg := testConfig(t)
	as, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer as.Close()
	srv := NewServer(cfg, nil, WithLLM(&goalWakeLLM{}), WithSkipStore())
	defer srv.sessions.Stop()
	srv.agents = as
	for id, raw := range map[string]string{"normal": `{"agent_type":"normal"}`, "empty": `{}`} {
		if err := as.Save(context.Background(), store.AgentRecord{AgentID: id, ConfigSnapshot: json.RawMessage(raw)}); err != nil {
			t.Fatal(err)
		}
		body := []byte(`{"objective":"x","acceptance":"y"}`)
		r := httptest.NewRecorder()
		srv.Handler().ServeHTTP(r, httptest.NewRequest(http.MethodPut, "/v1/agents/"+id+"/autonomy", bytes.NewReader(body)))
		if r.Code != http.StatusConflict {
			t.Fatalf("%s status=%d body=%s", id, r.Code, r.Body.String())
		}
	}
}

func TestAutonomyTypeConversionBlocksResumeWakeWithoutMutation(t *testing.T) {
	cfg := testConfig(t)
	as, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer as.Close()
	fake := &goalWakeLLM{called: make(chan struct{}, 1)}
	srv := NewServer(cfg, nil, WithLLM(fake), WithSkipStore())
	defer srv.sessions.Stop()
	srv.agents = as
	now := time.Now().UTC()
	if err := as.Save(context.Background(), store.AgentRecord{AgentID: "convert-auto", ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	put := httptest.NewRecorder()
	srv.Handler().ServeHTTP(put, httptest.NewRequest(http.MethodPut, "/v1/agents/convert-auto/autonomy", bytes.NewBufferString(`{"objective":"x","acceptance":"y","enabled":false}`)))
	if put.Code != http.StatusOK {
		t.Fatalf("put=%d %s", put.Code, put.Body.String())
	}
	var view struct {
		GoalID string       `json:"goal_id"`
		Status goals.Status `json:"status"`
	}
	if err := json.Unmarshal(put.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	before, ok := srv.goalStore.Get(view.GoalID)
	if !ok || before.Status != goals.StatusPaused {
		t.Fatalf("before=%+v", before)
	}
	patch := httptest.NewRecorder()
	srv.Handler().ServeHTTP(patch, httptest.NewRequest(http.MethodPatch, "/v1/agents/convert-auto", bytes.NewBufferString(`{"agent_type":"normal"}`)))
	if patch.Code != http.StatusOK {
		t.Fatalf("patch=%d %s", patch.Code, patch.Body.String())
	}
	resume := httptest.NewRecorder()
	srv.Handler().ServeHTTP(resume, httptest.NewRequest(http.MethodPost, "/v1/goals/"+view.GoalID+"/resume", nil))
	wake := httptest.NewRecorder()
	srv.Handler().ServeHTTP(wake, httptest.NewRequest(http.MethodPost, "/v1/goals/"+view.GoalID+"/wake", nil))
	if resume.Code != http.StatusConflict || wake.Code != http.StatusConflict {
		t.Fatalf("resume=%d wake=%d", resume.Code, wake.Code)
	}
	after, _ := srv.goalStore.Get(view.GoalID)
	if after.Status != before.Status || after.Runs != before.Runs || len(srv.goalStore.Runs(view.GoalID)) != 0 {
		t.Fatalf("mutated before=%+v after=%+v", before, after)
	}
	select {
	case <-fake.called:
		t.Fatal("wake reached LLM")
	default:
	}
}
