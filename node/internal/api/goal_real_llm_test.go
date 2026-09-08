package api

// Opt-in end-to-end harness for one real-model managed Goal batch.
import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRealLLMGoalBatchEndToEnd(t *testing.T) {
	if os.Getenv("DAGENTS_REAL_GOAL_QA") != "1" {
		t.Skip("set DAGENTS_REAL_GOAL_QA=1 to run")
	}
	profileRoot := strings.TrimSpace(os.Getenv("DAGENTS_REAL_QA_PROFILE_ROOT"))
	if !filepath.IsAbs(profileRoot) {
		t.Fatal("DAGENTS_REAL_QA_PROFILE_ROOT must be absolute")
	}
	dbPath := filepath.Join(profileRoot, "llm_configs.db")
	if _, e := os.Stat(dbPath); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(dbPath + "-wal"); e == nil {
		t.Fatal("profile database has an active WAL; provide a quiescent profile root")
	}
	tmp := t.TempDir()
	t.Logf("isolated QA root: %s", tmp)
	raw, e := os.ReadFile(dbPath)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(tmp, "llm_configs.db"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	secretCopied := false
	for _, name := range []string{".llm_secret_key", "llm_secret_key"} {
		if b, e := os.ReadFile(filepath.Join(profileRoot, name)); e == nil {
			_ = os.WriteFile(filepath.Join(tmp, name), b, 0600)
			secretCopied = true
		}
	}
	if !secretCopied {
		t.Fatal("no LLM secret key found in profile root")
	}
	profiles, e := store.OpenLLMConfigs(filepath.Join(tmp, "llm_configs.db"), tmp)
	if e != nil {
		t.Fatal(e)
	}
	defer profiles.Close()
	recs, e := profiles.List(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	var chosen store.LLMConfigRecord
	var key string
	for _, r := range recs {
		if r.Mock || !r.HasAPIKey() {
			continue
		}
		key, e = profiles.DecryptAPIKey(r)
		if e == nil && key != "" {
			chosen = r
			break
		}
	}
	if chosen.ID == "" {
		t.Fatal("no decryptable non-mock profile")
	}
	t.Setenv("OPENAI_API_KEY", key)
	cfg := testConfig(t)
	cfg.NodeID = "real-goal-qa"
	cfg.RuntimeRoot = tmp
	cfg.Onboarding.NodeProfileCompleted = true
	cfg.LLM.Provider = chosen.Provider
	cfg.LLM.BaseURL = chosen.BaseURL
	cfg.LLM.Model = chosen.Model
	cfg.LLM.APIKeyEnv = "OPENAI_API_KEY"
	cfg.LLM.Mock = false
	qaPolicy := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{
		"read_file":       policy.ModeNever,
		"write_file":      policy.ModeNever,
		"goal_checkpoint": policy.ModeNever,
	}})
	srv := NewServer(cfg, nil, WithPolicy(qaPolicy))
	defer srv.Close()
	rec := store.AgentRecord{AgentID: "real-goal-agent", DisplayName: "real goal", ConfigSnapshot: json.RawMessage(`{"agent_type":"auto","workspace":{"mode":"private"},"defaults":{"tools":{"enabled_groups":["fs"]},"llm":{"active":"PROFILE_PLACEHOLDER","max_steps":32}}}`), RuntimeRevision: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	rec.ConfigSnapshot = json.RawMessage(strings.ReplaceAll(string(rec.ConfigSnapshot), "PROFILE_PLACEHOLDER", chosen.ID))
	if srv.agents == nil {
		t.Fatal("agent store unavailable")
	}
	if e = srv.agents.Save(context.Background(), rec); e != nil {
		t.Fatal(e)
	}
	if e = srv.agents.SaveAgentPolicy(context.Background(), store.AgentPolicyRecord{
		AgentID: rec.AgentID,
		Tools: map[string]string{
			"read_file": "never", "write_file": "never", "goal_checkpoint": "never",
		},
	}); e != nil {
		t.Fatal(e)
	}
	setupBody := []byte(`{"agent":{"name":"real-goal-qa","description":"isolated QA"},"user":{"preferred_name":"QA"},"onboarding":{"node_profile_completed":true}}`)
	setupResp := doGoalRequest(srv, http.MethodPatch, "/v1/setup/config", setupBody)
	if setupResp.Code != http.StatusOK {
		t.Fatalf("onboarding setup=%d %s", setupResp.Code, setupResp.Body.String())
	}
	workspace := filepath.Join(tmp, "agents", rec.AgentID, "workspace")
	payload, _ := json.Marshal(map[string]any{"objective": "Complete this batch in exactly three tool calls: read items.json once, write evidence.json once with exact text SUM=6, then call goal_checkpoint once referencing evidence.json. Do not reread or use any other tool.", "acceptance": "A checkpoint and evidence.json containing SUM=6 exist", "agent_id": rec.AgentID, "enabled": true, "max_runs": 1, "token_budget": 20000, "turn_token_budget": 15000})
	rr := createGoalViaAutonomy(srv, rec.AgentID, payload)
	if rr.Code != http.StatusOK {
		t.Fatalf("create=%d %s", rr.Code, rr.Body.String())
	}
	var g struct {
		ID        string `json:"id"`
		SessionID string `json:"session_id"`
	}
	if json.Unmarshal(rr.Body.Bytes(), &g) != nil || g.ID == "" {
		t.Fatal("missing goal id")
	}
	if root, ok := srv.sessions.SessionWorkspaceRoot(g.SessionID); ok {
		workspace = root
	} else {
		t.Fatal("goal session workspace unavailable")
	}
	if e := os.MkdirAll(workspace, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(workspace, "items.json"), []byte(`{"items":[1,2,3],"expected_sum":6}`), 0600); e != nil {
		t.Fatal(e)
	}
	wake := doGoalRequest(srv, http.MethodPost, "/v1/goals/"+g.ID+"/wake", nil)
	if wake.Code != 202 {
		t.Fatalf("wake=%d %s", wake.Code, wake.Body.String())
	}
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		got, _ := srv.goalStore.Get(g.ID)
		if got.LastCheckpoint != nil && len(got.LastCheckpoint.Evidence) > 0 {
			runs := srv.goalStore.Runs(g.ID)
			if len(runs) == 0 || runs[0].FinishedAt == nil {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			if runs[0].TurnID == "" || runs[0].Status != "completed" || runs[0].TokensUsed <= 0 {
				t.Fatalf("run terminal facts invalid: %+v", runs[0])
			}
			data, e := os.ReadFile(filepath.Join(workspace, "evidence.json"))
			if e != nil || !strings.Contains(string(data), "SUM=6") {
				t.Fatalf("evidence missing: %v %q", e, string(data))
			}
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("real goal did not produce checkpoint")
}

func doGoalRequest(s *Server, method, path string, body []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

// realGoalServer creates the same isolated, explicitly approved test runtime
// as the batch test. The returned directory is disposable and contains no
// production databases.
func realGoalServer(t *testing.T, agentID string) (*Server, store.AgentRecord, store.LLMConfigRecord) {
	t.Helper()
	profileRoot := strings.TrimSpace(os.Getenv("DAGENTS_REAL_QA_PROFILE_ROOT"))
	if !filepath.IsAbs(profileRoot) {
		t.Fatal("DAGENTS_REAL_QA_PROFILE_ROOT must be absolute")
	}
	dbPath := filepath.Join(profileRoot, "llm_configs.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dbPath + "-wal"); err == nil {
		t.Fatal("profile database has an active WAL")
	}
	tmp := t.TempDir()
	raw, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(tmp, "llm_configs.db"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	secretCopied := false
	for _, name := range []string{".llm_secret_key", "llm_secret_key"} {
		b, e := os.ReadFile(filepath.Join(profileRoot, name))
		if e == nil {
			if e = os.WriteFile(filepath.Join(tmp, name), b, 0600); e != nil {
				t.Fatal(e)
			}
			secretCopied = true
		}
	}
	if !secretCopied {
		t.Fatal("no LLM secret key found")
	}
	profiles, err := store.OpenLLMConfigs(filepath.Join(tmp, "llm_configs.db"), tmp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = profiles.Close() })
	// Remove copied credentials and configuration after the isolated server
	// closes; leave only non-sensitive goal artifacts for post-test inspection.
	t.Cleanup(func() {
		for _, name := range []string{"llm_configs.db", ".llm_secret_key", "llm_secret_key", "agents.db", "node_settings.db"} {
			_ = os.Remove(filepath.Join(tmp, name))
		}
	})
	recs, err := profiles.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var chosen store.LLMConfigRecord
	var key string
	for _, r := range recs {
		if r.Mock || !r.HasAPIKey() {
			continue
		}
		key, err = profiles.DecryptAPIKey(r)
		if err == nil && key != "" {
			chosen = r
			break
		}
	}
	if chosen.ID == "" {
		t.Fatal("no decryptable non-mock profile")
	}
	t.Setenv("OPENAI_API_KEY", key)
	cfg := testConfig(t)
	cfg.NodeID = "real-goal-qa"
	cfg.RuntimeRoot = tmp
	cfg.Onboarding.NodeProfileCompleted = true
	cfg.LLM.Provider, cfg.LLM.BaseURL, cfg.LLM.Model, cfg.LLM.APIKeyEnv, cfg.LLM.Mock = chosen.Provider, chosen.BaseURL, chosen.Model, "OPENAI_API_KEY", false
	qaPolicy := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"read_file": policy.ModeNever, "write_file": policy.ModeNever, "goal_checkpoint": policy.ModeNever}})
	srv := NewServer(cfg, nil, WithPolicy(qaPolicy))
	t.Cleanup(func() { srv.Close() })
	rec := store.AgentRecord{AgentID: agentID, DisplayName: agentID, ConfigSnapshot: json.RawMessage(strings.ReplaceAll(`{"agent_type":"auto","workspace":{"mode":"private"},"defaults":{"tools":{"enabled_groups":["fs"]},"llm":{"active":"PROFILE_PLACEHOLDER","max_steps":32}}}`, "PROFILE_PLACEHOLDER", chosen.ID)), RuntimeRevision: 1, UpdatedAt: time.Now(), CreatedAt: time.Now()}
	if srv.agents == nil {
		t.Fatal("agent store unavailable")
	}
	if err = srv.agents.Save(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	if err = srv.agents.SaveAgentPolicy(context.Background(), store.AgentPolicyRecord{AgentID: agentID, Tools: map[string]string{"read_file": "never", "write_file": "never", "goal_checkpoint": "never"}}); err != nil {
		t.Fatal(err)
	}
	setup := doGoalRequest(srv, http.MethodPatch, "/v1/setup/config", []byte(`{"agent":{"name":"real-goal-qa","description":"isolated QA"},"user":{"preferred_name":"QA"},"onboarding":{"node_profile_completed":true}}`))
	if setup.Code != http.StatusOK {
		t.Fatalf("onboarding setup=%d %s", setup.Code, setup.Body.String())
	}
	return srv, rec, chosen
}

func waitRealRun(t *testing.T, srv *Server, goalID string, n int) goals.Run {
	t.Helper()
	deadline := time.Now().Add(150 * time.Second)
	for time.Now().Before(deadline) {
		rs := srv.goalStore.Runs(goalID)
		if len(rs) >= n && rs[n-1].FinishedAt != nil {
			return rs[n-1]
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("run %d did not finish; runs=%+v", n, srv.goalStore.Runs(goalID))
	return goals.Run{}
}

func TestRealLLMGoalObserveVersionedTwoTicks(t *testing.T) {
	if os.Getenv("DAGENTS_REAL_GOAL_QA") != "1" {
		t.Skip("set DAGENTS_REAL_GOAL_QA=1 to run")
	}
	srv, rec, _ := realGoalServer(t, "real-observe-agent")
	payload, _ := json.Marshal(map[string]any{"objective": "Use exactly read_file(state.json), write_file(evidence.json), goal_checkpoint. Never use glob/search or any other tool. First run: read version 1, write exactly version=1, checkpoint done=false with next wake. Second run after state changes: read version 2, overwrite exactly version=2, checkpoint done=true.", "acceptance": "evidence.json changes from version=1 to version=2", "agent_id": rec.AgentID, "enabled": true, "max_runs": 2, "token_budget": 60000, "turn_token_budget": 32000, "min_wake_interval_seconds": 60})
	r := createGoalViaAutonomy(srv, rec.AgentID, payload)
	if r.Code != http.StatusOK {
		t.Fatalf("create=%d %s", r.Code, r.Body.String())
	}
	var g struct {
		ID        string `json:"id"`
		SessionID string `json:"session_id"`
		TriggerID string `json:"trigger_id"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &g); err != nil || g.ID == "" || g.SessionID == "" || g.TriggerID == "" {
		t.Fatalf("bad goal: %s", r.Body.String())
	}
	root, ok := srv.sessions.SessionWorkspaceRoot(g.SessionID)
	if !ok || !filepath.IsAbs(root) {
		t.Fatal("goal session workspace unavailable")
	}
	rel, err := filepath.Rel(srv.cfg.RuntimeRoot, root)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("workspace escaped isolated root: %s", root)
	}
	if err := os.WriteFile(filepath.Join(root, "state.json"), []byte(`{"version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if x := doGoalRequest(srv, http.MethodPost, "/v1/goals/"+g.ID+"/wake", nil); x.Code != 202 {
		t.Fatalf("wake=%d", x.Code)
	}
	first := waitRealRun(t, srv, g.ID, 1)
	if first.TurnID == "" || first.TokensUsed <= 0 || first.Status != "completed" || first.Checkpoint == nil || first.Checkpoint.Done {
		t.Fatalf("first run=%+v", first)
	}
	firstEvidence, err := os.ReadFile(filepath.Join(root, "evidence.json"))
	if err != nil || !strings.Contains(string(firstEvidence), `"version":1`) {
		t.Fatalf("first evidence=%q err=%v", firstEvidence, err)
	}
	if err := os.WriteFile(filepath.Join(root, "state.json"), []byte(`{"version":2}`), 0600); err != nil {
		t.Fatal(err)
	}
	tr, found := srv.triggerStore.GetTrigger(g.TriggerID)
	if !found || tr == nil {
		t.Fatal("managed trigger unavailable")
	}
	if tr.NextFireAt == nil {
		t.Fatal("missing next fire")
	}
	srv.triggerSched.RunOnceForTest(context.Background(), time.Unix(int64(*tr.NextFireAt), 0).Add(time.Second))
	second := waitRealRun(t, srv, g.ID, 2)
	if first.Status != "completed" || second.TurnID == "" || second.TurnID == first.TurnID || second.TokensUsed <= 0 || second.Status != "completed" || second.Checkpoint == nil || !second.Checkpoint.Done {
		t.Fatalf("second run=%+v", second)
	}
	if goal, ok := srv.goalStore.Get(g.ID); !ok || goal.Status != goals.StatusCompleted {
		t.Fatalf("goal status=%v ok=%v", goal.Status, ok)
	}
	b, err := os.ReadFile(filepath.Join(root, "evidence.json"))
	if err != nil || !strings.Contains(string(b), `"version":2`) {
		t.Fatalf("evidence=%q err=%v", b, err)
	}
}

func TestRealLLMGoalConditionFollowupTwoTicks(t *testing.T) {
	if os.Getenv("DAGENTS_REAL_GOAL_QA") != "1" {
		t.Skip("set DAGENTS_REAL_GOAL_QA=1 to run")
	}
	srv, rec, _ := realGoalServer(t, "real-condition-agent")
	payload, _ := json.Marshal(map[string]any{"objective": "Use exactly read_file(ready.json), optionally write_file(evidence.json), and goal_checkpoint. Never use glob/search or any other tool. If ready=false, checkpoint done=false with next wake. When ready=true, write exactly READY and checkpoint done=true.", "acceptance": "ready false waits, then evidence.json contains READY", "agent_id": rec.AgentID, "enabled": true, "max_runs": 2, "token_budget": 60000, "turn_token_budget": 32000, "min_wake_interval_seconds": 60})
	r := createGoalViaAutonomy(srv, rec.AgentID, payload)
	if r.Code != http.StatusOK {
		t.Fatalf("create=%d %s", r.Code, r.Body.String())
	}
	var g struct {
		ID        string `json:"id"`
		SessionID string `json:"session_id"`
		TriggerID string `json:"trigger_id"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &g); err != nil || g.ID == "" || g.SessionID == "" || g.TriggerID == "" {
		t.Fatalf("bad goal: %s", r.Body.String())
	}
	root, ok := srv.sessions.SessionWorkspaceRoot(g.SessionID)
	if !ok || !filepath.IsAbs(root) {
		t.Fatal("goal session workspace unavailable")
	}
	rel, err := filepath.Rel(srv.cfg.RuntimeRoot, root)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("workspace escaped isolated root: %s", root)
	}
	if err := os.WriteFile(filepath.Join(root, "ready.json"), []byte(`{"ready":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	if x := doGoalRequest(srv, http.MethodPost, "/v1/goals/"+g.ID+"/wake", nil); x.Code != 202 {
		t.Fatalf("wake=%d", x.Code)
	}
	first := waitRealRun(t, srv, g.ID, 1)
	if first.Status != "completed" || first.Checkpoint == nil || first.Checkpoint.Done || first.TokensUsed <= 0 {
		t.Fatalf("first run=%+v", first)
	}
	if err := os.WriteFile(filepath.Join(root, "ready.json"), []byte(`{"ready":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	tr, found := srv.triggerStore.GetTrigger(g.TriggerID)
	if !found || tr == nil {
		t.Fatal("managed trigger unavailable")
	}
	if tr.NextFireAt == nil {
		t.Fatal("missing next fire")
	}
	srv.triggerSched.RunOnceForTest(context.Background(), time.Unix(int64(*tr.NextFireAt), 0).Add(time.Second))
	second := waitRealRun(t, srv, g.ID, 2)
	if first.Status != "completed" || second.TurnID == first.TurnID || second.Status != "completed" || second.Checkpoint == nil || !second.Checkpoint.Done || second.TokensUsed <= 0 {
		t.Fatalf("second run=%+v", second)
	}
	if goal, ok := srv.goalStore.Get(g.ID); !ok || goal.Status != goals.StatusCompleted {
		t.Fatalf("goal status=%v ok=%v", goal.Status, ok)
	}
	b, err := os.ReadFile(filepath.Join(root, "evidence.json"))
	if err != nil || string(b) != "READY" {
		t.Fatalf("evidence=%q err=%v", b, err)
	}
}
