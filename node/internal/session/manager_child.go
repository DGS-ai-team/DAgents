package session

import (
	"fmt"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/childagent"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
)

// SetChildAgentManager 注入子 Agent 管理器并绑定 Host。
func (m *Manager) SetChildAgentManager(cm *childagent.Manager) {
	m.children = cm
	if cm != nil {
		cm.BindHost(m)
	}
}

// ParentSessionActive 实现 childagent.Host。
func (m *Manager) ParentSessionActive(parentID string) bool {
	rt := m.getRuntime(parentID)
	return rt != nil && !rt.isChildSession()
}

// SpawnChild 创建子 runtime 并启动 consumer。
func (m *Manager) SpawnChild(spec childagent.SpawnSpec) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[spec.ChildAgentID]; ok {
		return fmt.Errorf("child session already exists")
	}
	if parent, ok := m.sessions[spec.ParentAgentID]; !ok || parent.isChildSession() {
		return fmt.Errorf("parent session not found")
	}
	loadedSkills, err := m.resolveChildLoadedSkills(spec.SkillNames)
	if err != nil {
		return err
	}
	childOpts := m.turn
	childOpts.MaxToolLoops = spec.MaxTurns
	rt := newChildRuntime(
		spec.ChildAgentID,
		spec.ParentAgentID,
		m.agentID,
		m.hub,
		m.llm,
		m.tools,
		m.policy,
		m.logger,
		childOpts,
		spec.AllowedTools,
		spec.Purpose,
		loadedSkills,
		m.children,
	)
	m.sessions[spec.ChildAgentID] = rt
	rt.start(m.ctx)
	return nil
}

func (m *Manager) resolveChildLoadedSkills(names []string) ([]skills.LoadedSkill, error) {
	if len(names) == 0 {
		return nil, nil
	}
	catalog := skills.NewCatalog(m.turn.SkillsRoot, m.turn.SkillsEnabled, m.turn.SkillsMaxInPrompt)
	if !catalog.Enabled() {
		return nil, fmt.Errorf("skills are disabled")
	}
	var missing []string
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := catalog.SelectByName(name); !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("unknown skill(s): %s", strings.Join(missing, ", "))
	}
	loaded := catalog.SetLoadedSkills(names)
	if len(loaded) == 0 {
		return nil, fmt.Errorf("no skills loaded")
	}
	return loaded, nil
}

// StopChild 停止子 consumer。
func (m *Manager) StopChild(childSessionID string) {
	m.mu.Lock()
	rt, ok := m.sessions[childSessionID]
	if ok {
		delete(m.sessions, childSessionID)
	}
	m.mu.Unlock()
	if ok {
		// StopChild may be called by the child consumeLoop itself after it
		// reports completion; do not wait for that goroutine from itself.
		rt.requestStop()
	}
}

// EnqueueChildTask 向子 session 投递首条 task。
func (m *Manager) EnqueueChildTask(childSessionID, content string) error {
	rt := m.getRuntime(childSessionID)
	if rt == nil || !rt.isChildSession() {
		return fmt.Errorf("child session not found")
	}
	_, err := rt.appendInput(InputKindA2A, queue.Envelope{RequestType: "message", Content: content, UserName: llm.UserNameChildTask})
	return err
}

// ChildHasPendingHITL 实现 childagent.Host。
func (m *Manager) ChildHasPendingHITL(childSessionID string) bool {
	rt := m.getRuntime(childSessionID)
	if rt == nil {
		return false
	}
	return rt.hasPendingHITL()
}

// ParentHasPendingHITL 实现 childagent.Host。
func (m *Manager) ParentHasPendingHITL(parentSessionID string) bool {
	rt := m.getRuntime(parentSessionID)
	if rt == nil {
		return false
	}
	return rt.hasPendingHITL()
}

