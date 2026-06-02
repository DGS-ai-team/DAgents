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
	ChildSessionID  string
	ParentSessionID string
	AllowedTools    []string
	MaxTurns        int
	Purpose         string
	TemplateID      string
	Record          *Record
}

// Manager 跟踪子 Agent 记录、交付结果与 TTL。
type Manager struct {
	cfg     Config
	hub     *stream.Hub
	agentID string
	logger  *slog.Logger

	host Host

	mu       sync.Mutex
	records  map[string]*Record
	byParent map[string][]string
	// parentOf 在 removeRecord 后仍保留，供 wait_child_agents / status 校验归属。
	parentOf map[string]string
	// delivered 保存终态快照，供异步 wait 在记录回收后读取。
	delivered map[string]Result
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
		cfg:      cfg,
		hub:      hub,
		agentID:  agentID,
		logger:   logx.OrDefault(logger),
		records:   make(map[string]*Record),
		byParent:  make(map[string][]string),
		parentOf:  make(map[string]string),
		delivered: make(map[string]Result),
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

	tmpl, err := LookupTemplate(input.TemplateID)
	if err != nil {
		return "ERROR: " + err.Error(), nil
	}
	allowed, err := resolveAllowedTools(tmpl, input.AllowedTools)
	if err != nil {
		return "ERROR: " + err.Error(), nil
	}

	childID, err := generateChildSessionID()
	if err != nil {
		return "", err
	}
	expiresAt := time.Now().Add(time.Duration(input.TTLSeconds) * time.Second)
	rec := newRecord(parentSessionID, input, childID, expiresAt)
	rec.TemplateID = tmpl.ID
	rec.AllowedTools = allowed
	rec.Status = StatusCreating

	m.mu.Lock()
	m.records[childID] = rec
	m.byParent[parentSessionID] = append(m.byParent[parentSessionID], childID)
	m.parentOf[childID] = parentSessionID
	m.mu.Unlock()

	if err := m.host.SpawnChild(SpawnSpec{
		ChildSessionID:  childID,
		ParentSessionID: parentSessionID,
		AllowedTools:    allowed,
		MaxTurns:        input.MaxTurns,
		Purpose:         input.Purpose,
		TemplateID:      tmpl.ID,
		Record:          rec,
	}); err != nil {
		m.removeRecord(childID)
		return "ERROR: " + err.Error(), nil
	}

	task := FormatTask(tmpl, input.Task)
	if err := m.host.EnqueueChildTask(childID, task); err != nil {
		m.Cancel(parentSessionID, childID, "spawn enqueue failed")
		return "ERROR: " + err.Error(), nil
	}

	rec.mu.Lock()
	rec.Status = StatusActive
	rec.mu.Unlock()

	m.publishCreated(parentSessionID, rec, input.Wait)
	go m.runTTLTimer(childID, time.Duration(input.TTLSeconds)*time.Second)

	if input.Wait {
		res, waitErr := m.waitRecord(ctx, rec, time.Duration(input.TTLSeconds)*time.Second+30*time.Second)
		if waitErr != nil {
			return "ERROR: " + waitErr.Error(), nil
		}
		body, _ := json.Marshal(map[string]any{
			"kind":             "result",
			"child_session_id": res.ChildSessionID,
			"status":           res.Status,
			"summary":          res.Summary,
			"turn_count":       res.TurnCount,
			"artifacts":        res.Artifacts,
			"error":            res.Error,
		})
		return string(body), nil
	}

	body, _ := json.Marshal(map[string]any{
		"kind":             "handle",
		"child_session_id": childID,
		"status":           StatusActive,
		"purpose":          input.Purpose,
		"template_id":      tmpl.ID,
		"expires_at":       expiresAt.Format(time.RFC3339),
		"max_turns":        input.MaxTurns,
	})
	return string(body), nil
}

// OnChildSettled 在子 runtime turn 空闲且无 pending HITL 时调用，尝试完成子 Agent。
func (m *Manager) OnChildSettled(childSessionID, summary string, turnCount int) {
	m.mu.Lock()
	rec, ok := m.records[childSessionID]
	if !ok || rec.terminal() {
		m.mu.Unlock()
		return
	}
	rec.mu.Lock()
	rec.TurnCount = turnCount
	rec.mu.Unlock()
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
	rec, ok := m.records[childSessionID]
	if !ok {
		m.mu.Unlock()
		return Result{}, fmt.Errorf("child_agent_not_found")
	}
	if parentSessionID != "" && rec.ParentSessionID != parentSessionID {
		m.mu.Unlock()
		return Result{}, fmt.Errorf("child_agent_not_found")
	}
	if rec.terminal() {
		out := rec.snapshot()
		m.mu.Unlock()
		return out, nil
	}
	prev := rec.Status
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
	if rec, ok := m.records[childSessionID]; ok {
		return rec.snapshot(), nil
	}
	if res, ok := m.delivered[childSessionID]; ok {
		return res, nil
	}
	return Result{}, fmt.Errorf("child_agent_not_found")
}

// ListActive 返回父 session 下未交付的子 Agent 记录。
func (m *Manager) ListActive(parentSessionID string) []*Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := m.byParent[parentSessionID]
	out := make([]*Record, 0, len(ids))
	for _, id := range ids {
		rec := m.records[id]
		if rec == nil || rec.terminal() {
			continue
		}
		out = append(out, rec)
	}
	return out
}

