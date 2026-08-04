package manage

import (
	"context"
	"fmt"
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
func (c *ControlClient) PostWorkgroupMessage(ctx context.Context, workgroupID, text, clientMessageID string) (map[string]any, error) {
	body := map[string]any{
		"text":          text,
		"from_node_id":  c.cfg.NodeID,
	}
	if strings.TrimSpace(clientMessageID) != "" {
		body["client_message_id"] = clientMessageID
	}
	var out map[string]any
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/messages"
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := c.doJSON(ctx, http.MethodPost, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
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

// GetWorkgroupMemberSpec 读取成员 MemberSpec 快照。
func (c *ControlClient) GetWorkgroupMemberSpec(ctx context.Context, workgroupID, memberID string) (map[string]any, error) {
	var out map[string]any
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/members/" + strings.TrimSpace(memberID) + "/spec"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListWorkgroupGrants 列出 Grant。
func (c *ControlClient) ListWorkgroupGrants(ctx context.Context, workgroupID string) (jsonArray, error) {
	var out jsonArray
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/grants"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// InviteWorkgroupGrant 邀请 ExecutionGrant。
func (c *ControlClient) InviteWorkgroupGrant(ctx context.Context, workgroupID, memberID string, tools []string) (map[string]any, error) {
	body := map[string]any{"member_id": memberID}
	if len(tools) > 0 {
		body["tool_allow_names"] = tools
	}
	var out map[string]any
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/grants"
	if err := c.doJSON(ctx, http.MethodPost, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AcceptWorkgroupGrant home Node 接受 Grant（触发 Manage provision outbox）。
func (c *ControlClient) AcceptWorkgroupGrant(ctx context.Context, workgroupID, grantID, digest string) (map[string]any, error) {
	body := map[string]any{}
	if strings.TrimSpace(digest) != "" {
		body["member_spec_digest"] = digest
	}
	var out map[string]any
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/grants/" + strings.TrimSpace(grantID) + "/accept"
	if err := c.doJSON(ctx, http.MethodPost, path, body, &out); err != nil {
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
