package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func testPolicyServer(t *testing.T) (*httptest.Server, *config.Config) {
	t.Helper()
	cfg := testConfig(t)
	reg, err := tools.NewRegistry(cfg.FSRoot, 30)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil,
		WithLLM(&llm.MockClient{}),
		WithTools(reg),
		WithSkipStore(),
	)
	return httptest.NewServer(srv.Handler()), cfg
}

func TestHandleGetPutPolicy(t *testing.T) {
	ts, _ := testPolicyServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/policy")
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
	if snap["policy_dir"] == "" {
		t.Fatal("policy_dir required")
	}

	putBody := []byte(`{"updates":[{"name":"write_file","decision":"deny"}]}`)
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/v1/policy/tools", bytes.NewReader(putBody))
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

	resp2, err := http.Get(ts.URL + "/v1/policy")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var snap2 struct {
		Tools []struct {
			Name     string `json:"name"`
			Decision string `json:"decision"`
		} `json:"tools"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&snap2); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range snap2.Tools {
		if item.Name == "write_file" && item.Decision == "deny" {
			found = true
		}
	}
	if !found {
		t.Fatal("write_file should be deny after PUT")
	}
}

func TestHandlePutShellPolicy(t *testing.T) {
	ts, _ := testPolicyServer(t)
	defer ts.Close()

	putBody := []byte(`{"updates":[{"command":"rm","decision":"deny"}]}`)
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/v1/policy/shell/bash", bytes.NewReader(putBody))
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
}

func TestHandlePutPolicyProtectAskUserInformation(t *testing.T) {
	ts, _ := testPolicyServer(t)
	defer ts.Close()

	putBody := []byte(`{"updates":[{"name":"ask_user_information","decision":"deny"}]}`)
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/v1/policy/tools", bytes.NewReader(putBody))
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
