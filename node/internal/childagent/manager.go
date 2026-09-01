package childagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

// Config 为 child_agents YAML 配置（默认值由 shared/config 填充）。
type Config struct {
	Enabled            bool
	DefaultTTLSeconds  int
	MaxTTLSeconds      int
	DefaultMaxTurns    int
	MaxMaxTurns        int
	MaxActivePerParent int
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
	// settledSnapshots 保存当前进程最近终态；完整快照也会写入 repository。
	settledSnapshots map[string]ActiveAgentSnapshot
	repository       RunRepository
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
	return &Manager{
		cfg:               cfg,
		hub:               hub,
		agentID:           agentID,
		logger:            logx.OrDefault(logger),
		activeByID:        make(map[string]*ActiveAgent),
		activeIDsByParent: make(map[string][]string),
		settledSnapshots:  make(map[string]ActiveAgentSnapshot),
	}
}

// SetRunRepository 注入持久化仓库，并把上次进程遗留的 running 记录
// 标记为 interrupted。子 runtime 只存在于内存，进程重启后不能伪装成仍在执行。
func (m *Manager) SetRunRepository(repository RunRepository) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.repository = repository
	m.mu.Unlock()
	if repository == nil {
		return
	}
	records, err := repository.ListChildRuns(context.Background(), "", 1000)
	if err != nil {
		m.logger.Warn("load child runs for recovery failed", "error", err)
		return
	}
	for _, rec := range records {
		if rec.Status != string(StatusCreating) && rec.Status != string(StatusActive) {
			continue
		}
		rec.Status = string(StatusInterrupted)
		rec.Phase = "interrupted"
		rec.Error = "node restarted before child agent completed"
		rec.FinishedAt = time.Now().UTC()
		rec.UpdatedAt = rec.FinishedAt
		rec.Revision++
		progress := Progress{Status: StatusInterrupted, Phase: "interrupted"}
		if len(rec.ProgressJSON) > 0 {
			_ = json.Unmarshal(rec.ProgressJSON, &progress)
		}
		progress.Status = StatusInterrupted
		progress.Phase = "interrupted"
		progress.Error = rec.Error
		progress.PendingApproval = false
		progress.PendingApprovalData = nil
		progress.UpdatedAt = rec.UpdatedAt
		progress.FinishedAt = rec.FinishedAt
		progress.Revision = rec.Revision
		rec.ProgressJSON, _ = json.Marshal(progress)
		if err := repository.SaveChildRun(context.Background(), rec); err != nil {
			m.logger.Warn("mark interrupted child run failed", "child_agent_id", rec.ChildAgentID, "error", err)
		}
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
		m.finishWithEvent(childID, StatusFailed, "", err.Error(), false, string(StatusCreating))
		return "ERROR: " + err.Error(), nil
	}

	// 先把运行记录切到 active 并发布 created，再投递首条 task。这样即使
	// 子任务立即产生进度事件，客户端也已经有稳定的父工具关联。
	agent.mu.Lock()
	agent.Status = StatusActive
	agent.Progress.Status = StatusActive
	agent.Progress.Phase = "queued"
	agent.Progress.MaxTurns = agent.MaxTurns
	agent.Progress.Revision++
	agent.Progress.UpdatedAt = time.Now().UTC()
	agent.mu.Unlock()
	if err := m.persistAgent(agent); err != nil {
		m.logger.Warn("persist child run before execution failed", "child_agent_id", childID, "error", err)
	}
	// 发布创建事件
	m.publishCreated(parentSessionID, agent)
	// 启动 TTL 定时器
	go m.runTTLTimer(childID, time.Duration(input.TTLSeconds)*time.Second)

	// 投递首条 task
	task := FormatChildTask(input.Task)
	if err := m.host.EnqueueChildTask(childID, task); err != nil {
		m.finishWithEvent(childID, StatusFailed, "", "spawn enqueue failed: "+err.Error(), false, string(StatusActive))
		return "ERROR: " + err.Error(), nil
	}

	// 创建工具只有同步语义：父 Turn 在这里等待子 Agent 的终态。
	res, waitErr := m.waitUntilSettled(ctx, agent, time.Duration(input.TTLSeconds)*time.Second+30*time.Second)
	if waitErr != nil {
		return "ERROR: " + waitErr.Error(), nil
	}
	return marshalResult(res), nil
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

