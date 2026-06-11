// Package a2aclient 提供 Node 作为 A2A 调用方访问 Manage Task API 的 HTTP 客户端。
package a2aclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

const (
	agentIDHeader = "x-dagents-agent-id"
	tokenHeader   = "x-dagents-a2a-token"

	defaultTaskPollInterval = 2 * time.Second
)

// Client 向 Manage 创建 A2A Task 并轮询结果。
type Client struct {
	cfg    *config.Config
	client *http.Client
}

// CreateResponse 为 POST /v1/a2a/tasks 响应体。
type CreateResponse struct {
	TaskID    string `json:"task_id"`
	Status    string `json:"status"`
	ToAgentID string `json:"to_agent_id"`
}

// TaskRecord 为 GET /v1/a2a/tasks/{id} 中的 task 字段。
type TaskRecord struct {
	TaskID          string `json:"task_id"`
	Status          string `json:"status"`
	ResultText      string `json:"result_text"`
	ErrorDetail     string `json:"error_detail"`
	ResultStatus    string `json:"result_status"`
	CalleeSessionID string `json:"callee_session_id"`
	CallerSessionID string `json:"caller_session_id"`
}

// New 创建 Manage A2A Task HTTP 客户端。
func New(cfg *config.Config) *Client {
	return &Client{
		cfg: cfg,
		client: &http.Client{
			Timeout: 2 * time.Minute,
		},
	}
}

