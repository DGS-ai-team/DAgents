package childagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

// Config 为 child_agents YAML 配置（默认值由 shared/config 填充）。
type Config struct {
	Enabled                   bool
	DefaultTTLSeconds         int
	MaxTTLSeconds             int
	DefaultMaxTurns           int
	MaxMaxTurns               int
	MaxActivePerParent        int
	DefaultWaitTimeoutSeconds int
}

// Host 由 session.Manager 实现：spawn/stop 子 runtime 与 resume 路由。
type Host interface {
	ParentSessionActive(parentID string) bool
	SpawnChild(spec SpawnSpec) error
	StopChild(childSessionID string)
	EnqueueChildTask(childSessionID, content string) error
	ChildHasPendingHITL(childSessionID string) bool
	DeliverChildResume(childSessionID string, resume map[string]any) error
	ParentHasPendingHITL(parentSessionID string) bool
	DeliverParentResume(parentSessionID string, resume map[string]any) error
}

// SpawnSpec 为创建子 runtime 的参数。
type SpawnSpec struct {
	ChildAgentID  string
	ParentAgentID string
	AllowedTools  []string
	SkillNames    []string
	MaxTurns      int
	Purpose       string
}

// Manager 跟踪子 Agent 记录、交付结果与 TTL。
type Manager struct {
	cfg     Config
	hub     *stream.Hub
	agentID string
	logger  *slog.Logger

	host Host

	mu sync.Mutex
	// activeByID：活跃临时 Agent 账本（child_agent_id → ActiveAgent）。
	activeByID map[string]*ActiveAgent
	// activeIDsByParent：父 session 下仍活跃的 child_agent_id 列表。
	activeIDsByParent map[string][]string
	// childToParent：unregisterActive 后仍保留，供 wait/status 校验归属。
	childToParent map[string]string
	// settledResults：终态快照，unregisterActive 后仍可供 wait 读取。
	settledResults map[string]Result
}

// NewManager 创建子 Agent 管理器；未 BindHost 前 Create 不可用。
func NewManager(cfg Config, hub *stream.Hub, agentID string, logger *slog.Logger) *Manager {
	if cfg.DefaultTTLSeconds <= 0 {
		cfg.DefaultTTLSeconds = 1800
	}
	if cfg.MaxTTLSeconds <= 0 {
		cfg.MaxTTLSeconds = 7200
	}
	if cfg.DefaultMaxTurns <= 0 {
		cfg.DefaultMaxTurns = 20
	}
	if cfg.MaxMaxTurns <= 0 {
		cfg.MaxMaxTurns = 50
	}
	if cfg.MaxActivePerParent <= 0 {
		cfg.MaxActivePerParent = 8
	}
	if cfg.DefaultWaitTimeoutSeconds <= 0 {
		cfg.DefaultWaitTimeoutSeconds = 300
	}
	return &Manager{
		cfg:               cfg,
		hub:               hub,
		agentID:           agentID,
		logger:            logx.OrDefault(logger),
		activeByID:        make(map[string]*ActiveAgent),
		activeIDsByParent: make(map[string][]string),
		childToParent:     make(map[string]string),
		settledResults:    make(map[string]Result),
	}
}

// BindHost 注入 session 宿主（NewServer 在 Manager 创建后调用）。
func (m *Manager) BindHost(host Host) {
	m.host = host
}

// Enabled 是否启用子 Agent 功能。
func (m *Manager) Enabled() bool {
	return m.cfg.Enabled
}

// HandleCreate 实现 create_temporary_agent 工具。
func (m *Manager) HandleCreate(ctx context.Context, parentSessionID, argsJSON string) (string, error) {
	return m.handleCreate(ctx, parentSessionID, argsJSON, "")
}

