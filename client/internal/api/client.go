// Package api 提供 Agent Node HTTP/SSE 客户端（N1：session、message、stream）。
package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client 连接本地 Agent Node 的 HTTP 客户端。
type Client struct {
	base       string
	httpClient *http.Client
}

// New 构造 Node API 客户端；base 为 local.endpoint（无尾斜杠）。
func New(base string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		base:       strings.TrimRight(base, "/"),
		httpClient: httpClient,
	}
}

// StreamEvent 表示一条 SSE 业务事件（已解析 JSON data 行）。
type StreamEvent struct {
	ID        string
	Type      string
	SessionID string
	AgentID   string
	Seq       int
	Data      map[string]any
}

// CreateSession 调用 POST /v1/sessions；sessionID 为空则由 Node 生成。
func (c *Client) CreateSession(ctx context.Context, sessionID string) (string, error) {
	body := map[string]any{}
	if strings.TrimSpace(sessionID) != "" {
		body["session_id"] = sessionID
	}
	var resp struct {
		SessionID string `json:"session_id"`
	}
	if err := c.postJSON(ctx, "/v1/sessions", body, &resp); err != nil {
		return "", err
	}
	if resp.SessionID == "" {
		return "", fmt.Errorf("empty session_id in response")
	}
	return resp.SessionID, nil
}

// LLMSettings 为 Node LLM 运行时参数（GET/PATCH /v1/llm/settings 与 agent/info.llm）。
type LLMSettings struct {
	Provider          string `json:"provider"`
	Model             string `json:"model"`
	Mock              bool   `json:"mock"`
	ThinkingSupported bool   `json:"thinking_supported"`
	Thinking          string `json:"thinking,omitempty"`
	ReasoningEffort   string `json:"reasoning_effort,omitempty"`
}

// LLMSettingsPatch 为 PATCH /v1/llm/settings 请求体。
type LLMSettingsPatch struct {
	Thinking        *string `json:"thinking,omitempty"`
	ReasoningEffort *string `json:"reasoning_effort,omitempty"`
}

// AgentInfo 为 GET /v1/agent/info 响应。
type AgentInfo struct {
	AgentID          string      `json:"agent_id"`
	ExposeToPeers    bool        `json:"expose_to_peers"`
	Capabilities     []string    `json:"capabilities"`
	ManageRegistered bool        `json:"manage_registered"`
	LLM              LLMSettings `json:"llm"`
}

// SessionSummary 为 GET /v1/sessions 列表项。
type SessionSummary struct {
	SessionID        string `json:"session_id"`
	AgentID          string `json:"agent_id"`
	Active           bool   `json:"active"`
	MessageCount     int    `json:"message_count"`
	FirstUserMessage string `json:"first_user_message"`
	UpdatedAt        string `json:"updated_at"`
	QueuePending     int    `json:"queue_pending"`
	HasActiveTurn    bool   `json:"has_active_turn"`
	RunTurnPhase     string `json:"run_turn_phase"`
}

// SessionContext 为 GET /v1/sessions/{id}/context 响应。
type SessionContext struct {
	SessionID             string `json:"session_id"`
	MessagesCount         int    `json:"messages_count"`
	PendingToolCallsCount int    `json:"pending_tool_calls_count"`
	MessagesTotalTokens   int    `json:"messages_total_tokens"`
	ToolLoopCount         int    `json:"tool_loop_count"`
	QueuePending          int    `json:"queue_pending"`
	HasActiveTurn         bool   `json:"has_active_turn"`
	TurnState             string `json:"turn_state"`
	RunTurnPhase          string `json:"run_turn_phase"`
	SystemPrompt                   string `json:"system_prompt"`
	SystemPromptEstimatedTokens    int    `json:"system_prompt_estimated_tokens"`
	SkillsCatalogEstimatedTokens        int    `json:"skills_catalog_estimated_tokens"`
	SkillsCatalogMaxBodyEstimatedTokens int    `json:"skills_catalog_max_body_estimated_tokens"`
	SkillsCatalogBloatThreshold         int    `json:"skills_catalog_bloat_threshold"`
	LoadedSkills                   []LoadedSkillSummary `json:"loaded_skills"`
	RecentMessages        []ContextMessagePreview `json:"recent_messages"`
}

