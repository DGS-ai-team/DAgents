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

// ListWorkgroups 列出工作组；subscribedOnly 时带 subscribed_by=本机 node_id。
func (c *ControlClient) ListWorkgroups(ctx context.Context, subscribedOnly bool) ([]WorkgroupListItem, error) {
	path := "/v1/workgroups"
	if subscribedOnly {
		path += "?subscribed_by=" + url.QueryEscape(c.cfg.NodeID)
	}
	var out []WorkgroupListItem
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
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
