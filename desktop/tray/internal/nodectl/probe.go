package nodectl

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

// Health 为 GET /health 的摘要。
type Health struct {
	OK      bool
	Status  string
	AgentID string
	Version string
}

type healthPayload struct {
	Status  string `json:"status"`
	AgentID string `json:"agent_id"`
	Version string `json:"version"`
}

// Probe 对 cfg.Local.Endpoint 执行 GET /health。
func Probe(ctx context.Context, cfg *config.Config, client *http.Client) (*Health, error) {
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.Local.Endpoint), "/")
	if base == "" {
		return nil, fmt.Errorf("local.endpoint is empty")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("health status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload healthPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return &Health{
		OK:      payload.Status == "ok",
		Status:  payload.Status,
		AgentID: payload.AgentID,
		Version: payload.Version,
	}, nil
}

// IsRunning 探活成功且 status=ok 时返回 true。
func IsRunning(ctx context.Context, cfg *config.Config) bool {
	h, err := Probe(ctx, cfg, nil)
	return err == nil && h != nil && h.OK
}
