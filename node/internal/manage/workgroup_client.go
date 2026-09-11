package manage

import (
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

// WorkgroupListItem 对齐 Manage GET /v1/workgroups 列表项（字段子集，供侧栏/元信息）。
type WorkgroupListItem struct {
	WorkgroupID        string `json:"workgroup_id"`
	DisplayName        string `json:"display_name"`
	Status             string `json:"status"`
	CreatedByNodeID    string `json:"created_by_node_id"`
	CreatedAt          string `json:"created_at"`
	LLMProfileID       string `json:"llm_profile_id,omitempty"`
	LLMProfileRevision string `json:"llm_profile_revision,omitempty"`
}

// CreateWorkgroupInput 为本机建组入参。
type CreateWorkgroupInput struct {
	DisplayName        string
	LLMProfileID       string
	LLMProfileRevision string
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

// pickDefaultManageLLMProfile 从 Manage /v1/llm/configs 选默认档案；失败回退 default@1。
func (c *ControlClient) pickDefaultManageLLMProfile(ctx context.Context) (id, rev string) {
	var configs []map[string]any
	if err := c.doJSON(ctx, http.MethodGet, "/v1/llm/configs", nil, &configs); err != nil || len(configs) == 0 {
		return "default", "1"
	}
	var preferred map[string]any
	for _, cfg := range configs {
		if b, _ := cfg["is_default"].(bool); b {
			preferred = cfg
			break
		}
	}
	if preferred == nil {
		preferred = configs[0]
	}
	id, _ = preferred["id"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		id, _ = preferred["name"].(string)
		id = strings.TrimSpace(id)
	}
	if id == "" {
		return "default", "1"
	}
	return id, "1"
}

// CreateWorkgroup 以本机 node_id 建组（自动订阅）。未指定 LLM 时尝试取 Manage 默认档案。
func (c *ControlClient) CreateWorkgroup(ctx context.Context, in CreateWorkgroupInput) (map[string]any, error) {
	name := strings.TrimSpace(in.DisplayName)
	if name == "" {
		return nil, fmt.Errorf("display_name required")
	}
	llmID := strings.TrimSpace(in.LLMProfileID)
	llmRev := strings.TrimSpace(in.LLMProfileRevision)
	if llmID == "" {
		llmID, llmRev = c.pickDefaultManageLLMProfile(ctx)
	}
	if llmRev == "" {
		llmRev = "1"
	}
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodPost, "/v1/workgroups", map[string]any{
		"display_name":         name,
		"created_by_node_id":   c.cfg.NodeID,
		"llm_profile_id":       llmID,
		"llm_profile_revision": llmRev,
	}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetWorkgroup 读取单个工作组。
func (c *ControlClient) GetWorkgroup(ctx context.Context, workgroupID string) (map[string]any, error) {
	var out map[string]any
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// PatchWorkgroup 更新展示名 / Supervisor LLM。
func (c *ControlClient) PatchWorkgroup(ctx context.Context, workgroupID string, body map[string]any) (map[string]any, error) {
	var out map[string]any
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID)
	if err := c.doJSON(ctx, http.MethodPatch, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// PublishWorkgroup configuring → active。
func (c *ControlClient) PublishWorkgroup(ctx context.Context, workgroupID string) (map[string]any, error) {
	var out map[string]any
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/publish"
	if err := c.doJSON(ctx, http.MethodPost, path, map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListWorkgroupLLMConfigs 列出可绑到该工作组的 Manage LLM 档案（已脱敏）。
func (c *ControlClient) ListWorkgroupLLMConfigs(ctx context.Context, workgroupID string) (jsonArray, error) {
	var out jsonArray
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/llm-configs"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
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
	nid := strings.TrimSpace(nodeID)
	for _, x := range stringSlice(acl["owners"]) {
		if x == nid {
			return acl, nil
		}
	}
	collab := stringSlice(acl["collaborators"])
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
func (c *ControlClient) GetWorkgroupTimeline(ctx context.Context, workgroupID string, limit ...int) (jsonArray, error) {
	var out jsonArray
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/timeline"
	if len(limit) > 0 && limit[0] > 0 {
		path += "?limit=" + strconv.Itoa(limit[0])
	}
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

// ListRegisteredAgents returns the Manage catalog of existing Agents. The
// response is intentionally metadata-only; execution still happens on the
// owning Node through the outbound Workgroup WS.
func (c *ControlClient) ListRegisteredAgents(ctx context.Context) (jsonArray, error) {
	var out struct {
		Agents jsonArray `json:"agents"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/v1/registry/agents?status=online&page_size=200", nil, &out); err != nil {
		return nil, err
	}
	agents := make(jsonArray, 0, len(out.Agents))
	for _, item := range out.Agents {
		// The registry also contains one record for the hosting
		// Node itself.  Workgroup AgentRef must expose only actual local Agent
		// records; selecting the Node row would fail Node AgentStore.Get().
		if strings.TrimSpace(payloadString(item["agent_id"])) == strings.TrimSpace(payloadString(item["node_id"])) {
			continue
		}
		agents = append(agents, item)
	}
	return agents, nil
}

func payloadString(value any) string {
	s, _ := value.(string)
	return s
}

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

// ListWorkgroupHumanQueue 列出工作组排队中的 human 消息。
func (c *ControlClient) ListWorkgroupHumanQueue(ctx context.Context, workgroupID string) (map[string]any, error) {
	var out map[string]any
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/human-queue"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// PatchWorkgroupHumanQueueItem 修改排队消息正文。
func (c *ControlClient) PatchWorkgroupHumanQueueItem(ctx context.Context, workgroupID, queueID, text string) (map[string]any, error) {
	var out map[string]any
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/human-queue/" + strings.TrimSpace(queueID)
	if err := c.doJSON(ctx, http.MethodPatch, path, map[string]any{"text": text}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CancelWorkgroupHumanQueueItem 取消排队消息。
func (c *ControlClient) CancelWorkgroupHumanQueueItem(ctx context.Context, workgroupID, queueID string) (map[string]any, error) {
	var out map[string]any
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/human-queue/" + strings.TrimSpace(queueID)
	if err := c.doJSON(ctx, http.MethodDelete, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SendWorkgroupHumanQueueItemNow cancels the active turn and promotes one queued message.
func (c *ControlClient) SendWorkgroupHumanQueueItemNow(ctx context.Context, workgroupID, queueID string) (map[string]any, error) {
	var out map[string]any
	path := "/v1/workgroups/" + strings.TrimSpace(workgroupID) + "/human-queue/" + strings.TrimSpace(queueID) + "/send-now"
	if err := c.doJSON(ctx, http.MethodPost, path, map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out, nil
}
