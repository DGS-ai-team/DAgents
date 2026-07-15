package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/a2aclient"
)

const defaultAgentInvokeTimeout = 90 * time.Second

// SetManageRuntime 注入 Manage A2A 客户端；manage.enabled 时由 server 调用。
func (r *Registry) SetManageRuntime(
	client *a2aclient.Client,
	agentID string,
	hitl a2aclient.A2ACallerHITLHandler,
) {
	r.manageClient = client
	r.a2aCallerHITL = hitl
	if id := strings.TrimSpace(agentID); id != "" {
		r.agentID = id
	}
	if client != nil {
		r.handlers["agent_invoke"] = r.execAgentInvoke
		r.handlers["agent_discover"] = r.execAgentDiscover
	}
}

func manageA2AToolDefs() []ToolDef {
	return []ToolDef{
		agentInvokeToolDef(),
		agentDiscoverToolDef(),
	}
}

func agentInvokeToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name: "agent_invoke",
			Description: "经 Manage 向其他 Agent 发起 A2A 协作请求（invoke），等待对端回复后返回 result_text。" +
				"禁止直连其他 Agent Node；目标 Agent 须已在 Manage 注册且 expose_to_peers=true。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"content": map[string]any{
						"type":        "string",
						"description": "发给目标 Agent 的任务正文（必填）",
					},
					"to_agent_id": map[string]any{
						"type":        "string",
						"description": "目标 Agent ID（必填；可先 agent_discover 确认对端）",
					},
					"timeout_seconds": map[string]any{
						"type":        "integer",
						"description": "最长等待对端回复秒数（可选，默认 90）",
					},
				},
				"required":             []string{"content", "to_agent_id"},
				"additionalProperties": false,
			}),
		},
	}
}

func agentDiscoverToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name: "agent_discover",
			Description: "经 Manage 发现可 A2A 协作的对等 Agent 列表（online 且 expose_to_peers=true；响应不含 endpoint/base_url）。" +
				"返回 JSON，含 agent_id、name、capabilities、card 等。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"discovery_group": map[string]any{
						"type":        "string",
						"description": "发现分组（可选；省略时由 Manage 按调用方已分配的 discovery_group 匹配对端）",
					},
				},
				"required":             []string{},
				"additionalProperties": false,
			}),
		},
	}
}

func (r *Registry) requireManageClient() (*a2aclient.Client, error) {
	if r.manageClient == nil {
		return nil, fmt.Errorf("A2A tools require manage.enabled and Manage URL configured")
	}
	return r.manageClient, nil
}

func (r *Registry) execAgentInvoke(ctx context.Context, args json.RawMessage) (string, error) {
	client, err := r.requireManageClient()
	if err != nil {
		return "", err
	}
	var in struct {
		Content        string `json:"content"`
		ToAgentID      string `json:"to_agent_id"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return "", fmt.Errorf("content is required")
	}
	toAgentID := strings.TrimSpace(in.ToAgentID)
	if toAgentID == "" {
		return "", fmt.Errorf("to_agent_id is required")
	}
	timeout := defaultAgentInvokeTimeout
	if in.TimeoutSeconds > 0 {
		timeout = time.Duration(in.TimeoutSeconds) * time.Second
	}
	callerSessionID := sessionIDFromContext(ctx)
	created, err := client.CreateInvokeTask(ctx, toAgentID, content, callerSessionID)
	if err != nil {
		return "", err
	}
	rec, err := client.WaitForInvokeResult(ctx, created.TaskID, callerSessionID, timeout, r.a2aCallerHITL)
	if err != nil {
		return "", err
	}
	return rec.ResultText, nil
}

func (r *Registry) execAgentDiscover(ctx context.Context, args json.RawMessage) (string, error) {
	client, err := r.requireManageClient()
	if err != nil {
		return "", err
	}
	var in struct {
		DiscoveryGroup string `json:"discovery_group"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
	}
	group := strings.TrimSpace(in.DiscoveryGroup)
	resp, err := client.DiscoverAgents(ctx, group)
	if err != nil {
		return "", err
	}
	out, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}
