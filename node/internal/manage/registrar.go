// Package manage 实现 Node 向 Manage 控制面的出站注册与心跳 sidecar。
package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/hostsnapshot"
	"github.com/DGS-ai-team/DAgents/node/internal/version"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

const tokenHeader = "x-dagents-a2a-token"
const agentIDHeader = "x-dagents-agent-id"

// ToolNamesProvider 返回当前可用工具名列表（心跳时刷新）。
type ToolNamesProvider func() []string

// AgentCatalogEntry describes an existing local Agent that Manage may expose
// as a Workgroup member candidate. The Node still owns the runtime; Manage
// stores only this registration metadata.
type AgentCatalogEntry struct {
	ID           string
	Name         string
	Description  string
	Capabilities []string
	Tools        []string
	Skills       []string
	Card         map[string]any
	Metadata     map[string]any
}

type AgentCatalogProvider func() []AgentCatalogEntry

// Registrar 周期性向 Manage 注册并发送心跳。
type Registrar struct {
	cfg          *config.Config
	logger       *slog.Logger
	client       *http.Client
	toolNames    ToolNamesProvider
	agentCatalog AgentCatalogProvider
	interval     time.Duration
	ttlSeconds   int

	mu         sync.RWMutex
	registered bool
}

// NewRegistrar 构造 Manage 注册 sidecar；cfg.Manage.Enabled 应为 true。
func NewRegistrar(cfg *config.Config, logger *slog.Logger) *Registrar {
	if logger == nil {
		logger = slog.Default()
	}
	return &Registrar{
		cfg:        cfg,
		logger:     logger,
		client:     &http.Client{Timeout: 15 * time.Second},
		interval:   cfg.ManageRegistrationInterval(),
		ttlSeconds: cfg.Manage.Registration.TTLSeconds,
	}
}

// SetToolNamesProvider 注入工具名提供者（通常为 session.Manager.ToolNames）。
func (r *Registrar) SetToolNamesProvider(provider ToolNamesProvider) {
	r.toolNames = provider
}

func (r *Registrar) SetAgentCatalogProvider(provider AgentCatalogProvider) {
	r.agentCatalog = provider
}

// Registered 表示最近一次 register/heartbeat 是否成功。
func (r *Registrar) Registered() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.registered
}

func (r *Registrar) setRegistered(ok bool) {
	r.mu.Lock()
	r.registered = ok
	r.mu.Unlock()
}

// Start 启动后台注册/心跳循环；ctx 取消时 goroutine 退出（不自动 deregister）。
func (r *Registrar) Start(ctx context.Context) {
	go r.run(ctx)
}

// Stop 尝试优雅注销并清除注册态。
func (r *Registrar) Stop(ctx context.Context) {
	if err := r.deregister(ctx); err != nil {
		r.logger.Warn("manage deregister failed", "error", err)
	}
	r.setRegistered(false)
}

func (r *Registrar) run(ctx context.Context) {
	if iv := r.register(ctx); iv > 0 {
		r.resetInterval(iv)
	}

	ticker := time.NewTicker(r.currentInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.heartbeat(ctx); err != nil {
				r.logger.Warn("manage heartbeat failed", "error", err)
				if isNotFound(err) {
					if iv := r.register(ctx); iv > 0 {
						ticker.Reset(iv)
					}
				}
				continue
			}
			ticker.Reset(r.currentInterval())
		}
	}
}

func (r *Registrar) currentInterval() time.Duration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.interval
}

func (r *Registrar) resetInterval(d time.Duration) {
	if d <= 0 {
		return
	}
	r.mu.Lock()
	r.interval = d
	r.mu.Unlock()
}