// RouteResume 将父 session 收到的 resume 路由到父或子 runtime。
func (m *Manager) RouteResume(parentSessionID string, resume map[string]any) (targetParent bool, err error) {
	if m.host == nil {
		return true, fmt.Errorf("child agent host not configured")
	}
	childID, _ := resume["child_session_id"].(string)
	childID = strings.TrimSpace(childID)
	if childID == "" {
		return true, m.host.DeliverParentResume(parentSessionID, resume)
	}
	m.mu.Lock()
	rec, ok := m.records[childID]
	if !ok || rec.ParentSessionID != parentSessionID {
		m.mu.Unlock()
		return false, fmt.Errorf("hitl_target_mismatch")
	}
	m.mu.Unlock()
	if !m.host.ChildHasPendingHITL(childID) {
		return false, fmt.Errorf("no_pending_hitl")
	}
	return false, m.host.DeliverChildResume(childID, resume)
}

// CancelAllForParent 父 session 删除时取消非 detached 子 Agent。
func (m *Manager) CancelAllForParent(parentSessionID string) {
	m.mu.Lock()
	ids := append([]string(nil), m.byParent[parentSessionID]...)
	m.mu.Unlock()
	for _, id := range ids {
		m.mu.Lock()
		rec := m.records[id]
		detached := rec != nil && rec.Detached
		m.mu.Unlock()
		if detached {
			continue
		}
		_, _ = m.Cancel(parentSessionID, id, "parent session released")
	}
}

func (m *Manager) finishWithEvent(childSessionID string, status Status, summary, errText string, cancelledEvent bool, previousStatus string) Result {
	m.mu.Lock()
	rec, ok := m.records[childSessionID]
	if !ok || rec.terminal() {
		out := Result{}
		if rec != nil {
			out = rec.snapshot()
		}
		m.mu.Unlock()
		return out
	}
	rec.mu.Lock()
	rec.Status = status
	out := Result{
		ChildSessionID: childSessionID,
		Status:         status,
		Summary:        summary,
		TurnCount:      rec.TurnCount,
		Error:          errText,
		Artifacts:      []string{},
	}
	rec.result = &out
	select {
	case <-rec.done:
	default:
		close(rec.done)
	}
	rec.mu.Unlock()
	parentID := rec.ParentSessionID
	m.mu.Unlock()

	m.mu.Lock()
	m.delivered[childSessionID] = out
	m.mu.Unlock()

	if cancelledEvent {
		m.publishCancelled(parentID, childSessionID, errText, previousStatus)
	} else {
		m.publishCompleted(parentID, &out)
	}
	if m.host != nil {
		m.host.StopChild(childSessionID)
	}
	m.removeRecord(childSessionID)
	return out
}

func (m *Manager) removeRecord(childSessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec := m.records[childSessionID]
	if rec == nil {
		return
	}
	delete(m.records, childSessionID)
	ids := m.byParent[rec.ParentSessionID]
	filtered := ids[:0]
	for _, id := range ids {
		if id != childSessionID {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) == 0 {
		delete(m.byParent, rec.ParentSessionID)
	} else {
		m.byParent[rec.ParentSessionID] = filtered
	}
}

func (m *Manager) waitRecord(ctx context.Context, rec *Record, timeout time.Duration) (Result, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-rec.done:
		return rec.snapshot(), nil
	case <-timer.C:
		_, _ = m.Cancel(rec.ParentSessionID, rec.ChildSessionID, "wait timeout")
		return Result{}, fmt.Errorf("wait timeout")
	}
}

func (m *Manager) checkActiveLimit(parentID string) error {
	active := 0
	m.mu.Lock()
	for _, id := range m.byParent[parentID] {
		rec := m.records[id]
		if rec != nil && !rec.terminal() {
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
	rec := m.records[childID]
	if rec == nil || rec.terminal() {
		m.mu.Unlock()
		return
	}
	parentID := rec.ParentSessionID
	prev := string(rec.Status)
	m.mu.Unlock()
	m.finishWithEvent(childID, StatusExpired, "", "ttl expired", false, prev)
	m.logger.Info("child agent expired", "child_session_id", childID, "parent_session_id", parentID)
}

func (m *Manager) publishCreated(parentID string, rec *Record, wait bool) {
	if m.hub == nil {
		return
	}
	m.hub.Publish(parentID, m.agentID, "child_agent_created", map[string]any{
		"child_session_id":  rec.ChildSessionID,
		"parent_session_id": parentID,
		"purpose":           rec.Purpose,
		"template_id":       rec.TemplateID,
		"status":            StatusActive,
		"expires_at":        rec.ExpiresAt.Format(time.RFC3339),
		"max_turns":         rec.MaxTurns,
		"wait":              wait,
	})
}

func (m *Manager) publishCompleted(parentID string, res *Result) {
	if m.hub == nil || res == nil {
		return
	}
	m.hub.Publish(parentID, m.agentID, "child_agent_completed", map[string]any{
		"child_session_id":  res.ChildSessionID,
		"parent_session_id": parentID,
		"status":            res.Status,
		"summary":           res.Summary,
		"turn_count":        res.TurnCount,
		"error":             res.Error,
		"artifacts":         res.Artifacts,
	})
}

func (m *Manager) publishCancelled(parentID, childID, reason, previous string) {
	if m.hub == nil {
		return
	}
	m.hub.Publish(parentID, m.agentID, "child_agent_cancelled", map[string]any{
		"child_session_id":  childID,
		"parent_session_id": parentID,
		"status":            StatusCancelled,
		"reason":            reason,
		"previous_status":   previous,
	})
}

func generateChildSessionID() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate child session id: %w", err)
	}
	return "child-" + hex.EncodeToString(b[:]), nil
}
