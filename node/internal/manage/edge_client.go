package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

// EdgeClient 调用 Manage Edge Tunnel。
type EdgeClient struct {
	cfg    *config.Config
	client *http.Client
	stream *http.Client

	mu       sync.Mutex
	sessions map[string]cachedEdgeSession // key: home|agent
}

type cachedEdgeSession struct {
	ID        string
	ExpiresAt time.Time
}

func NewEdgeClient(cfg *config.Config) *EdgeClient {
	return &EdgeClient{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		stream: &http.Client{
			Timeout: 0, // SSE / 长连接
		},
		sessions: map[string]cachedEdgeSession{},
	}
}

func (c *EdgeClient) enabled() bool {
	return c != nil && c.cfg != nil && c.cfg.Manage.Enabled && strings.TrimSpace(c.cfg.Manage.URL) != ""
}

func (c *EdgeClient) manageURL(path string) string {
	base := strings.TrimRight(strings.TrimSpace(c.cfg.Manage.URL), "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

type edgeSessionResponse struct {
	EdgeSessionID string   `json:"edge_session_id"`
	HomeNodeID    string   `json:"home_node_id"`
	AgentID       string   `json:"agent_id"`
	OwnerNodeID   string   `json:"owner_node_id"`
	Scopes        []string `json:"scopes"`
	ExpiresAt     string   `json:"expires_at"`
	ProxyPrefix   string   `json:"proxy_prefix"`
}

func (c *EdgeClient) cacheKey(homeNodeID, agentID string) string {
	return strings.TrimSpace(homeNodeID) + "|" + strings.TrimSpace(agentID)
}

// EnsureSession 取得（或复用）Edge session。
func (c *EdgeClient) EnsureSession(ctx context.Context, homeNodeID, agentID string, scopes []string) (string, error) {
	if !c.enabled() {
		return "", fmt.Errorf("manage is not enabled")
	}
	homeNodeID = strings.TrimSpace(homeNodeID)
	agentID = strings.TrimSpace(agentID)
	if homeNodeID == "" || agentID == "" {
		return "", fmt.Errorf("home_node_id and agent_id required")
	}
	key := c.cacheKey(homeNodeID, agentID)
	now := time.Now()
	c.mu.Lock()
	if cached, ok := c.sessions[key]; ok && cached.ID != "" && cached.ExpiresAt.After(now.Add(30*time.Second)) {
		id := cached.ID
		c.mu.Unlock()
		return id, nil
	}
	c.mu.Unlock()

	if len(scopes) == 0 {
		scopes = []string{"agent", "messages", "streams"}
	}
	body := map[string]any{
		"home_node_id": homeNodeID,
		"agent_id":     agentID,
		"scopes":       scopes,
		"ttl_seconds":  3600,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.manageURL("/v1/edge/sessions"), bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set(agentIDHeader, c.cfg.NodeID)
	if token := strings.TrimSpace(c.cfg.Manage.NodeToken); token != "" {
		req.Header.Set(tokenHeader, token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("edge session status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var out edgeSessionResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.EdgeSessionID) == "" {
		return "", fmt.Errorf("edge session missing id")
	}
	exp := now.Add(50 * time.Minute)
	if t, err := time.Parse(time.RFC3339, out.ExpiresAt); err == nil {
		exp = t
	} else if t, err := time.Parse(time.RFC3339Nano, out.ExpiresAt); err == nil {
		exp = t
	}
	c.mu.Lock()
	c.sessions[key] = cachedEdgeSession{ID: out.EdgeSessionID, ExpiresAt: exp}
	c.mu.Unlock()
	return out.EdgeSessionID, nil
}

func (c *EdgeClient) InvalidateSession(homeNodeID, agentID string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	delete(c.sessions, c.cacheKey(homeNodeID, agentID))
	c.mu.Unlock()
}

// Proxy 将本机请求经 Manage Edge 转发到 home，并把响应写回 w。
func (c *EdgeClient) Proxy(w http.ResponseWriter, r *http.Request, sessionID, targetPathAndQuery string) error {
	if !c.enabled() {
		return fmt.Errorf("manage is not enabled")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("edge session required")
	}
	targetPathAndQuery = strings.TrimSpace(targetPathAndQuery)
	if targetPathAndQuery == "" {
		targetPathAndQuery = "/"
	}
	if !strings.HasPrefix(targetPathAndQuery, "/") {
		targetPathAndQuery = "/" + targetPathAndQuery
	}
	pathOnly := targetPathAndQuery
	rawQuery := ""
	if i := strings.IndexByte(targetPathAndQuery, '?'); i >= 0 {
		pathOnly = targetPathAndQuery[:i]
		rawQuery = targetPathAndQuery[i+1:]
	}
	proxyURL := c.manageURL("/v1/edge/" + url.PathEscape(sessionID) + "/proxy" + pathOnly)
	if rawQuery != "" {
		proxyURL += "?" + rawQuery
	}

	var body io.Reader
	if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
		body = r.Body
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, proxyURL, body)
	if err != nil {
		return err
	}
	// 透传部分头；身份用本 Node 凭证
	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	if accept := r.Header.Get("Accept"); accept != "" {
		req.Header.Set("Accept", accept)
	} else {
		req.Header.Set("Accept", "*/*")
	}
	req.Header.Set(agentIDHeader, c.cfg.NodeID)
	if token := strings.TrimSpace(c.cfg.Manage.NodeToken); token != "" {
		req.Header.Set(tokenHeader, token)
	}

	client := c.client
	if strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream") ||
		strings.HasPrefix(pathOnly, "/v1/streams") {
		client = c.stream
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	for k, vals := range resp.Header {
		if strings.EqualFold(k, "Transfer-Encoding") || strings.EqualFold(k, "Connection") {
			continue
		}
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	streamish := strings.HasPrefix(pathOnly, "/v1/streams") ||
		strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") ||
		strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream")
	if !streamish {
		_, copyErr := io.Copy(w, resp.Body)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return copyErr
	}
	// SSE：逐块写出并 Flush，避免缓冲到连接结束才推到浏览器。
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return werr
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}
