// Package api 提供 Agent Node HTTP/SSE 客户端（Agent、message、stream）。
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
	ID      string
	Type    string
	AgentID string
	Seq     int
	Data    map[string]any
}

// EnsureAgent 调用 POST /v1/agents/{id}/ensure。
func (c *Client) EnsureAgent(ctx context.Context, agentID string) error {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return fmt.Errorf("agent_id is required")
	}
	path := "/v1/agents/" + url.PathEscape(id) + "/ensure"
	var resp struct {
		OK      bool   `json:"ok"`
		AgentID string `json:"agent_id"`
	}
	if err := c.postJSON(ctx, path, map[string]any{}, &resp); err != nil {
		return err
	}
	if !resp.OK && strings.TrimSpace(resp.AgentID) == "" {
		return fmt.Errorf("ensure agent failed")
	}
	return nil
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

// AgentSummary 为 GET /v1/agents 列表项。
type AgentSummary struct {
	AgentID       string `json:"agent_id"`
	SessionID     string `json:"session_id,omitempty"` // 兼容旧字段；等于 AgentID
	Active        bool   `json:"active"`
	HasActiveTurn bool   `json:"has_active_turn"`
	RunTurnPhase  string `json:"run_turn_phase"`
	UpdatedAt     string `json:"updated_at"`
	MessageCount  int    `json:"message_count,omitempty"`
	QueuePending  int    `json:"queue_pending,omitempty"`
}

// ListAgents 调用 GET /v1/agents。
func (c *Client) ListAgents(ctx context.Context) ([]AgentSummary, error) {
	var resp struct {
		Agents []struct {
			AgentID       string `json:"agent_id"`
			Active        bool   `json:"active"`
			HasActiveTurn bool   `json:"has_active_turn"`
			RunTurnPhase  string `json:"run_turn_phase"`
			UpdatedAt     string `json:"updated_at"`
		} `json:"agents"`
	}
	if err := c.getJSON(ctx, "/v1/agents", &resp); err != nil {
		return nil, err
	}
	out := make([]AgentSummary, 0, len(resp.Agents))
	for _, a := range resp.Agents {
		id := strings.TrimSpace(a.AgentID)
		out = append(out, AgentSummary{
			AgentID:       id,
			SessionID:     id,
			Active:        a.Active,
			HasActiveTurn: a.HasActiveTurn,
			RunTurnPhase:  a.RunTurnPhase,
			UpdatedAt:     a.UpdatedAt,
		})
	}
	return out, nil
}

// AgentContext 为 GET /v1/agents/{id}/context 响应。
type AgentContext struct {
	AgentID               string `json:"agent_id"`
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

// CompressContextResult 为 POST /v1/agents/{id}/compress 响应。
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
	Mode       string `json:"mode"`
	Decision   string `json:"decision,omitempty"`
	Configured bool   `json:"configured"`
}

// PolicyShellEntry 为 shell 命令策略条目。
type PolicyShellEntry struct {
	Command    string `json:"command"`
	Mode       string `json:"mode"`
	Decision   string `json:"decision,omitempty"`
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
	Mode     string `json:"mode,omitempty"`
	Decision string `json:"decision,omitempty"`
}

// PolicyShellUpdate 为 PUT /v1/policy/shell/{type} 单项。
type PolicyShellUpdate struct {
	Command  string `json:"command"`
	Mode     string `json:"mode,omitempty"`
	Decision string `json:"decision,omitempty"`
}

