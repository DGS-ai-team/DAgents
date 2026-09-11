package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func testAgentPolicyServer(t *testing.T) (*Server, *httptest.Server, string) {
	t.Helper()
	cfg := &config.Config{NodeID: "node-test", RuntimeRoot: t.TempDir()}
	cfg.ApplyDefaults()
	cfg.Onboarding.NodeProfileCompleted = true
	agentsDB, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agentsDB.Close() })

	userDir := cfg.AgentTemplatesDir()
	_ = os.MkdirAll(userDir, 0o755)
	_ = os.WriteFile(filepath.Join(userDir, "general.yaml"), []byte("id: general\ndisplay_name: G\n"), 0o644)

	reg, err := tools.NewRegistry(cfg.RuntimeDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil,
		WithLLM(&llm.MockClient{}),
		WithTools(reg),
		WithSkipStore(),
	)
	if srv.triggerSched != nil {
		srv.triggerSched.Stop()
	}
	srv.agents = agentsDB
	t.Cleanup(func() {
		if srv.triggerSched != nil {
			srv.triggerSched.Stop()
		}
		if srv.sessions != nil {
			srv.sessions.Stop()
		}
	})

	body, _ := json.Marshal(map[string]any{
		"template_id":  "general",
		"display_name": "策略测试",
		"defaults": map[string]any{
			"llm":   map[string]any{"active": "mock"},
			"tools": map[string]any{"enabled_groups": []string{"fs", "bash", "memory"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("create agent status=%d body=%s", rr.Code, rr.Body.String())
	}
	var created agentView
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts, created.AgentID
}

func TestAgentPolicyGrantRevokeConcurrentWithToolPutDoesNotResurrect(t *testing.T) {
	_, ts, agentID := testAgentPolicyServer(t)
	workspace := t.TempDir()
	create := doPolicyJSON(t, ts, http.MethodPost, "/v1/agents/"+agentID+"/policy/grants", map[string]any{"tools": []string{"write_file"}, "workspace": workspace, "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	var grant map[string]any
	if err := json.Unmarshal(create, &grant); err != nil {
		t.Fatal(err)
	}
	id, _ := grant["id"].(string)
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, status := doPolicyJSONResult(ts, http.MethodDelete, "/v1/agents/"+agentID+"/policy/grants/"+id, nil)
		statuses <- status
	}()
	go func() {
		defer wg.Done()
		_, status := doPolicyJSONResult(ts, http.MethodPut, "/v1/agents/"+agentID+"/policy/tools", map[string]any{"updates": []map[string]string{{"name": "write_file", "mode": "rule"}}})
		statuses <- status
	}()
	wg.Wait()
	close(statuses)
	for status := range statuses {
		if status < 200 || status >= 300 {
			t.Fatalf("concurrent policy request status=%d", status)
		}
	}
	grants := doPolicyJSON(t, ts, http.MethodGet, "/v1/agents/"+agentID+"/policy/grants", nil)
	var result struct {
		Grants []policy.Grant `json:"grants"`
	}
	if err := json.Unmarshal(grants, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Grants) != 1 || result.Grants[0].RevokedAt == nil {
		t.Fatalf("grant resurrected: %+v", result.Grants)
	}
}

func doPolicyJSON(t *testing.T, ts *httptest.Server, method, path string, body any) []byte {
	raw, status := doPolicyJSONResult(ts, method, path, body)
	if status >= 300 {
		t.Fatalf("%s %s status=%d body=%s", method, path, status, raw)
	}
	return []byte(raw)
}

func doPolicyJSONResult(ts *httptest.Server, method, path string, body any) (string, int) {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		encoded, _ := json.Marshal(body)
		reader = bytes.NewReader(encoded)
	}
	req, _ := http.NewRequest(method, ts.URL+path, reader)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err.Error(), 599
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return string(raw), resp.StatusCode
}

func TestGlobalPolicyRouteRemoved(t *testing.T) {
	_, ts, _ := testAgentPolicyServer(t)
	resp, err := http.Get(ts.URL + "/v1/policy")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /v1/policy status = %d want 404", resp.StatusCode)
	}
}

func TestHandleGetPutAgentPolicy(t *testing.T) {
	_, ts, agentID := testAgentPolicyServer(t)

	resp, err := http.Get(ts.URL + "/v1/agents/" + agentID + "/policy")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d", resp.StatusCode)
	}
	var snap map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}
	if snap["source"] != "sqlite" || snap["agent_id"] != agentID {
		t.Fatalf("snap meta = %+v", snap)
	}

	putBody := []byte(`{"updates":[{"name":"write_file","mode":"deny"}]}`)
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/v1/agents/"+agentID+"/policy/tools", bytes.NewReader(putBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	putResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT tools status = %d", putResp.StatusCode)
	}

	resp2, err := http.Get(ts.URL + "/v1/agents/" + agentID + "/policy")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var snap2 struct {
		Tools []struct {
			Name string `json:"name"`
			Mode string `json:"mode"`
		} `json:"tools"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&snap2); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range snap2.Tools {
		if item.Name == "write_file" && item.Mode == "deny" {
			found = true
		}
	}
	if !found {
		t.Fatal("write_file should be deny after PUT")
	}
}

func TestHandlePutAgentShellPolicy(t *testing.T) {
	_, ts, agentID := testAgentPolicyServer(t)

	putBody := []byte(`{"updates":[{"command":"rm","mode":"deny"}]}`)
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/v1/agents/"+agentID+"/policy/shell/bash", bytes.NewReader(putBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	putResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT shell status = %d", putResp.StatusCode)
	}

	putBody2 := []byte(`{"deletes":["rm"]}`)
	req2, err := http.NewRequest(http.MethodPut, ts.URL+"/v1/agents/"+agentID+"/policy/shell/bash", bytes.NewReader(putBody2))
	if err != nil {
		t.Fatal(err)
	}
	req2.Header.Set("Content-Type", "application/json")
	putResp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	putResp2.Body.Close()
	if putResp2.StatusCode != http.StatusOK {
		t.Fatalf("DELETE shell status = %d", putResp2.StatusCode)
	}
}

func TestHandlePutAgentPolicyProtectAskUserInformation(t *testing.T) {
	_, ts, agentID := testAgentPolicyServer(t)

	putBody := []byte(`{"updates":[{"name":"ask_user_information","mode":"deny"}]}`)
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/v1/agents/"+agentID+"/policy/tools", bytes.NewReader(putBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	putResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	putResp.Body.Close()
	if putResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", putResp.StatusCode)
	}
}

func TestHandleAgentPromptContextRoundtrip(t *testing.T) {
	srv, ts, agentID := testAgentPolicyServer(t)
	contextBefore, err := srv.sessions.GetContextView(agentID)
	if err != nil {
		t.Fatal(err)
	}

	putBody := []byte(`{"soul_md":"我是助手","custom_md":"临时指令","memory_entries":[{"content":"记得开会"}],"memory_scope":"global"}`)
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/v1/agents/"+agentID+"/prompt-context", bytes.NewReader(putBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	putResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT prompt-context status = %d", putResp.StatusCode)
	}

	resp, err := http.Get(ts.URL + "/v1/agents/" + agentID + "/prompt-context")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d", resp.StatusCode)
	}
	var view agentPromptContextView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if view.SoulMD != "我是助手" || view.Source != "workspace_memory" {
		t.Fatalf("view = %+v", view)
	}
	if len(view.GlobalMemoryEntries) != 1 || view.GlobalMemoryEntries[0].Content != "记得开会" {
		t.Fatalf("global memory entries = %+v", view.GlobalMemoryEntries)
	}
	contextView, err := srv.sessions.GetContextView(agentID)
	if err != nil {
		t.Fatal(err)
	}
	if contextView.ContextInjectionCount == 0 || contextView.ContextInjectionDigest == contextBefore.ContextInjectionDigest {
		t.Fatalf("live runtime context was not refreshed: before=%+v after=%+v", contextBefore, contextView)
	}
	rec, err := srv.agents.Get(t.Context(), agentID)
	if err != nil || rec == nil {
		t.Fatalf("load agent after prompt update: rec=%v err=%v", rec, err)
	}
	snap, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if got := agentruntime.MemoryScopeFromDefaults(snap); got != "global" {
		t.Fatalf("memory_scope was not persisted to agent snapshot: %q", got)
	}
}

func TestHandleAgentMemoryEntryMutation(t *testing.T) {
	_, ts, agentID := testAgentPolicyServer(t)
	base := ts.URL + "/v1/agents/" + agentID + "/prompt-context"
	putBody := []byte(`{"memory_entries":[{"id":"lt-edit","content":"旧内容"},{"id":"lt-delete","content":"待删除"}]}`)
	putReq, err := http.NewRequest(http.MethodPut, base, bytes.NewReader(putBody))
	if err != nil {
		t.Fatal(err)
	}
	putReq.Header.Set("Content-Type", "application/json")
	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("seed memory status = %d", putResp.StatusCode)
	}

	patchBody := []byte(`{"scope":"agent","content":"已编辑"}`)
	patchReq, err := http.NewRequest(http.MethodPatch, base+"/memory/lt-edit", bytes.NewReader(patchBody))
	if err != nil {
		t.Fatal(err)
	}
	patchReq.Header.Set("Content-Type", "application/json")
	patchResp, err := http.DefaultClient.Do(patchReq)
	if err != nil {
		t.Fatal(err)
	}
	patchResp.Body.Close()
	if patchResp.StatusCode != http.StatusOK {
		t.Fatalf("patch memory status = %d", patchResp.StatusCode)
	}

	deleteReq, err := http.NewRequest(http.MethodDelete, base+"/memory/lt-delete?scope=agent", nil)
	if err != nil {
		t.Fatal(err)
	}
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatal(err)
	}
	deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusOK {
		t.Fatalf("delete memory status = %d", deleteResp.StatusCode)
	}

	getResp, err := http.Get(base)
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	var view agentPromptContextView
	if err := json.NewDecoder(getResp.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if len(view.MemoryEntries) != 1 || view.MemoryEntries[0].ID != "lt-edit" || view.MemoryEntries[0].Content != "已编辑" {
		t.Fatalf("mutated memory entries = %+v", view.MemoryEntries)
	}
}
