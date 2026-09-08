package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

func TestGoalColdRestartKeepsDedicatedRuntime(t *testing.T) {
	cfg := testConfig(t)
	cfg.Onboarding.NodeProfileCompleted = true
	first := NewServer(cfg, nil)
	defer first.Close()
	as := first.agents
	if as == nil {
		t.Fatal("agent store unavailable")
	}
	rec := store.AgentRecord{AgentID: "restart-agent", DisplayName: "restart agent", ConfigSnapshot: []byte(`{"agent_type":"auto","workspace":{"mode":"private"}}`), RuntimeRevision: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := as.Save(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	setup := doGoalRequest(first, http.MethodPatch, "/v1/setup/config", []byte(`{"agent":{"name":"restart-agent","description":"restart"},"user":{"preferred_name":"QA"},"onboarding":{"node_profile_completed":true}}`))
	if setup.Code != http.StatusOK {
		t.Fatalf("setup=%d %s", setup.Code, setup.Body.String())
	}
	body, _ := json.Marshal(map[string]any{"objective": "idle restart goal", "acceptance": "run finishes", "agent_id": rec.AgentID, "enabled": true})
	resp := createGoalViaAutonomy(first, rec.AgentID, body)
	if resp.Code != http.StatusOK {
		t.Fatalf("create=%d %s", resp.Code, resp.Body.String())
	}
	var created struct {
		ID        string `json:"id"`
		SessionID string `json:"session_id"`
		AgentID   string `json:"agent_id"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil || created.ID == "" || created.SessionID == "" || created.AgentID != rec.AgentID {
		t.Fatalf("goal=%s", resp.Body.String())
	}
	if created.SessionID == rec.AgentID {
		t.Fatal("goal reused default agent session")
	}
	first.Close()

	fake := &goalWakeLLM{called: make(chan struct{}, 1), release: make(chan struct{})}
	second := NewServer(cfg, nil, WithLLM(fake), WithSkipStore())
	defer second.Close()
	if second.agents == nil {
		var openErr error
		second.agents, openErr = store.OpenAgents(cfg.AgentsDBPath())
		if openErr != nil {
			t.Fatal(openErr)
		}
	}
	wake := doGoalRequest(second, http.MethodPost, "/v1/goals/"+created.ID+"/wake", nil)
	if wake.Code != http.StatusAccepted {
		t.Fatalf("wake=%d %s", wake.Code, wake.Body.String())
	}
	hydrate := doGoalRequest(second, http.MethodGet, "/v1/agents/"+created.SessionID+"/hydrate", nil)
	if hydrate.Code != http.StatusOK {
		t.Fatalf("dedicated hydrate=%d %s", hydrate.Code, hydrate.Body.String())
	}
	select {
	case <-fake.called:
	case <-time.After(5 * time.Second):
		t.Fatal("restarted goal did not call model")
	}
	close(fake.release)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runs := second.goalStore.Runs(created.ID)
		if len(runs) == 1 && runs[0].FinishedAt != nil {
			if runs[0].TurnID == "" || runs[0].TokensUsed <= 0 {
				t.Fatalf("run=%+v", runs[0])
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("run did not finish: %+v", second.goalStore.Runs(created.ID))
}

func TestGoalColdRestartArchivedAgentNeverFallsBack(t *testing.T) {
	t.Run("archived", func(t *testing.T) {
		cfg := testConfig(t)
		cfg.Onboarding.NodeProfileCompleted = true
		first := NewServer(cfg, nil)
		defer first.Close()
		as := first.agents
		if as == nil {
			t.Fatal("agent store unavailable")
		}
		rec := store.AgentRecord{AgentID: "restart-invalid-agent", DisplayName: "invalid", ConfigSnapshot: []byte(`{"agent_type":"auto","workspace":{"mode":"private"}}`), RuntimeRevision: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := as.Save(context.Background(), rec); err != nil {
			t.Fatal(err)
		}
		setup := doGoalRequest(first, http.MethodPatch, "/v1/setup/config", []byte(`{"agent":{"name":"invalid","description":"restart"},"user":{"preferred_name":"QA"},"onboarding":{"node_profile_completed":true}}`))
		if setup.Code != http.StatusOK {
			t.Fatalf("setup=%d", setup.Code)
		}
		body, _ := json.Marshal(map[string]any{"objective": "invalid restart goal", "acceptance": "never runs", "agent_id": rec.AgentID, "enabled": true})
		resp := createGoalViaAutonomy(first, rec.AgentID, body)
		if resp.Code != http.StatusOK {
			t.Fatalf("create=%d", resp.Code)
		}
		var created struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil || created.ID == "" {
			t.Fatal("missing goal")
		}
		if err := as.SoftDelete(context.Background(), rec.AgentID); err != nil {
			t.Fatal(err)
		}
		first.Close()
		fake := &goalWakeLLM{called: make(chan struct{}, 1), release: make(chan struct{})}
		second := NewServer(cfg, nil, WithLLM(fake))
		defer second.Close()
		wake := doGoalRequest(second, http.MethodPost, "/v1/goals/"+created.ID+"/wake", nil)
		if wake.Code == http.StatusAccepted {
			time.Sleep(500 * time.Millisecond)
		}
		select {
		case <-fake.called:
			t.Fatal("invalid agent fell back to default LLM")
		default:
		}
	})
}