// GetAgentInfo 调用 GET /v1/agent/info。
func (c *Client) GetAgentInfo(ctx context.Context) (*AgentInfo, error) {
	var info AgentInfo
	if err := c.getJSON(ctx, "/v1/agent/info", &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// AgentUpdateStatus 为 GET /v1/agent/update 响应（Manage Release Hub 摘要）。
type AgentUpdateStatus struct {
	CurrentVersion   string         `json:"current_version"`
	LatestVersion    string         `json:"latest_version"`
	UpgradeAvailable bool           `json:"upgrade_available"`
	ManageReachable  bool           `json:"manage_reachable"`
	LastCheckedAt    string         `json:"last_checked_at,omitempty"`
	Channel          string         `json:"channel"`
	Platform         string         `json:"platform"`
	ReleaseNotes     string         `json:"release_notes,omitempty"`
	Message          string         `json:"message,omitempty"`
	ApplyCommand     string         `json:"apply_command"`
	Asset            map[string]any `json:"asset,omitempty"`
	Deprecated       bool           `json:"deprecated,omitempty"`
	Delegate         string         `json:"delegate,omitempty"`
	DesktopAPI       string         `json:"desktop_api,omitempty"`
}

// GetAgentUpdate 调用 GET /v1/agent/update。
func (c *Client) GetAgentUpdate(ctx context.Context) (*AgentUpdateStatus, error) {
	var status AgentUpdateStatus
	if err := c.getJSON(ctx, "/v1/agent/update", &status); err != nil {
		return nil, err
	}
	return &status, nil
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

// GetAgentContext 调用 GET /v1/agents/{id}/context。
func (c *Client) GetAgentContext(ctx context.Context, agentID string) (*AgentContext, error) {
	var ctxBody AgentContext
	path := "/v1/agents/" + url.PathEscape(strings.TrimSpace(agentID)) + "/context"
	if err := c.getJSON(ctx, path, &ctxBody); err != nil {
		return nil, err
	}
	return &ctxBody, nil
}

// TranscriptEntry 为 hydrate transcript 单条（与 Node JSON 对齐）。
type TranscriptEntry map[string]any

// AgentHydrate 为 GET /v1/agents/{id}/hydrate 响应。
type AgentHydrate struct {
	AgentID         string            `json:"agent_id"`
	RunTurnPhase    string            `json:"run_turn_phase"`
	HasActiveTurn   bool              `json:"has_active_turn"`
	QueuePending    int               `json:"queue_pending"`
	Transcript      []TranscriptEntry `json:"transcript"`
	PendingHITL     map[string]any    `json:"pending_hitl"`
	PendingA2ARelay map[string]any    `json:"pending_a2a_relay,omitempty"`
	SSESeqHint      int               `json:"sse_seq_hint"`
	NotifySeq       int               `json:"notify_seq"`
	AckSeq          int               `json:"ack_seq"`
	HasUnread       bool              `json:"has_unread"`
}

// GetAgentHydrate 调用 GET /v1/agents/{id}/hydrate。
func (c *Client) GetAgentHydrate(ctx context.Context, agentID string) (*AgentHydrate, error) {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return nil, fmt.Errorf("agent_id is required")
	}
	var out AgentHydrate
	path := "/v1/agents/" + url.PathEscape(id) + "/hydrate"
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PostAgentAck 调用 POST /v1/agents/{id}/ack（IM cursor）。
func (c *Client) PostAgentAck(ctx context.Context, agentID string, sseSeq int) error {
	id := strings.TrimSpace(agentID)
	if id == "" || sseSeq <= 0 {
		return nil
	}
	path := "/v1/agents/" + url.PathEscape(id) + "/ack"
	return c.postJSON(ctx, path, map[string]any{"sse_seq": sseSeq}, nil)
}

// CancelTurn 调用 POST /v1/agents/{id}/cancel，取消在途 turn。
func (c *Client) CancelTurn(ctx context.Context, agentID string) (bool, error) {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return false, fmt.Errorf("agent_id is required")
	}
	path := "/v1/agents/" + url.PathEscape(id) + "/cancel"
	var resp struct {
		AgentID   string `json:"agent_id"`
		Cancelled bool   `json:"cancelled"`
	}
	if err := c.postJSON(ctx, path, map[string]any{}, &resp); err != nil {
		return false, err
	}
	return resp.Cancelled, nil
}

// ClearAgentContext 调用 POST /v1/agents/{id}/clear-context。
func (c *Client) ClearAgentContext(ctx context.Context, agentID string) error {
	path := "/v1/agents/" + url.PathEscape(strings.TrimSpace(agentID)) + "/clear-context"
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

// UpdateShellPolicy 调用 PUT /v1/policy/shell/{shellType}；deletes 移除显式条目（未列出命令默认需审批）。
func (c *Client) UpdateShellPolicy(ctx context.Context, shellType string, updates []PolicyShellUpdate, deletes ...string) error {
	path := "/v1/policy/shell/" + url.PathEscape(strings.TrimSpace(shellType))
	body := map[string]any{"updates": updates}
	if len(deletes) > 0 {
		body["deletes"] = deletes
	}
	return c.putJSON(ctx, path, body, nil)
}

// CompressAgentContext 调用 POST /v1/agents/{id}/compress，手动触发阻塞压缩。
func (c *Client) CompressAgentContext(ctx context.Context, agentID string) (*CompressContextResult, error) {
	var out CompressContextResult
	path := "/v1/agents/" + url.PathEscape(strings.TrimSpace(agentID)) + "/compress"
	if err := c.postJSON(ctx, path, map[string]any{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteAgent 调用 DELETE /v1/agents/{id}。
func (c *Client) DeleteAgent(ctx context.Context, agentID string) error {
	path := "/v1/agents/" + url.PathEscape(strings.TrimSpace(agentID))
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

// AgentSkills 为 skills 查询/变更响应。
type AgentSkills struct {
	AgentID         string `json:"agent_id"`
	LoadedSkills    []any  `json:"loaded_skills"`
	AvailableSkills []any  `json:"available_skills"`
}

// ListAgentSkills 调用 GET /v1/agents/{id}/skills。
func (c *Client) ListAgentSkills(ctx context.Context, agentID string) (*AgentSkills, error) {
	var out AgentSkills
	path := "/v1/agents/" + url.PathEscape(strings.TrimSpace(agentID)) + "/skills"
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// LoadAgentSkill 调用 POST /v1/agents/{id}/skills/load。
func (c *Client) LoadAgentSkill(ctx context.Context, agentID, skillName string) (*AgentSkills, error) {
	path := "/v1/agents/" + url.PathEscape(strings.TrimSpace(agentID)) + "/skills/load"
	var out AgentSkills
	if err := c.postJSON(ctx, path, map[string]any{"skill_name": skillName}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UnloadAgentSkill 调用 POST /v1/agents/{id}/skills/unload。
func (c *Client) UnloadAgentSkill(ctx context.Context, agentID, skillName string) (*AgentSkills, error) {
	path := "/v1/agents/" + url.PathEscape(strings.TrimSpace(agentID)) + "/skills/unload"
	var out AgentSkills
	if err := c.postJSON(ctx, path, map[string]any{"skill_name": skillName}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ManagePackageUploadResult 为 Node 代传 Manage 上传的结果。
type ManagePackageUploadResult struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Version string `json:"version"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// UploadSkillToManage 调用 POST /v1/manage/upload/skill。
func (c *Client) UploadSkillToManage(ctx context.Context, path, skillID, version, name string, publish bool) (*ManagePackageUploadResult, error) {
	var out ManagePackageUploadResult
	err := c.postJSON(ctx, "/v1/manage/upload/skill", map[string]any{
		"path": path, "skill_id": skillID, "version": version, "name": name, "publish": publish,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// UploadExternalToolToManage 调用 POST /v1/manage/upload/externaltool。
func (c *Client) UploadExternalToolToManage(ctx context.Context, path, toolID, version, name, platform string, publish bool) (*ManagePackageUploadResult, error) {
	var out ManagePackageUploadResult
	err := c.postJSON(ctx, "/v1/manage/upload/externaltool", map[string]any{
		"path": path, "tool_id": toolID, "version": version, "name": name,
		"platform": platform, "publish": publish,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// UploadPluginToManage 调用 POST /v1/manage/upload/plugin。
func (c *Client) UploadPluginToManage(ctx context.Context, path, pluginID, version, name, platform string, publish bool) (*ManagePackageUploadResult, error) {
	var out ManagePackageUploadResult
	err := c.postJSON(ctx, "/v1/manage/upload/plugin", map[string]any{
		"path": path, "plugin_id": pluginID, "version": version, "name": name,
		"platform": platform, "publish": publish,
	}, &out)
	if err != nil {
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

func (c *Client) SubmitMessage(ctx context.Context, agentID, content string) error {
	return c.SubmitUserMessage(ctx, agentID, content, nil)
}

// ContentPart 为 POST /v1/messages 多模态 content_parts 项。
type ContentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *ImageURLPart `json:"image_url,omitempty"`
}

// ImageURLPart 为 image_url 载荷。
type ImageURLPart struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

// SubmitUserMessage 调用 POST /v1/messages；contentParts 非空时发送多模态 user 消息。
func (c *Client) SubmitUserMessage(ctx context.Context, agentID, content string, contentParts []ContentPart) error {
	body := map[string]any{
		"agent_id":     agentID,
		"request_type": "message",
		"content":      content,
	}
	if len(contentParts) > 0 {
		body["content_parts"] = contentParts
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
func (c *Client) SubmitResume(ctx context.Context, agentID string, resumeValue map[string]any) error {
	body := map[string]any{
		"agent_id":     agentID,
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
	ChildAgentID string   `json:"child_agent_id"`
	Status       string   `json:"status"`
	Purpose      string   `json:"purpose"`
	AllowedTools []string `json:"allowed_tools"`
	CreatedAt    string   `json:"created_at"`
	ExpiresAt    string   `json:"expires_at"`
	TurnCount    int      `json:"turn_count"`
	MaxTurns     int      `json:"max_turns"`
}

// ListChildAgents 返回父 Agent 下活跃子 Agent 列表。
func (c *Client) ListChildAgents(ctx context.Context, parentAgentID string) ([]ChildAgentListItem, error) {
	parentAgentID = strings.TrimSpace(parentAgentID)
	var resp struct {
		Items []ChildAgentListItem `json:"items"`
	}
	path := "/v1/agents/" + url.PathEscape(parentAgentID) + "/child-agents"
	if err := c.getJSON(ctx, path, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// StreamEvents 订阅 GET /v1/streams，按 agent_id 过滤（可选），逐条回调 handler。
// handler 返回 false 时提前结束；ctx 取消时退出。
// 异常：非 200、读流错误向上返回。
func (c *Client) StreamEvents(
	ctx context.Context,
	agentID string,
	lastEventID int,
	handler func(StreamEvent) bool,
) error {
	q := url.Values{}
	if strings.TrimSpace(agentID) != "" {
		q.Set("agent_id", agentID)
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
		AgentID string         `json:"agent_id"`
		Type    string         `json:"type"`
		Seq     int            `json:"seq"`
		Data    map[string]any `json:"data"`
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
		ID:      eventID,
		Type:    typ,
		AgentID: envelope.AgentID,
		Seq:     seq,
		Data:    envelope.Data,
	}, nil
}