func (m *Manager) handleCreate(ctx context.Context, parentSessionID, argsJSON, toolCallID string) (string, error) {
	if !m.cfg.Enabled {
		return "", fmt.Errorf("child agents disabled")
	}
	if m.host == nil {
		return "", fmt.Errorf("child agent host not configured")
	}
	input, err := parseCreateInput(argsJSON, m.cfg)
	if err != nil {
		return "ERROR: " + err.Error(), nil
	}
	parentSessionID = strings.TrimSpace(parentSessionID)
	if !m.host.ParentSessionActive(parentSessionID) {
		return "ERROR: parent session not found", nil
	}
	if err := m.checkActiveLimit(parentSessionID); err != nil {
		return "ERROR: " + err.Error(), nil
	}

	allowed, err := resolveAllowedTools(input.AllowedTools)
	if err != nil {
		return "ERROR: " + err.Error(), nil
	}

	childID, err := generateChildAgentID()
	if err != nil {
		return "", err
	}
	expiresAt := time.Now().Add(time.Duration(input.TTLSeconds) * time.Second)
	agent := newActiveAgent(parentSessionID, input, childID, expiresAt)
	agent.AllowedTools = allowed
	agent.ToolCallID = strings.TrimSpace(toolCallID)
	agent.Status = StatusCreating

	m.mu.Lock()
	m.activeByID[childID] = agent
	m.activeIDsByParent[parentSessionID] = append(m.activeIDsByParent[parentSessionID], childID)
	m.childToParent[childID] = parentSessionID
	m.mu.Unlock()

	// 创建子 runtime
	if err := m.host.SpawnChild(SpawnSpec{
		ChildAgentID:  childID,
		ParentAgentID: parentSessionID,
		AllowedTools:  allowed,
		SkillNames:    append([]string(nil), input.SkillNames...),
		MaxTurns:      input.MaxTurns,
		Purpose:       input.Purpose,
	}); err != nil {
		m.unregisterActive(childID)
		return "ERROR: " + err.Error(), nil
	}

	// 投递首条 task
	task := FormatChildTask(input.Task)
	if err := m.host.EnqueueChildTask(childID, task); err != nil {
		m.Cancel(parentSessionID, childID, "spawn enqueue failed")
		return "ERROR: " + err.Error(), nil
	}

	agent.mu.Lock()
	agent.Status = StatusActive
	agent.Progress.Status = StatusActive
	agent.Progress.Phase = "queued"
	agent.Progress.MaxTurns = agent.MaxTurns
	agent.Progress.Revision++
	agent.Progress.UpdatedAt = time.Now().UTC()
	agent.mu.Unlock()
	// 发布创建事件
	m.publishCreated(parentSessionID, agent, input.Wait)
	// 启动 TTL 定时器
	go m.runTTLTimer(childID, time.Duration(input.TTLSeconds)*time.Second)

	// 等待结果
	if input.Wait {
		res, waitErr := m.waitUntilSettled(ctx, agent, time.Duration(input.TTLSeconds)*time.Second+30*time.Second)
		if waitErr != nil {
			return "ERROR: " + waitErr.Error(), nil
		}
		body, _ := json.Marshal(map[string]any{
			"kind":           "result",
			"child_agent_id": res.ChildAgentID,
			"status":         res.Status,
			"summary":        res.Summary,
			"turn_count":     res.TurnCount,
			"artifacts":      res.Artifacts,
			"error":          res.Error,
		})
		return string(body), nil
	}

	// 返回结果
	body, _ := json.Marshal(map[string]any{
		"kind":           "handle",
		"child_agent_id": childID,
		"status":         StatusActive,
		"purpose":        input.Purpose,
		"loaded_skills":  append([]string(nil), input.SkillNames...),
		"expires_at":     expiresAt.Format(time.RFC3339),
		"max_turns":      input.MaxTurns,
	})
	return string(body), nil
}

// OnChildSettled 在子 runtime turn 空闲且无 pending HITL 时调用，尝试完成子 Agent。
func (m *Manager) OnChildSettled(childSessionID, summary string, turnCount int) {
	m.mu.Lock()
	agent, ok := m.activeByID[childSessionID]
	if !ok || agent.isTerminal() {
		m.mu.Unlock()
		return
	}
	agent.mu.Lock()
	agent.TurnCount = turnCount
	agent.mu.Unlock()
	m.mu.Unlock()

	if strings.TrimSpace(summary) == "" {
		summary = "（子 Agent 未产生文本结论）"
	}
	m.finishWithEvent(childSessionID, StatusCompleted, summary, "", false, "")
}

// Cancel 取消子 Agent（工具 / HTTP / TTL / 父 session 级联）。
func (m *Manager) Cancel(parentSessionID, childSessionID, reason string) (Result, error) {
	childSessionID = strings.TrimSpace(childSessionID)
	m.mu.Lock()
	agent, ok := m.activeByID[childSessionID]
	if !ok {
		m.mu.Unlock()
		return Result{}, fmt.Errorf("child_agent_not_found")
	}
	if parentSessionID != "" && agent.ParentAgentID != parentSessionID {
		m.mu.Unlock()
		return Result{}, fmt.Errorf("child_agent_not_found")
	}
	if agent.isTerminal() {
		out := agent.resultSnapshot()
		m.mu.Unlock()
		return out, nil
	}
	prev := agent.Snapshot().Status
	m.mu.Unlock()

	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "cancelled"
	}
	return m.finishWithEvent(childSessionID, StatusCancelled, "", reason, true, string(prev)), nil
}

