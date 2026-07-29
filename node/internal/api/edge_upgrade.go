package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

// tryEdgeUpgrade 若请求目标是远端 Placement 引用，则经 Manage Edge Tunnel 转发并返回 true。
//
// 不升级：
// - 列表 / 创建 Agent
// - DELETE /v1/agents/{id}（走 Control 双删）
// - peers / internal placement / health 等
//
// 注意：extractEdgeAgentID 对 POST /v1/messages 会读并关闭 Body；凡 fall-through 到本地
// 处理的路径都必须 restoreBody，否则本地 handler 会报 http: invalid Read on closed Body。
func (s *Server) tryEdgeUpgrade(w http.ResponseWriter, r *http.Request) bool {
	if s == nil || s.edge == nil || s.agents == nil || s.cfg == nil || !s.cfg.Manage.Enabled {
		return false
	}
	agentID, rebuiltBody, ok := extractEdgeAgentID(r)
	restoreBody := func() {
		if rebuiltBody == nil {
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(rebuiltBody))
		r.ContentLength = int64(len(rebuiltBody))
		r.Header.Set("Content-Length", itoaLen(len(rebuiltBody)))
	}
	if !ok {
		restoreBody()
		return false
	}
	if agentID == "" {
		restoreBody()
		return false
	}
	// DELETE 根资源走本地 Control 双删
	if r.Method == http.MethodDelete && isExactAgentRootPath(r.URL.Path, agentID) {
		restoreBody()
		return false
	}
	rec, err := s.agents.Get(r.Context(), agentID)
	if err != nil || rec == nil || rec.Archived {
		restoreBody()
		return false
	}
	if store.NormalizeAgentOrigin(rec.Origin) != store.AgentOriginRemote {
		restoreBody()
		return false
	}
	p := decodePlacement(rec.PlacementJSON)
	homeID := strings.TrimSpace(p.HomeNodeID)
	if homeID == "" {
		writeAPIError(w, http.StatusBadGateway, "placement_incomplete", "remote agent missing home_node_id", map[string]any{"agent_id": agentID})
		return true
	}

	if rebuiltBody != nil {
		restoreBody()
	} else if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
		_ = r.Body.Close()
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_body", err.Error(), nil)
			return true
		}
		rebuiltBody = raw
		restoreBody()
	}

	sid, err := s.edge.EnsureSession(r.Context(), homeID, agentID, []string{"agent", "messages", "streams", "screen:view"})
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "edge_session_failed", err.Error(), map[string]any{
			"agent_id":     agentID,
			"home_node_id": homeID,
		})
		return true
	}
	target := r.URL.Path
	if q := r.URL.RawQuery; q != "" {
		target = target + "?" + q
	}
	proxyOnce := func(sessionID string) error {
		if rebuiltBody != nil {
			r.Body = io.NopCloser(bytes.NewReader(rebuiltBody))
			r.ContentLength = int64(len(rebuiltBody))
		}
		return s.edge.Proxy(w, r, sessionID, target)
	}
	if err := proxyOnce(sid); err != nil {
		// 会话可能过期：清缓存后再试一次
		s.edge.InvalidateSession(homeID, agentID)
		sid2, err2 := s.edge.EnsureSession(r.Context(), homeID, agentID, []string{"agent", "messages", "streams", "screen:view"})
		if err2 != nil {
			writeAPIError(w, http.StatusBadGateway, "edge_proxy_failed", err.Error(), map[string]any{"retry": err2.Error()})
			return true
		}
		if err := proxyOnce(sid2); err != nil {
			writeAPIError(w, http.StatusBadGateway, "edge_proxy_failed", err.Error(), nil)
		}
	}
	return true
}

func itoaLen(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func isExactAgentRootPath(path, agentID string) bool {
	want := "/v1/agents/" + agentID
	return strings.TrimSuffix(path, "/") == want
}

// extractEdgeAgentID 从 path / query / body 解析远端代理所需的 agent_id。
// 对 POST /v1/messages 会读 body；若需继续本地处理，调用方应使用返回的 rebuiltBody 还原。
func extractEdgeAgentID(r *http.Request) (agentID string, rebuiltBody []byte, ok bool) {
	path := r.URL.Path
	if strings.HasPrefix(path, "/v1/agents/") {
		rest := strings.TrimPrefix(path, "/v1/agents/")
		if rest == "" {
			return "", nil, false
		}
		id, _, _ := strings.Cut(rest, "/")
		id = strings.TrimSpace(id)
		if id == "" {
			return "", nil, false
		}
		// 列表已排除；创建是 POST /v1/agents（无 id）
		return id, nil, true
	}
	if path == "/v1/streams" || strings.HasPrefix(path, "/v1/streams/") {
		id := strings.TrimSpace(r.URL.Query().Get("agent_id"))
		if id == "" {
			return "", nil, false
		}
		return id, nil, true
	}
	if path == "/v1/messages" && (r.Method == http.MethodPost || r.Method == http.MethodPut) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		_ = r.Body.Close()
		if err != nil {
			return "", nil, false
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			// 无法解析则不升级，还原 body 交本地
			return "", raw, false
		}
		id := ""
		if v, ok := body["agent_id"].(string); ok {
			id = strings.TrimSpace(v)
		}
		if id == "" {
			if v, ok := body["agentId"].(string); ok {
				id = strings.TrimSpace(v)
			}
		}
		if id == "" {
			return "", raw, false
		}
		return id, raw, true
	}
	return "", nil, false
}