func (c *Client) manageURL(path string) string {
	base := strings.TrimRight(strings.TrimSpace(c.cfg.Manage.URL), "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

func (c *Client) doJSON(ctx context.Context, method, rawURL string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(agentIDHeader, c.cfg.AgentID)
	if token := strings.TrimSpace(c.cfg.Manage.NodeToken); token != "" {
		req.Header.Set(tokenHeader, token)
	}
	return c.client.Do(req)
}

// CreateInvokeTask 创建 kind=invoke 的 A2A Task。
func (c *Client) CreateInvokeTask(ctx context.Context, toAgentID, content, callerSessionID string) (CreateResponse, error) {
	toAgentID = strings.TrimSpace(toAgentID)
	if toAgentID == "" {
		return CreateResponse{}, fmt.Errorf("to_agent_id is required")
	}
	payload := map[string]string{
		"from_agent_id":     c.cfg.AgentID,
		"to_agent_id":       toAgentID,
		"kind":              "invoke",
		"content":           content,
		"caller_session_id": strings.TrimSpace(callerSessionID),
	}
	resp, err := c.doJSON(ctx, http.MethodPost, c.manageURL("/v1/a2a/tasks"), payload)
	if err != nil {
		return CreateResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CreateResponse{}, fmt.Errorf("create task status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}
	var out CreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return CreateResponse{}, fmt.Errorf("decode create task response: %w", err)
	}
	if strings.TrimSpace(out.TaskID) == "" {
		return CreateResponse{}, fmt.Errorf("create task: empty task_id")
	}
	return out, nil
}

// DiscoverAgent 为 GET /v1/registry/agents/discover 单条记录（不含 base_url）。
type DiscoverAgent struct {
	AgentID        string         `json:"agent_id"`
	DiscoveryGroup []string       `json:"discovery_group"`
	Capabilities   []string       `json:"capabilities"`
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	Team           string         `json:"team"`
	Version        string         `json:"version"`
	Card           map[string]any `json:"card"`
}

// DiscoverResponse 为 discover API 响应体。
type DiscoverResponse struct {
	Agents []DiscoverAgent `json:"agents"`
}

// DiscoverAgents 查询 Manage 上可 A2A 投递的 online Agent（expose_to_peers=true）。
func (c *Client) DiscoverAgents(ctx context.Context, discoveryGroup string) (DiscoverResponse, error) {
	q := url.Values{}
	if g := strings.TrimSpace(discoveryGroup); g != "" {
		q.Set("discovery_group", g)
	}
	rawURL := c.manageURL("/v1/registry/agents/discover")
	if enc := q.Encode(); enc != "" {
		rawURL += "?" + enc
	}
	resp, err := c.doJSON(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return DiscoverResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return DiscoverResponse{}, fmt.Errorf("discover status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}
	var out DiscoverResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return DiscoverResponse{}, fmt.Errorf("decode discover response: %w", err)
	}
	return out, nil
}

// GetTask 查询 Task 状态与结果。
func (c *Client) GetTask(ctx context.Context, taskID string) (TaskRecord, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return TaskRecord{}, fmt.Errorf("task_id is required")
	}
	q := url.Values{}
	q.Set("caller_agent_id", c.cfg.AgentID)
	rawURL := c.manageURL("/v1/a2a/tasks/"+taskID) + "?" + q.Encode()
	resp, err := c.doJSON(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return TaskRecord{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TaskRecord{}, fmt.Errorf("get task status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}
	var wrapped struct {
		Task TaskRecord `json:"task"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapped); err != nil {
		return TaskRecord{}, fmt.Errorf("decode get task response: %w", err)
	}
	return wrapped.Task, nil
}

// WaitForCompletion 轮询直至 Task 进入终态；成功时返回 TaskRecord（含 result_text）。
func (c *Client) WaitForCompletion(ctx context.Context, taskID string, timeout time.Duration) (TaskRecord, error) {
	return c.waitForInvokeResult(ctx, taskID, timeout, "", nil)
}

// A2ACallerHITLHandler 在 Task 进入 awaiting_caller 时将 HITL 中继至 caller session。
type A2ACallerHITLHandler interface {
	WaitCallerHITL(ctx context.Context, callerSessionID, taskID string, hitlPayload map[string]any) (map[string]any, error)
}

// WaitForInvokeResult 轮询 invoke Task；awaiting_caller 时经 handler 收集 caller resume 并提交 Manage。
func (c *Client) WaitForInvokeResult(
	ctx context.Context,
	taskID, callerSessionID string,
	timeout time.Duration,
	hitl A2ACallerHITLHandler,
) (TaskRecord, error) {
	return c.waitForInvokeResult(ctx, taskID, timeout, callerSessionID, hitl)
}

func (c *Client) waitForInvokeResult(
	ctx context.Context,
	taskID string,
	timeout time.Duration,
	callerSessionID string,
	hitl A2ACallerHITLHandler,
) (TaskRecord, error) {
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	deadline := time.Now().Add(timeout)
	var last TaskRecord
	for {
		rec, err := c.GetTask(ctx, taskID)
		if err != nil {
			return TaskRecord{}, err
		}
		last = rec
		switch rec.Status {
		case "completed":
			return rec, nil
		case "failed", "expired":
			msg := strings.TrimSpace(rec.ErrorDetail)
			if msg == "" {
				msg = rec.Status
			}
			return rec, fmt.Errorf("task %s: %s", taskID, msg)
		case "awaiting_caller":
			if hitl == nil {
				return rec, fmt.Errorf("task %s awaiting caller input", taskID)
			}
			callerSessionID = strings.TrimSpace(callerSessionID)
			if callerSessionID == "" {
				callerSessionID = strings.TrimSpace(rec.CallerSessionID)
			}
			payload, err := ParseRequiresInputPayload(rec.ResultText)
			if err != nil {
				return rec, err
			}
			resume, err := hitl.WaitCallerHITL(ctx, callerSessionID, taskID, payload)
			if err != nil {
				return rec, err
			}
			if err := c.SubmitCallerResume(ctx, taskID, resume); err != nil {
				return rec, err
			}
		}
		if time.Now().After(deadline) {
			return last, fmt.Errorf("task %s: poll timeout after %s (last status=%s)", taskID, timeout, last.Status)
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(defaultTaskPollInterval):
		}
	}
}

// ParseRequiresInputPayload 解析 callee requires_input 的 result_text JSON。
func ParseRequiresInputPayload(resultText string) (map[string]any, error) {
	raw := strings.TrimSpace(resultText)
	if raw == "" {
		return nil, fmt.Errorf("empty requires_input payload")
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("parse requires_input payload: %w", err)
	}
	return out, nil
}

// SubmitCallerResume 提交 caller 侧 HITL resume。
func (c *Client) SubmitCallerResume(ctx context.Context, taskID string, resume map[string]any) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return fmt.Errorf("task_id is required")
	}
	resp, err := c.doJSON(ctx, http.MethodPost, c.manageURL("/v1/a2a/tasks/"+taskID+"/caller_resume"), map[string]any{
		"caller_agent_id": c.cfg.AgentID,
		"resume_value":    resume,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("caller_resume status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}
	return nil
}

func readErrorBody(r io.Reader) string {
	const limit = 512
	b, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil || len(b) == 0 {
		return ""
	}
	return strings.TrimSpace(string(b))
}