// DeliverChildResume 向子 runtime 投递 resume。
func (m *Manager) DeliverChildResume(childSessionID string, resume map[string]any) error {
	rt := m.getRuntime(childSessionID)
	if rt == nil {
		return fmt.Errorf("child session not found")
	}
	if !rt.hasPendingHITL() {
		return fmt.Errorf("no_pending_hitl")
	}
	m.logger.Info("resume deliver child",
		"child_agent_id", childSessionID,
		"resume_value", resume,
	)
	return rt.enqueue(queue.Envelope{RequestType: "resume", ResumeValue: resume}, queue.PriorityResume)
}

// DeliverParentResume 向父 runtime 投递 resume。
func (m *Manager) DeliverParentResume(parentSessionID string, resume map[string]any) error {
	rt := m.getRuntime(parentSessionID)
	if rt == nil {
		return fmt.Errorf("agent_not_found")
	}
	if !rt.hasPendingHITL() {
		return fmt.Errorf("no_pending_hitl")
	}
	m.logger.Info("resume deliver parent",
		"session_id", parentSessionID,
		"resume_value", resume,
	)
	return rt.enqueue(queue.Envelope{RequestType: "resume", ResumeValue: resume}, queue.PriorityResume)
}

// ListActiveUser 返回非 child session 的活跃 session。
func (m *Manager) ListActiveUser() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Session, 0, len(m.sessions))
	for _, rt := range m.sessions {
		if rt.isChildSession() {
			continue
		}
		out = append(out, &rt.session)
	}
	return out
}

// ListChildAgents 返回父 session 下活跃与最近终态子 Agent 快照。
func (m *Manager) ListChildAgents(parentSessionID string) ([]ChildAgentView, error) {
	if m.children == nil {
		return nil, nil
	}
	if m.Get(parentSessionID) == nil {
		return nil, fmt.Errorf("agent_not_found")
	}
	recs, err := m.children.ListSnapshots(parentSessionID)
	if err != nil {
		return nil, err
	}
	out := make([]ChildAgentView, 0, len(recs))
	for _, snapshot := range recs {
		out = append(out, ChildAgentView{
			ChildAgentID: snapshot.ChildAgentID,
			ToolCallID:   snapshot.ToolCallID,
			Status:       string(snapshot.Status),
			Purpose:      snapshot.Purpose,
			AllowedTools: append([]string(nil), snapshot.AllowedTools...),
			LoadedSkills: append([]string(nil), snapshot.LoadedSkills...),
			CreatedAt:    snapshot.CreatedAt,
			ExpiresAt:    snapshot.ExpiresAt,
			FinishedAt:   snapshot.FinishedAt,
			TurnCount:    snapshot.TurnCount,
			MaxTurns:     snapshot.MaxTurns,
			Progress:     snapshot.Progress,
		})
	}
	return out, nil
}

// CancelChildAgent HTTP/内部取消子 Agent。
func (m *Manager) CancelChildAgent(parentSessionID, childSessionID, reason string) (childagent.Result, error) {
	if m.children == nil || !m.children.Enabled() {
		return childagent.Result{}, fmt.Errorf("child agents disabled")
	}
	return m.children.Cancel(parentSessionID, childSessionID, reason)
}

// ChildAgentView 为 HTTP 列表项。
type ChildAgentView struct {
	ChildAgentID string              `json:"child_agent_id"`
	ToolCallID   string              `json:"tool_call_id,omitempty"`
	Status       string              `json:"status"`
	Purpose      string              `json:"purpose"`
	AllowedTools []string            `json:"allowed_tools"`
	LoadedSkills []string            `json:"loaded_skills"`
	CreatedAt    time.Time           `json:"created_at"`
	ExpiresAt    time.Time           `json:"expires_at"`
	FinishedAt   time.Time           `json:"finished_at,omitempty"`
	TurnCount    int                 `json:"turn_count"`
	MaxTurns     int                 `json:"max_turns"`
	Progress     childagent.Progress `json:"progress"`
}

func lastAssistantSummary(msgs []llm.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			s := strings.TrimSpace(msgs[i].Content)
			if s != "" {
				return s
			}
		}
	}
	return ""
}