func (r *Registrar) register(ctx context.Context) time.Duration {
	payload := r.buildRegisterPayload()
	if strings.TrimSpace(payload.HostIPs) == "" {
		r.logger.Warn("manage registration host_ips is empty; no non-loopback interface address found")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		r.logger.Error("manage register marshal failed", "error", err)
		r.setRegistered(false)
		return 0
	}

	endpoint := r.registryURL("/v1/registry/agents")
	resp, err := r.doRequest(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		r.logger.Warn("manage register request failed", "error", err)
		r.setRegistered(false)
		return 0
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := readErrorBody(resp.Body)
		r.logger.Warn("manage register rejected", "status", resp.StatusCode, "detail", msg)
		r.setRegistered(false)
		return 0
	}

	var out registerResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		r.logger.Warn("manage register decode failed", "error", err)
		r.setRegistered(false)
		return 0
	}

	r.setRegistered(true)
	r.registerAgentCatalog(ctx)
	r.logger.Info("manage registered", "agent_id", r.cfg.NodeID, "status", out.Agent.Status)
	if out.HeartbeatIntervalSeconds > 0 {
		return time.Duration(out.HeartbeatIntervalSeconds) * time.Second
	}
	return r.cfg.ManageRegistrationInterval()
}

func (r *Registrar) heartbeat(ctx context.Context) error {
	payload := heartbeatPayload{
		TTLSeconds: r.ttlSeconds,
		Version:    version.Version,
		Tools:      r.collectTools(),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		r.setRegistered(false)
		return fmt.Errorf("marshal heartbeat: %w", err)
	}

	endpoint := r.registryURL("/v1/registry/agents/" + url.PathEscape(r.cfg.NodeID) + "/heartbeat")
	resp, err := r.doRequest(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		r.setRegistered(false)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		r.setRegistered(false)
		return errNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		r.setRegistered(false)
		return fmt.Errorf("heartbeat status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}
	r.setRegistered(true)
	r.registerAgentCatalog(ctx)
	return nil
}

func (r *Registrar) registerAgentCatalog(ctx context.Context) {
	if r == nil || r.agentCatalog == nil {
		return
	}
	for _, entry := range r.agentCatalog() {
		id := strings.TrimSpace(entry.ID)
		if id == "" || id == r.cfg.NodeID {
			continue
		}
		payload := r.buildAgentRegisterPayload(entry)
		body, err := json.Marshal(payload)
		if err != nil {
			continue
		}
		resp, err := r.doRequest(ctx, http.MethodPost, r.registryURL("/v1/registry/agents"), body)
		if err != nil {
			r.logger.Warn("manage agent catalog registration failed", "agent_id", id, "error", err)
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			r.logger.Warn("manage agent catalog registration rejected", "agent_id", id, "status", resp.StatusCode)
		}
	}
}

func (r *Registrar) deregister(ctx context.Context) error {
	payload := map[string]string{"reason": "shutdown"}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	endpoint := r.registryURL("/v1/registry/agents/" + url.PathEscape(r.cfg.NodeID) + "/deregister")
	resp, err := r.doRequest(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("deregister status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}
	return nil
}

func (r *Registrar) doRequest(ctx context.Context, method, endpoint string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(agentIDHeader, r.cfg.NodeID)
	if token := strings.TrimSpace(r.cfg.Manage.NodeToken); token != "" {
		req.Header.Set(tokenHeader, token)
	}
	return r.client.Do(req)
}

func (r *Registrar) registryURL(path string) string {
	base := strings.TrimRight(strings.TrimSpace(r.cfg.Manage.URL), "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

func (r *Registrar) buildRegisterPayload() registerPayload {
	name := r.cfg.AgentDisplayName()
	description := r.cfg.AgentDescription()
	host := hostsnapshot.Get()
	caps := r.cfg.RegistrationCapabilities()
	hostIPs := hostsnapshot.LocalHostIPs()
	return registerPayload{
		NodeID:           r.cfg.NodeID,
		AgentID:          r.cfg.NodeID, // 兼容：值同 node_id；Manage 主键仍为 node 级
		BaseURL:          strings.TrimRight(strings.TrimSpace(r.cfg.Local.Endpoint), "/"),
		HostIPs:          hostIPs,
		Capabilities:     caps,
		CapabilitiesHint: caps,
		Tools:            r.collectTools(),
		TTLSeconds:       r.ttlSeconds,
		Name:             name,
		Description:      description,
		Team:             strings.TrimSpace(r.cfg.Manage.Registration.Team),
		Version:          version.Version,
		Card:             RegistrationCard(r.cfg),
		Metadata: map[string]any{
			"node_id":      r.cfg.NodeID,
			"node_version": version.Version,
			"host_ips":     hostIPs,
			"host_info": map[string]any{
				"os_kind":          host.OSKind,
				"sys_platform":     host.SysPlatform,
				"platform_release": host.PlatformRelease,
				"machine":          host.Machine,
				"login_name":       host.LoginName,
			},
			"display": displayMeta(),
			// 跨机器协作走工作组；不再广告 placement.allow_*。
		},
	}
}

func (r *Registrar) buildAgentRegisterPayload(entry AgentCatalogEntry) registerPayload {
	return registerPayload{
		NodeID:           r.cfg.NodeID,
		AgentID:          strings.TrimSpace(entry.ID),
		BaseURL:          strings.TrimRight(strings.TrimSpace(r.cfg.Local.Endpoint), "/"),
		CapabilitiesHint: entry.Capabilities,
		Capabilities:     entry.Capabilities,
		Tools:            entry.Tools,
		Skills:           entry.Skills,
		TTLSeconds:       r.ttlSeconds,
		Name:             strings.TrimSpace(entry.Name),
		Description:      strings.TrimSpace(entry.Description),
		Version:          version.Version,
		Card:             entry.Card,
		Metadata:         mergeCatalogMetadata(entry.Metadata, r.cfg.NodeID),
	}
}

func mergeCatalogMetadata(metadata map[string]any, nodeID string) map[string]any {
	out := make(map[string]any, len(metadata)+1)
	for key, value := range metadata {
		out[key] = value
	}
	out["node_id"] = nodeID
	out["registration_kind"] = "agent_catalog"
	return out
}

func displayMeta() map[string]any {
	h := hostsnapshot.Get()
	osKind := strings.ToLower(strings.TrimSpace(h.OSKind))
	sys := strings.ToLower(strings.TrimSpace(h.SysPlatform))
	available := false
	backend := "none"
	reason := ""
	label := "Unknown"
	switch {
	case osKind == "windows" || sys == "windows":
		available = true
		backend = "stub"
		label = "Windows"
	case osKind == "darwin" || sys == "darwin":
		available = true
		backend = "stub"
		label = "macOS"
	default:
		available = strings.TrimSpace(os.Getenv("DISPLAY")) != "" || strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY")) != ""
		if available {
			backend = "stub"
		} else {
			reason = "no_display"
		}
		switch {
		case osKind != "":
			label = strings.ToUpper(osKind[:1]) + osKind[1:]
		case sys != "":
			label = strings.ToUpper(sys[:1]) + sys[1:]
		}
	}
	out := map[string]any{
		"available": available,
		"label":     label,
		"backend":   backend,
	}
	if reason != "" {
		out["reason_if_unavailable"] = reason
	}
	return out
}

func (r *Registrar) collectTools() []string {
	if r.toolNames == nil {
		return nil
	}
	return r.toolNames()
}

type registerPayload struct {
	NodeID           string         `json:"node_id"`
	AgentID          string         `json:"agent_id,omitempty"` // deprecated: 兼容旧 Manage，值同 node_id
	BaseURL          string         `json:"base_url"`
	HostIPs          string         `json:"host_ips,omitempty"`
	CapabilitiesHint []string       `json:"capabilities_hint,omitempty"`
	Capabilities     []string       `json:"capabilities,omitempty"`
	Tools            []string       `json:"tools,omitempty"`
	Skills           []string       `json:"skills,omitempty"`
	TTLSeconds       int            `json:"ttl_seconds"`
	Name             string         `json:"name"`
	Description      string         `json:"description,omitempty"`
	Team             string         `json:"team,omitempty"`
	Version          string         `json:"version,omitempty"`
	Card             map[string]any `json:"card,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
}

type heartbeatPayload struct {
	TTLSeconds int      `json:"ttl_seconds"`
	Version    string   `json:"version,omitempty"`
	Tools      []string `json:"tools,omitempty"`
}

type registerResponse struct {
	HeartbeatIntervalSeconds int `json:"heartbeat_interval_seconds"`
	Agent                    struct {
		Status string `json:"status"`
	} `json:"agent"`
}

var errNotFound = fmt.Errorf("agent not found")

func isNotFound(err error) bool {
	return err == errNotFound
}

func readErrorBody(r io.Reader) string {
	const limit = 512
	b, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil || len(b) == 0 {
		return ""
	}
	return strings.TrimSpace(string(b))
}