// OnChildFailed 由子 runtime 在 LLM、工具链或回合上限异常时调用。
// 所有退出路径都必须进入同一个终态函数，避免 UI/同步调用永久等待。
func (m *Manager) OnChildFailed(childSessionID, errText string, turnCount int) {
	m.mu.Lock()
	agent, ok := m.activeByID[childSessionID]
	if !ok || agent == nil || agent.isTerminal() {
		m.mu.Unlock()
		return
	}
	agent.mu.Lock()
	agent.TurnCount = turnCount
	agent.mu.Unlock()
	m.mu.Unlock()
	if strings.TrimSpace(errText) == "" {
		errText = "child agent execution failed"
	}
	m.finishWithEvent(childSessionID, StatusFailed, "", errText, false, "")
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

// ListSnapshots 返回活跃及最近终态的统一快照。它是 API/hydrate 的唯一读取
// 入口，不再把“活跃列表”和“终态结果缓存”暴露成两个不同投影。
func (m *Manager) ListSnapshots(parentSessionID string) ([]ActiveAgentSnapshot, error) {
	m.mu.Lock()
	seen := make(map[string]struct{})
	out := make([]ActiveAgentSnapshot, 0)
	for id, agent := range m.activeByID {
		if agent == nil || agent.ParentAgentID != parentSessionID {
			continue
		}
		snapshot := agent.Snapshot()
		out = append(out, snapshot)
		seen[id] = struct{}{}
	}
	for id, snapshot := range m.settledSnapshots {
		if snapshot.ParentAgentID != parentSessionID {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		out = append(out, snapshot)
		seen[id] = struct{}{}
	}
	repository := m.repository
	m.mu.Unlock()
	if repository != nil {
		records, err := repository.ListChildRuns(context.Background(), parentSessionID, 256)
		if err != nil {
			return out, err
		}
		for _, record := range records {
			if _, ok := seen[record.ChildAgentID]; ok {
				continue
			}
			snapshot, err := snapshotFromRecord(record)
			if err != nil {
				m.logger.Warn("decode child run progress failed", "child_agent_id", record.ChildAgentID, "error", err)
				continue
			}
			out = append(out, snapshot)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		left := out[i].Progress.UpdatedAt
		right := out[j].Progress.UpdatedAt
		if left.Equal(right) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return left.Before(right)
	})
	return out, nil
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
	agent.Progress.PendingApprovalData = nil
	agent.Progress.Summary = strings.TrimSpace(summary)
	agent.Progress.Error = strings.TrimSpace(errText)
	agent.Progress.TurnCount = agent.TurnCount
	agent.Progress.MaxTurns = agent.MaxTurns
	agent.Progress.Revision++
	agent.Progress.UpdatedAt = time.Now().UTC()
	agent.Progress.FinishedAt = agent.Progress.UpdatedAt
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
	terminalSnapshot := agent.Snapshot()
	m.mu.Unlock()

	m.mu.Lock()
	m.settledSnapshots[childSessionID] = terminalSnapshot
	m.trimSettledLocked(parentID)
	m.mu.Unlock()
	if err := m.persistSnapshot(terminalSnapshot); err != nil {
		m.logger.Warn("persist child terminal state failed", "child_agent_id", childSessionID, "error", err)
	}

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

// unregisterActive 从活跃账本移除临时 Agent；终态快照仍由统一 snapshot/API 提供。
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

// waitUntilSettled 阻塞直到 ActiveAgent 进入终态。创建工具只有同步语义，
// 因此超时也会把本次运行收敛为终态并返回，不会遗留后台句柄。
func (m *Manager) waitUntilSettled(ctx context.Context, agent *ActiveAgent, timeout time.Duration) (Result, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		res := m.finishWithEvent(agent.ChildAgentID, StatusCancelled, "", "parent turn cancelled", true, string(agent.Snapshot().Status))
		return res, nil
	case <-agent.settledCh:
		return agent.resultSnapshot(), nil
	case <-timer.C:
		res, _ := m.Cancel(agent.ParentAgentID, agent.ChildAgentID, "synchronous child execution timeout")
		return res, nil
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

func (m *Manager) publishCreated(parentID string, agent *ActiveAgent) {
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
		"phase":           snapshot.Progress.Phase,
		"revision":        snapshot.Progress.Revision,
		"updated_at":      snapshot.Progress.UpdatedAt,
	}
	if snapshot.ToolCallID != "" {
		payload["tool_call_id"] = snapshot.ToolCallID
	}
	m.hub.Publish(parentID, EventTemporaryAgentCreated, payload)
}

func marshalResult(res Result) string {
	body, _ := json.Marshal(map[string]any{
		"kind":           "result",
		"child_agent_id": res.ChildAgentID,
		"status":         res.Status,
		"summary":        res.Summary,
		"turn_count":     res.TurnCount,
		"artifacts":      res.Artifacts,
		"error":          res.Error,
	})
	return string(body)
}

func (m *Manager) persistAgent(agent *ActiveAgent) error {
	if agent == nil {
		return nil
	}
	return m.persistSnapshot(agent.Snapshot())
}

func (m *Manager) persistSnapshot(snapshot ActiveAgentSnapshot) error {
	m.mu.Lock()
	repository := m.repository
	m.mu.Unlock()
	if repository == nil {
		return nil
	}
	progress, err := json.Marshal(snapshot.Progress)
	if err != nil {
		return err
	}
	return repository.SaveChildRun(context.Background(), RunRecord{
		ChildAgentID:  snapshot.ChildAgentID,
		ParentAgentID: snapshot.ParentAgentID,
		NodeID:        m.agentID,
		ToolCallID:    snapshot.ToolCallID,
		Purpose:       snapshot.Purpose,
		Status:        string(snapshot.Status),
		Phase:         snapshot.Progress.Phase,
		AllowedTools:  snapshot.AllowedTools,
		LoadedSkills:  snapshot.LoadedSkills,
		ProgressJSON:  progress,
		TurnCount:     snapshot.Progress.TurnCount,
		MaxTurns:      snapshot.MaxTurns,
		Summary:       snapshot.Progress.Summary,
		Error:         snapshot.Progress.Error,
		CreatedAt:     snapshot.CreatedAt,
		ExpiresAt:     snapshot.ExpiresAt,
		UpdatedAt:     snapshot.Progress.UpdatedAt,
		FinishedAt:    snapshot.FinishedAt,
		Revision:      snapshot.Progress.Revision,
	})
}

func snapshotFromRecord(record RunRecord) (ActiveAgentSnapshot, error) {
	progress := Progress{}
	if len(record.ProgressJSON) > 0 {
		if err := json.Unmarshal(record.ProgressJSON, &progress); err != nil {
			return ActiveAgentSnapshot{}, err
		}
	}
	status := Status(record.Status)
	progress.Status = status
	if progress.Phase == "" {
		progress.Phase = record.Phase
	}
	if progress.TurnCount == 0 {
		progress.TurnCount = record.TurnCount
	}
	if progress.MaxTurns == 0 {
		progress.MaxTurns = record.MaxTurns
	}
	if progress.Summary == "" {
		progress.Summary = record.Summary
	}
	if progress.Error == "" {
		progress.Error = record.Error
	}
	if progress.UpdatedAt.IsZero() {
		progress.UpdatedAt = record.UpdatedAt
	}
	if progress.FinishedAt.IsZero() {
		progress.FinishedAt = record.FinishedAt
	}
	return ActiveAgentSnapshot{
		ChildAgentID: record.ChildAgentID, ParentAgentID: record.ParentAgentID,
		ToolCallID: record.ToolCallID, Purpose: record.Purpose,
		AllowedTools: append([]string(nil), record.AllowedTools...),
		LoadedSkills: append([]string(nil), record.LoadedSkills...), Status: status,
		CreatedAt: record.CreatedAt, ExpiresAt: record.ExpiresAt,
		MaxTurns: record.MaxTurns, TurnCount: record.TurnCount,
		Progress: progress, FinishedAt: record.FinishedAt,
	}, nil
}

func (m *Manager) trimSettledLocked(parentID string) {
	const maxSettledPerParent = 64
	ids := make([]string, 0)
	for id, snapshot := range m.settledSnapshots {
		if snapshot.ParentAgentID == parentID {
			ids = append(ids, id)
		}
	}
	if len(ids) <= maxSettledPerParent {
		return
	}
	sort.Slice(ids, func(i, j int) bool {
		return m.settledSnapshots[ids[i]].Progress.UpdatedAt.Before(m.settledSnapshots[ids[j]].Progress.UpdatedAt)
	})
	for _, id := range ids[:len(ids)-maxSettledPerParent] {
		delete(m.settledSnapshots, id)
	}
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