// CompressContextResult 为 POST /v1/sessions/{id}/compress 响应。
type CompressContextResult struct {
	Status                 string `json:"status"`
	TriggerLevel           string `json:"trigger_level"`
	CompressedMessageCount int    `json:"compressed_message_count"`
	CompressionStart       int    `json:"compression_start"`
	CompressionEnd         int    `json:"compression_end"`
	MessagesCount          int     `json:"messages_count"`
	MessagesTotalTokens    int     `json:"messages_total_tokens"`
	PromptTokens           int     `json:"prompt_tokens"`
	CompletionTokens       int     `json:"completion_tokens"`
	TotalTokens            int     `json:"total_tokens"`
	TokenReductionRate     float64 `json:"token_reduction_rate"`
	PromptCacheHitTokens   int     `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens  int     `json:"prompt_cache_miss_tokens"`
}

// LoadedSkillSummary 为 context/skills 中的已加载 skill 摘要。
type LoadedSkillSummary struct {
	SkillName   string `json:"skill_name"`
	Description string `json:"description"`
}

// ContextMessagePreview 为 context 最近消息预览。
type ContextMessagePreview struct {
	Role                string `json:"role"`
	Content             string `json:"content"`
	ToolCallID          string `json:"tool_call_id"`
	ToolCallsCount      int    `json:"tool_calls_count"`
	HasReasoningContent bool   `json:"has_reasoning_content"`
}

// PolicyPlatform 为 GET /v1/policy 中的 Node 平台信息。
type PolicyPlatform struct {
	GOOS         string `json:"goos"`
	DefaultShell string `json:"default_shell"`
}

// PolicyToolEntry 为工具策略条目。
type PolicyToolEntry struct {
	Name       string `json:"name"`
	Decision   string `json:"decision"`
	Configured bool   `json:"configured"`
}

// PolicyShellEntry 为 shell 命令策略条目。
type PolicyShellEntry struct {
	Command    string `json:"command"`
	Decision   string `json:"decision"`
	Configured bool   `json:"configured"`
}

// PolicySnapshot 为 GET /v1/policy 响应。
type PolicySnapshot struct {
	PolicyDir string                       `json:"policy_dir"`
	Platform  PolicyPlatform               `json:"platform"`
	Tools     []PolicyToolEntry            `json:"tools"`
	Shell     map[string][]PolicyShellEntry `json:"shell"`
}

// PolicyToolUpdate 为 PUT /v1/policy/tools 单项。
type PolicyToolUpdate struct {
	Name     string `json:"name"`
	Decision string `json:"decision"`
}

// PolicyShellUpdate 为 PUT /v1/policy/shell/{type} 单项。
type PolicyShellUpdate struct {
	Command  string `json:"command"`
	Decision string `json:"decision"`
}

// GetAgentInfo 调用 GET /v1/agent/info。
func (c *Client) GetAgentInfo(ctx context.Context) (*AgentInfo, error) {
	var info AgentInfo
	if err := c.getJSON(ctx, "/v1/agent/info", &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// GetLLMSettings 调用 GET /v1/llm/settings。
func (c *Client) GetLLMSettings(ctx context.Context) (*LLMSettings, error) {
	var settings LLMSettings
	if err := c.getJSON(ctx, "/v1/llm/settings", &settings); err != nil {
		return nil, err
	}
	return &settings, nil
}

// PatchLLMSettings 调用 PATCH /v1/llm/settings。
func (c *Client) PatchLLMSettings(ctx context.Context, patch LLMSettingsPatch) (*LLMSettings, error) {
	var settings LLMSettings
	if err := c.patchJSON(ctx, "/v1/llm/settings", patch, &settings); err != nil {
		return nil, err
	}
	return &settings, nil
}

// ListSessions 调用 GET /v1/sessions。
func (c *Client) ListSessions(ctx context.Context) ([]SessionSummary, error) {
	var resp struct {
		Sessions []SessionSummary `json:"sessions"`
	}
	if err := c.getJSON(ctx, "/v1/sessions", &resp); err != nil {
		return nil, err
	}
	return resp.Sessions, nil
}

// GetSessionContext 调用 GET /v1/sessions/{id}/context。
func (c *Client) GetSessionContext(ctx context.Context, sessionID string) (*SessionContext, error) {
	var ctxBody SessionContext
	path := "/v1/sessions/" + url.PathEscape(strings.TrimSpace(sessionID)) + "/context"
	if err := c.getJSON(ctx, path, &ctxBody); err != nil {
		return nil, err
	}
	return &ctxBody, nil
}

// CancelTurn 调用 POST /v1/sessions/{id}/cancel，取消在途 turn。
func (c *Client) CancelTurn(ctx context.Context, sessionID string) (bool, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return false, fmt.Errorf("session_id is required")
	}
	path := "/v1/sessions/" + url.PathEscape(sid) + "/cancel"
	var resp struct {
		SessionID string `json:"session_id"`
		Cancelled bool   `json:"cancelled"`
	}
	if err := c.postJSON(ctx, path, map[string]any{}, &resp); err != nil {
		return false, err
	}
	return resp.Cancelled, nil
}

// ClearSessionContext 调用 POST /v1/sessions/{id}/clear-context。
func (c *Client) ClearSessionContext(ctx context.Context, sessionID string) error {
	path := "/v1/sessions/" + url.PathEscape(strings.TrimSpace(sessionID)) + "/clear-context"
	return c.postJSON(ctx, path, map[string]any{}, nil)
}

// TriggerDefinition 为 GET /v1/triggers 列表项。
type TriggerDefinition struct {
	TriggerID         string         `json:"trigger_id"`
	Name              string         `json:"name"`
	Condition         map[string]any `json:"condition"`
	TargetAgentID     string         `json:"target_agent_id"`
	TargetSessionID   *string        `json:"target_session_id"`
	SessionTargetMode string         `json:"session_target_mode"`
	TaskTemplate      string         `json:"task_template"`
	Enabled           bool           `json:"enabled"`
	FireCount         int            `json:"fire_count"`
	LastFiredAt       *float64       `json:"last_fired_at"`
	NextFireAt        *float64       `json:"next_fire_at"`
}

// ListTriggers 调用 GET /v1/triggers。
func (c *Client) ListTriggers(ctx context.Context) ([]TriggerDefinition, error) {
	var resp struct {
		Triggers []TriggerDefinition `json:"triggers"`
	}
	if err := c.getJSON(ctx, "/v1/triggers", &resp); err != nil {
		return nil, err
	}
	return resp.Triggers, nil
}

// GetPolicy 调用 GET /v1/policy；shellQuery 可为 auto/bash/cmd/powershell。
func (c *Client) GetPolicy(ctx context.Context, shellQuery string) (*PolicySnapshot, error) {
	path := "/v1/policy"
	if q := strings.TrimSpace(shellQuery); q != "" {
		path += "?shell=" + url.QueryEscape(q)
	}
	var snap PolicySnapshot
	if err := c.getJSON(ctx, path, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

// UpdateToolPolicy 调用 PUT /v1/policy/tools。
func (c *Client) UpdateToolPolicy(ctx context.Context, updates []PolicyToolUpdate) error {
	return c.putJSON(ctx, "/v1/policy/tools", map[string]any{"updates": updates}, nil)
}

// UpdateShellPolicy 调用 PUT /v1/policy/shell/{shellType}。
func (c *Client) UpdateShellPolicy(ctx context.Context, shellType string, updates []PolicyShellUpdate) error {
	path := "/v1/policy/shell/" + url.PathEscape(strings.TrimSpace(shellType))
	return c.putJSON(ctx, path, map[string]any{"updates": updates}, nil)
}

// CompressSessionContext 调用 POST /v1/sessions/{id}/compress，手动触发阻塞压缩。
func (c *Client) CompressSessionContext(ctx context.Context, sessionID string) (*CompressContextResult, error) {
	var out CompressContextResult
	path := "/v1/sessions/" + url.PathEscape(strings.TrimSpace(sessionID)) + "/compress"
	if err := c.postJSON(ctx, path, map[string]any{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteSession 调用 DELETE /v1/sessions/{id}。
func (c *Client) DeleteSession(ctx context.Context, sessionID string) error {
	path := "/v1/sessions/" + url.PathEscape(strings.TrimSpace(sessionID))
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.base+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("DELETE %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// SessionSkills 为 skills 查询/变更响应。
type SessionSkills struct {
	SessionID       string `json:"session_id"`
	LoadedSkills    []any  `json:"loaded_skills"`
	AvailableSkills []any  `json:"available_skills"`
}

// ListSessionSkills 调用 GET /v1/sessions/{id}/skills。
func (c *Client) ListSessionSkills(ctx context.Context, sessionID string) (*SessionSkills, error) {
	var out SessionSkills
	path := "/v1/sessions/" + url.PathEscape(strings.TrimSpace(sessionID)) + "/skills"
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// LoadSessionSkill 调用 POST /v1/sessions/{id}/skills/load。
func (c *Client) LoadSessionSkill(ctx context.Context, sessionID, skillName string) (*SessionSkills, error) {
	path := "/v1/sessions/" + url.PathEscape(strings.TrimSpace(sessionID)) + "/skills/load"
	var out SessionSkills
	if err := c.postJSON(ctx, path, map[string]any{"skill_name": skillName}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UnloadSessionSkill 调用 POST /v1/sessions/{id}/skills/unload。
func (c *Client) UnloadSessionSkill(ctx context.Context, sessionID, skillName string) (*SessionSkills, error) {
	path := "/v1/sessions/" + url.PathEscape(strings.TrimSpace(sessionID)) + "/skills/unload"
	var out SessionSkills
	if err := c.postJSON(ctx, path, map[string]any{"skill_name": skillName}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
	}
	return nil
}

func (c *Client) SubmitMessage(ctx context.Context, sessionID, content string) error {
	body := map[string]any{
		"session_id":   sessionID,
		"request_type": "message",
		"content":      content,
	}
	var resp struct {
		Accepted bool `json:"accepted"`
	}
	if err := c.postJSON(ctx, "/v1/messages", body, &resp); err != nil {
		return err
	}
	if !resp.Accepted {
		return fmt.Errorf("message not accepted")
	}
	return nil
}

// SubmitResume 调用 POST /v1/messages 投递 HITL resume。
func (c *Client) SubmitResume(ctx context.Context, sessionID string, resumeValue map[string]any) error {
	body := map[string]any{
		"session_id":   sessionID,
		"request_type": "resume",
		"resume_value": resumeValue,
	}
	var resp struct {
		Accepted bool `json:"accepted"`
	}
	if err := c.postJSON(ctx, "/v1/messages", body, &resp); err != nil {
		return err
	}
	if !resp.Accepted {
		return fmt.Errorf("resume not accepted")
	}
	return nil
}

// ChildAgentListItem 为 GET child-agents 列表项。
type ChildAgentListItem struct {
	ChildSessionID string `json:"child_session_id"`
	Status         string `json:"status"`
	Purpose        string `json:"purpose"`
	AllowedTools   []string `json:"allowed_tools"`
	CreatedAt      string `json:"created_at"`
	ExpiresAt      string `json:"expires_at"`
	TurnCount      int    `json:"turn_count"`
	MaxTurns       int    `json:"max_turns"`
}

// ListChildAgents 返回父 session 下活跃子 Agent 列表。
func (c *Client) ListChildAgents(ctx context.Context, parentSessionID string) ([]ChildAgentListItem, error) {
	parentSessionID = strings.TrimSpace(parentSessionID)
	var resp struct {
		Items []ChildAgentListItem `json:"items"`
	}
	path := "/v1/sessions/" + url.PathEscape(parentSessionID) + "/child-agents"
	if err := c.getJSON(ctx, path, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// StreamEvents 订阅 GET /v1/streams，按 session_id 过滤（可选），逐条回调 handler。

// handler 返回 false 时提前结束；ctx 取消时退出。
// 异常：非 200、读流错误向上返回。
func (c *Client) StreamEvents(
	ctx context.Context,
	sessionID string,
	lastEventID int,
	handler func(StreamEvent) bool,
) error {
	q := url.Values{}
	if strings.TrimSpace(sessionID) != "" {
		q.Set("session_id", sessionID)
	}
	q.Set("live", "1")
	streamURL := c.base + "/v1/streams"
	if encoded := q.Encode(); encoded != "" {
		streamURL += "?" + encoded
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	if lastEventID > 0 {
		req.Header.Set("Last-Event-ID", strconv.Itoa(lastEventID))
	}

	// SSE 长连接：单独 client，不设总超时。
	streamClient := &http.Client{Timeout: 0}
	resp, err := streamClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /v1/streams: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GET /v1/streams: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return parseSSE(ctx, resp.Body, handler)
}

func (c *Client) postJSON(ctx context.Context, path string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
	}
	return nil
}

func (c *Client) patchJSON(ctx context.Context, path string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.base+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
	}
	return nil
}

func (c *Client) putJSON(ctx context.Context, path string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.base+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
	}
	return nil
}

func parseSSE(ctx context.Context, r io.Reader, handler func(StreamEvent) bool) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var eventType, eventID, dataLine string
	flush := func() error {
		if dataLine == "" {
			eventType, eventID, dataLine = "", "", ""
			return nil
		}
		ev, err := decodeStreamEvent(eventType, eventID, dataLine)
		if err != nil {
			return err
		}
		if !handler(ev) {
			return io.EOF
		}
		eventType, eventID, dataLine = "", "", ""
		return nil
	}

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				if err == io.EOF {
					return nil
				}
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "id:") {
			eventID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		} else if strings.HasPrefix(line, "data:") {
			dataLine = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}

func decodeStreamEvent(eventType, eventID, dataLine string) (StreamEvent, error) {
	var envelope struct {
		SessionID string         `json:"session_id"`
		AgentID   string         `json:"agent_id"`
		Type      string         `json:"type"`
		Seq       int            `json:"seq"`
		Data      map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(dataLine), &envelope); err != nil {
		return StreamEvent{}, fmt.Errorf("decode sse data: %w", err)
	}
	typ := eventType
	if typ == "" {
		typ = envelope.Type
	}
	seq := envelope.Seq
	if seq == 0 && eventID != "" {
		seq, _ = strconv.Atoi(eventID)
	}
	return StreamEvent{
		ID:        eventID,
		Type:      typ,
		SessionID: envelope.SessionID,
		AgentID:   envelope.AgentID,
		Seq:       seq,
		Data:      envelope.Data,
	}, nil
}
