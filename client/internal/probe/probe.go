package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

// LLMInfo 为探活时从 agent/info 解析的 LLM 运行时摘要。
type LLMInfo struct {
	Provider          string
	Model             string
	Mock              bool
	ThinkingSupported bool
	Thinking          string
	ReasoningEffort   string
}

// Result 为一次 Node 探活的结果摘要。
type Result struct {
	Endpoint         string
	NodeID           string
	Status           string
	Version          string
	ManageRegistered bool
	Capabilities     []string
	LLM              LLMInfo
}

type healthPayload struct {
	Status  string `json:"status"`
	NodeID  string `json:"node_id"`
	Version string `json:"version"`
}

type llmInfoPayload struct {
	Provider          string `json:"provider"`
	Model             string `json:"model"`
	Mock              bool   `json:"mock"`
	ThinkingSupported bool   `json:"thinking_supported"`
	Thinking          string `json:"thinking"`
	ReasoningEffort   string `json:"reasoning_effort"`
}

type agentInfoPayload struct {
	NodeID           string         `json:"node_id"`
	Capabilities     []string       `json:"capabilities"`
	ManageRegistered bool           `json:"manage_registered"`
	LLM              llmInfoPayload `json:"llm"`
}

// Node 对 local.endpoint 执行 GET /health 与 GET /v1/agent/info，并可选校验配置中的 node_id。
func Node(ctx context.Context, cfg *config.Config, httpClient *http.Client) (*Result, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	base := strings.TrimRight(cfg.Local.Endpoint, "/")

	health, err := fetchHealth(ctx, httpClient, base)
	if err != nil {
		return nil, err
	}
	if health.Status != "ok" {
		return nil, fmt.Errorf("node unhealthy: status=%q", health.Status)
	}

	info, err := fetchAgentInfo(ctx, httpClient, base)
	if err != nil {
		return nil, err
	}

	expectedID := strings.TrimSpace(cfg.Local.NodeID)
	if expectedID == "" {
		expectedID = strings.TrimSpace(cfg.NodeID)
	}
	if expectedID != "" && health.NodeID != expectedID {
		return nil, fmt.Errorf("node_id mismatch: config %q, node %q", expectedID, health.NodeID)
	}
	if expectedID != "" && info.NodeID != expectedID {
		return nil, fmt.Errorf("agent info node_id mismatch: config %q, node %q", expectedID, info.NodeID)
	}

	return &Result{
		Endpoint:         base,
		NodeID:           health.NodeID,
		Status:           health.Status,
		Version:          health.Version,
		ManageRegistered: info.ManageRegistered,
		Capabilities:     info.Capabilities,
		LLM: LLMInfo{
			Provider:          info.LLM.Provider,
			Model:             info.LLM.Model,
			Mock:              info.LLM.Mock,
			ThinkingSupported: info.LLM.ThinkingSupported,
			Thinking:          info.LLM.Thinking,
			ReasoningEffort:   info.LLM.ReasoningEffort,
		},
	}, nil
}

func fetchHealth(ctx context.Context, client *http.Client, base string) (*healthPayload, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /health: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("GET /health: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload healthPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode /health: %w", err)
	}
	return &payload, nil
}

func fetchAgentInfo(ctx context.Context, client *http.Client, base string) (*agentInfoPayload, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/agent/info", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /v1/agent/info: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("GET /v1/agent/info: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload agentInfoPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode /v1/agent/info: %w", err)
	}
	return &payload, nil
}