// GetResult 返回已交付或进行中的结果快照。
func (m *Manager) GetResult(childSessionID string) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if agent, ok := m.activeByID[childSessionID]; ok {
		return agent.resultSnapshot(), nil
	}
	if res, ok := m.settledResults[childSessionID]; ok {
		return res, nil
	}
	return Result{}, fmt.Errorf("child_agent_not_found")
}

// ListActive 返回父 session 下未交付的子 Agent 记录。
func (m *Manager) ListActive(parentSessionID string) []*ActiveAgent {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := m.activeIDsByParent[parentSessionID]
	out := make([]*ActiveAgent, 0, len(ids))
	for _, id := range ids {
		agent := m.activeByID[id]
		if agent == nil || agent.isTerminal() {
			continue
		}
		out = append(out, agent)
	}
	return out
}

// RouteResume 将父 session 收到的 resume 路由到父或子 runtime。
func (m *Manager) RouteResume(parentSessionID string, resume map[string]any) (targetParent bool, err error) {
	if m.host == nil {
		return true, fmt.Errorf("child agent host not configured")
	}
	childID, _ := resume["child_agent_id"].(string)
	childID = strings.TrimSpace(childID)
	if childID == "" {
		return true, m.host.DeliverParentResume(parentSessionID, resume)
	}
	m.mu.Lock()
	agent, ok := m.activeByID[childID]
	if !ok || agent.ParentAgentID != parentSessionID {
		m.mu.Unlock()
		return false, fmt.Errorf("hitl_target_mismatch")
	}
	m.mu.Unlock()
	if !m.host.ChildHasPendingHITL(childID) {
		return false, fmt.Errorf("no_pending_hitl")
	}
	return false, m.host.DeliverChildResume(childID, resume)
}

// CancelAllForParent 父 session 释放/清空时取消其下所有活跃子 Agent。
func (m *Manager) CancelAllForParent(parentSessionID, reason string) {
	if strings.TrimSpace(reason) == "" {
		reason = "parent session released"
	}
	m.mu.Lock()
	ids := append([]string(nil), m.activeIDsByParent[parentSessionID]...)
	m.mu.Unlock()
	for _, id := range ids {
		_, _ = m.Cancel(parentSessionID, id, reason)
	}
}

// finishWithEvent 完成子 Agent 并发布事件。
func (m *Manager) finishWithEvent(childSessionID string, status Status, summary, errText string, cancelledEvent bool, previousStatus string) Result {
	m.mu.Lock()
	agent, ok := m.activeByID[childSessionID]
	if !ok || agent.isTerminal() {
		out := Result{}
		if agent != nil {
			out = agent.resultSnapshot()
		}
		m.mu.Unlock()
		return out
	}
	agent.mu.Lock()
	agent.Status = status
	agent.Progress.Status = status
	agent.Progress.Phase = string(status)
	agent.Progress.PendingApproval = false
	agent.Progress.Summary = strings.TrimSpace(summary)
	agent.Progress.Error = strings.TrimSpace(errText)
	agent.Progress.TurnCount = agent.TurnCount
	agent.Progress.MaxTurns = agent.MaxTurns
	agent.Progress.Revision++
	agent.Progress.UpdatedAt = time.Now().UTC()
	terminalProgress := agent.Progress
	out := Result{
		ChildAgentID: childSessionID,
		Status:       status,
		Summary:      summary,
		TurnCount:    agent.TurnCount,
		Error:        errText,
		Artifacts:    []string{},
	}
	agent.terminalResult = &out
	select {
	case <-agent.settledCh:
	default:
		close(agent.settledCh)
	}
	agent.mu.Unlock()
	parentID := agent.ParentAgentID
	m.mu.Unlock()

	m.mu.Lock()
	m.settledResults[childSessionID] = out
	m.mu.Unlock()

	m.publishProgress(parentID, agent, terminalProgress)
	if cancelledEvent {
		m.publishCancelled(parentID, childSessionID, errText, previousStatus)
	} else {
		m.publishCompleted(parentID, &out)
	}
	if m.host != nil {
		m.host.StopChild(childSessionID)
	}
	m.unregisterActive(childSessionID)
	return out
}

