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

// Result 为一次 Node 探活的结果摘要。
type Result struct {
	Endpoint         string
	AgentID          string
	Status           string
	Version          string
	ExposeToPeers    bool
	ManageRegistered bool
	Capabilities     []string
}

type healthPayload struct {
	Status  string `json:"status"`
	AgentID string `json:"agent_id"`
	Version string `json:"version"`
}

type agentInfoPayload struct {
	AgentID          string   `json:"agent_id"`
	ExposeToPeers    bool     `json:"expose_to_peers"`
	Capabilities     []string `json:"capabilities"`
	ManageRegistered bool     `json:"manage_registered"`
}

// Node 对 local.endpoint 执行 GET /health 与 GET /v1/agent/info，并可选校验配置中的 agent_id。

// 逻辑：
// 1. 规范化 endpoint 并 GET /health；
// 2. status 非 ok 或 HTTP 非 200 则失败；
// 3. GET /v1/agent/info 并解析；
// 4. 若 cfg.Local.AgentID 非空且与响应不一致则失败。
//
// 异常：网络错误、非 200、JSON 解析失败、agent_id 不一致均返回 error。
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

	expectedID := strings.TrimSpace(cfg.Local.AgentID)
	if expectedID == "" {
		expectedID = strings.TrimSpace(cfg.AgentID)
	}
	if expectedID != "" && health.AgentID != expectedID {
		return nil, fmt.Errorf("agent_id mismatch: config %q, node %q", expectedID, health.AgentID)
	}
	if expectedID != "" && info.AgentID != expectedID {
		return nil, fmt.Errorf("agent info agent_id mismatch: config %q, node %q", expectedID, info.AgentID)
	}

	return &Result{
		Endpoint:         base,
		AgentID:          health.AgentID,
		Status:           health.Status,
		Version:          health.Version,
		ExposeToPeers:    info.ExposeToPeers,
		ManageRegistered: info.ManageRegistered,
		Capabilities:     info.Capabilities,
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
