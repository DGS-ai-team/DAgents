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
	"time"
)

// WorkgroupListResponse 对齐 Manage GET /v1/workgroups。
type WorkgroupListItem struct {
	WorkgroupID       string `json:"workgroup_id"`
	DisplayName       string `json:"display_name"`
	Status            string `json:"status"`
	CreatedByNodeID   string `json:"created_by_node_id"`
	CreatedAt         string `json:"created_at"`
}

// WorkgroupListMode 控制列表过滤。
type WorkgroupListMode string

const (
	WorkgroupListAll        WorkgroupListMode = "all"
	WorkgroupListSubscribed WorkgroupListMode = "subscribed"
	WorkgroupListACL        WorkgroupListMode = "acl"
)

// ListWorkgroups 列出工作组。
func (c *ControlClient) ListWorkgroups(ctx context.Context, mode WorkgroupListMode) ([]WorkgroupListItem, error) {
	path := "/v1/workgroups"
	q := url.Values{}
	switch mode {
	case WorkgroupListSubscribed:
		q.Set("subscribed_by", c.cfg.NodeID)
	case WorkgroupListACL:
		q.Set("acl_member", c.cfg.NodeID)
	}
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var out []WorkgroupListItem
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateWorkgroup 以本机 node_id 建组（自动订阅）。
func (c *ControlClient) CreateWorkgroup(ctx context.Context, displayName string) (map[string]any, error) {
	name := strings.TrimSpace(displayName)
	if name == "" {
		return nil, fmt.Errorf("display_name required")
	}
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodPost, "/v1/workgroups", map[string]any{
		"display_name":        name,
		"created_by_node_id":  c.cfg.NodeID,
		"llm_profile_id":      "default",
		"llm_profile_revision": "1",
	}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetMemberToolCatalog 拉取 Manage 侧成员工具目录（与仓库 JSON 同源；Node WebUI 优先用本地嵌入，不必经此调用）。
func (c *ControlClient) GetMemberToolCatalog(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodGet, "/v1/workgroups/meta/member-tools", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetWorkgroupACL 读取 ACL。
func (c *ControlClient) GetWorkgroupACL(ctx context.Context, workgroupID string) (map[string]any, error) {
	var out map[string]any
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/acl"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// PatchWorkgroupACL 更新 ACL（CAS revision）。
func (c *ControlClient) PatchWorkgroupACL(ctx context.Context, workgroupID string, body map[string]any) (map[string]any, error) {
	var out map[string]any
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/acl"
	if err := c.doJSON(ctx, http.MethodPatch, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AddWorkgroupCollaborator 将 node 加入 collaborators（读 revision 后 CAS）。
func (c *ControlClient) AddWorkgroupCollaborator(ctx context.Context, workgroupID, nodeID string) (map[string]any, error) {
	acl, err := c.GetWorkgroupACL(ctx, workgroupID)
	if err != nil {
		return nil, err
	}
	rev, _ := acl["revision"].(float64)
	collab := stringSlice(acl["collaborators"])
	nid := strings.TrimSpace(nodeID)
	for _, x := range collab {
		if x == nid {
			return acl, nil
		}
	}
	collab = append(collab, nid)
	return c.PatchWorkgroupACL(ctx, workgroupID, map[string]any{
		"collaborators":     collab,
		"expected_revision": int(rev),
	})
}

func stringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		s, _ := item.(string)
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// SubscribeWorkgroup 持久订阅（须在 ACL 内）。
func (c *ControlClient) SubscribeWorkgroup(ctx context.Context, workgroupID string) error {
	wid := strings.TrimSpace(workgroupID)
	if wid == "" {
		return fmt.Errorf("workgroup_id required")
	}
	return c.doJSON(ctx, http.MethodPost, "/v1/workgroups/"+wid+"/subscribe", map[string]any{
		"node_id": c.cfg.NodeID,
	}, nil)
}

// UnsubscribeWorkgroup 取消订阅。
func (c *ControlClient) UnsubscribeWorkgroup(ctx context.Context, workgroupID string) error {
	wid := strings.TrimSpace(workgroupID)
	if wid == "" {
		return fmt.Errorf("workgroup_id required")
	}
	q := url.Values{}
	q.Set("node_id", c.cfg.NodeID)
	return c.doJSON(ctx, http.MethodDelete, "/v1/workgroups/"+wid+"/subscribe?"+q.Encode(), nil, nil)
}

// GetWorkgroupTimeline 拉取公开 Timeline。
func (c *ControlClient) GetWorkgroupTimeline(ctx context.Context, workgroupID string) (jsonArray, error) {
	var out jsonArray
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/timeline"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// PostWorkgroupMessage 以本机 node_id 发言。
// directMemberID 非空时 Manage 走 @直连（跳过 Leader LLM）。
func (c *ControlClient) PostWorkgroupMessage(ctx context.Context, workgroupID, text, clientMessageID, directMemberID string) (map[string]any, error) {
	body := map[string]any{
		"text":         text,
		"from_node_id": c.cfg.NodeID,
	}
	if strings.TrimSpace(clientMessageID) != "" {
		body["client_message_id"] = clientMessageID
	}
	if strings.TrimSpace(directMemberID) != "" {
		body["direct_member_id"] = strings.TrimSpace(directMemberID)
	}
	var out map[string]any
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/messages"
	// Leader assign + Member LLM/tool 可能远超默认 HTTP 超时；过短会导致 Manage 侧留下未配对 tool_call。
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := c.doJSON(ctx, http.MethodPost, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CancelWorkgroupTurn 取消当前工作组活跃 turn（Leader / 直连 Member）。
func (c *ControlClient) CancelWorkgroupTurn(ctx context.Context, workgroupID string) (map[string]any, error) {
	var out map[string]any
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/turn/cancel"
	if err := c.doJSON(ctx, http.MethodPost, path, map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// OpenWorkgroupMessageStream 打开 Manage messages/stream SSE；调用方必须 Close 返回的 Body。
func (c *ControlClient) OpenWorkgroupMessageStream(
	ctx context.Context,
	workgroupID, text, clientMessageID, directMemberID string,
) (*http.Response, error) {
	if !c.enabled() {
		return nil, fmt.Errorf("manage is not enabled")
	}
	body := map[string]any{
		"text":         text,
		"from_node_id": c.cfg.NodeID,
	}
	if strings.TrimSpace(clientMessageID) != "" {
		body["client_message_id"] = clientMessageID
	}
	if strings.TrimSpace(directMemberID) != "" {
		body["direct_member_id"] = strings.TrimSpace(directMemberID)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/messages/stream"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.manageURL(path), bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(agentIDHeader, c.cfg.NodeID)
	if token := strings.TrimSpace(c.cfg.Manage.NodeToken); token != "" {
		req.Header.Set(tokenHeader, token)
	}
	cli := c.streamClient
	if cli == nil {
		cli = &http.Client{Timeout: 0}
	}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("manage stream status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return resp, nil
}

// jsonArray 便于 doJSON 解码任意 JSON 数组。
type jsonArray []map[string]any

// ListWorkgroupMembers 列出成员。
func (c *ControlClient) ListWorkgroupMembers(ctx context.Context, workgroupID string) (jsonArray, error) {
	var out jsonArray
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/members"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateWorkgroupMember 创建成员（home 须在 ACL）。
func (c *ControlClient) CreateWorkgroupMember(ctx context.Context, workgroupID string, body map[string]any) (map[string]any, error) {
	var out map[string]any
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/members"
	if err := c.doJSON(ctx, http.MethodPost, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// PatchWorkgroupMember 更新成员 Spec（会 re-provision）。
func (c *ControlClient) PatchWorkgroupMember(ctx context.Context, workgroupID, memberID string, body map[string]any) (map[string]any, error) {
	var out map[string]any
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/members/" + strings.TrimSpace(memberID)
	if err := c.doJSON(ctx, http.MethodPatch, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetWorkgroupMemberSpec 读取成员 MemberSpec 快照。
func (c *ControlClient) GetWorkgroupMemberSpec(ctx context.Context, workgroupID, memberID string) (map[string]any, error) {
	var out map[string]any
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/members/" + strings.TrimSpace(memberID) + "/spec"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ArchiveWorkgroupMember 归档成员（侧栏删除）。
func (c *ControlClient) ArchiveWorkgroupMember(ctx context.Context, workgroupID, memberID string) (map[string]any, error) {
	var out map[string]any
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/members/" + strings.TrimSpace(memberID) + "/archive"
	if err := c.doJSON(ctx, http.MethodPost, path, map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListWorkgroupHITL 列出 HITL（默认仅 pending）。
func (c *ControlClient) ListWorkgroupHITL(ctx context.Context, workgroupID string, pendingOnly bool) (jsonArray, error) {
	q := url.Values{}
	if pendingOnly {
		q.Set("pending_only", "true")
	} else {
		q.Set("pending_only", "false")
	}
	var out jsonArray
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/hitl?" + q.Encode()
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateWorkgroupHITL 创建信息型 HITL。
func (c *ControlClient) CreateWorkgroupHITL(ctx context.Context, workgroupID, prompt string) (map[string]any, error) {
	var out map[string]any
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/hitl"
	if err := c.doJSON(ctx, http.MethodPost, path, map[string]any{"prompt": prompt}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ResolveWorkgroupHITL CAS 决议 HITL。
func (c *ControlClient) ResolveWorkgroupHITL(ctx context.Context, workgroupID, hitlID string, resolution map[string]any) (map[string]any, error) {
	var out map[string]any
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/hitl/" + strings.TrimSpace(hitlID) + "/resolve"
	if err := c.doJSON(ctx, http.MethodPost, path, map[string]any{"resolution": resolution}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListWorkgroupRuns 列出工作组 ActorRun（含 Supervisor LLM 解析摘要）。
func (c *ControlClient) ListWorkgroupRuns(ctx context.Context, workgroupID, actorID string, limit int) (map[string]any, error) {
	q := url.Values{}
	if strings.TrimSpace(actorID) != "" {
		q.Set("actor_id", strings.TrimSpace(actorID))
	}
	if limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
	}
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/runs"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetWorkgroupRunHistory 取 ActorRunHistory（调试 / mock 可观测）。
func (c *ControlClient) GetWorkgroupRunHistory(ctx context.Context, workgroupID, runID string) (map[string]any, error) {
	var out map[string]any
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/runs/" + strings.TrimSpace(runID) + "/history"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