// unregisterActive 从活跃账本移除临时 Agent（终态快照保留在 settledResults）。
func (m *Manager) unregisterActive(childSessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	agent := m.activeByID[childSessionID]
	if agent == nil {
		return
	}
	delete(m.activeByID, childSessionID)
	ids := m.activeIDsByParent[agent.ParentAgentID]
	filtered := ids[:0]
	for _, id := range ids {
		if id != childSessionID {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) == 0 {
		delete(m.activeIDsByParent, agent.ParentAgentID)
	} else {
		m.activeIDsByParent[agent.ParentAgentID] = filtered
	}
}

// waitUntilSettled 阻塞直到 ActiveAgent 进入终态（wait=true 创建路径）。
func (m *Manager) waitUntilSettled(ctx context.Context, agent *ActiveAgent, timeout time.Duration) (Result, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-agent.settledCh:
		return agent.resultSnapshot(), nil
	case <-timer.C:
		_, _ = m.Cancel(agent.ParentAgentID, agent.ChildAgentID, "wait timeout")
		return Result{}, fmt.Errorf("wait timeout")
	}
}

func (m *Manager) checkActiveLimit(parentID string) error {
	active := 0
	m.mu.Lock()
	for _, id := range m.activeIDsByParent[parentID] {
		agent := m.activeByID[id]
		if agent != nil && !agent.isTerminal() {
			active++
		}
	}
	m.mu.Unlock()
	if active >= m.cfg.MaxActivePerParent {
		return fmt.Errorf("max active child agents per parent exceeded (%d)", m.cfg.MaxActivePerParent)
	}
	return nil
}

func (m *Manager) runTTLTimer(childID string, ttl time.Duration) {
	timer := time.NewTimer(ttl)
	defer timer.Stop()
	<-timer.C
	m.mu.Lock()
	agent := m.activeByID[childID]
	if agent == nil || agent.isTerminal() {
		m.mu.Unlock()
		return
	}
	parentID := agent.ParentAgentID
	prev := string(agent.Snapshot().Status)
	m.mu.Unlock()
	m.finishWithEvent(childID, StatusExpired, "", "ttl expired", false, prev)
	m.logger.Info("child agent expired", "child_agent_id", childID, "parent_agent_id", parentID)
}

func (m *Manager) publishCreated(parentID string, agent *ActiveAgent, wait bool) {
	if m.hub == nil {
		return
	}
	snapshot := agent.Snapshot()
	payload := map[string]any{
		"child_agent_id":  snapshot.ChildAgentID,
		"parent_agent_id": parentID,
		"purpose":         snapshot.Purpose,
		"loaded_skills":   append([]string(nil), snapshot.LoadedSkills...),
		"status":          StatusActive,
		"expires_at":      snapshot.ExpiresAt.Format(time.RFC3339),
		"max_turns":       snapshot.MaxTurns,
		"wait":            wait,
		"phase":           snapshot.Progress.Phase,
		"revision":        snapshot.Progress.Revision,
		"updated_at":      snapshot.Progress.UpdatedAt,
	}
	if snapshot.ToolCallID != "" {
		payload["tool_call_id"] = snapshot.ToolCallID
	}
	m.hub.Publish(parentID, EventTemporaryAgentCreated, payload)
}

func (m *Manager) publishCompleted(parentID string, res *Result) {
	if m.hub == nil || res == nil {
		return
	}
	payload := map[string]any{
		"child_agent_id":  res.ChildAgentID,
		"parent_agent_id": parentID,
		"status":          res.Status,
		"summary":         res.Summary,
		"turn_count":      res.TurnCount,
		"error":           res.Error,
		"artifacts":       res.Artifacts,
	}
	m.hub.Publish(parentID, EventTemporaryAgentCompleted, payload)
}

func (m *Manager) publishCancelled(parentID, childID, reason, previous string) {
	if m.hub == nil {
		return
	}
	m.hub.Publish(parentID, EventTemporaryAgentCancelled, map[string]any{
		"child_agent_id":  childID,
		"parent_agent_id": parentID,
		"status":          StatusCancelled,
		"reason":          reason,
		"previous_status": previous,
	})
}

func generateChildAgentID() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate child session id: %w", err)
	}
	return "child-" + hex.EncodeToString(b[:]), nil
}
