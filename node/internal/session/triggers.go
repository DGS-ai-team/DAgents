package session

import (
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
)

// TriggerSubmitter 将 trigger fire 结果投递到 session 队列。
type TriggerSubmitter struct {
	Mgr *Manager
	// EnsureAgentRuntime loads an existing registered Agent before routing.
	EnsureAgentRuntime func(agentID string) error
}

// SubmitGoalTriggerMessage uses the same durable trigger delivery path while
// carrying Goal/Run identity into the InputBox.
func (t *TriggerSubmitter) SubmitGoalTriggerMessage(sessionID, triggerID, goalID, runID, content, deliveryID string) error {
	return t.Mgr.EnqueueGoalTriggerMessage(sessionID, triggerID, goalID, runID, content, deliveryID)
}

// EnsureSession 创建或复用 session 并返回 ID。
func (t *TriggerSubmitter) EnsureSession(requestedID string) (string, error) {
	requestedID = strings.TrimSpace(requestedID)
	sess, _, err := t.Mgr.Create(requestedID)
	if err != nil {
		return "", err
	}
	return sess.ID, nil
}

// EnsureSessionForAgent routes fixed empty-session triggers to the canonical
// runtime of an already loaded Agent. It refuses implicit cross-Agent runtime
// creation; explicit sessions continue through the normal manager path.
func (t *TriggerSubmitter) EnsureSessionForAgent(targetAgentID, requestedID string) (string, error) {
	targetAgentID = strings.TrimSpace(targetAgentID)
	requestedID = strings.TrimSpace(requestedID)
	if requestedID == "" {
		if targetAgentID == "" {
			return "", fmt.Errorf("target_agent_id is required")
		}
		requestedID = targetAgentID
	}
	if targetAgentID != "" && targetAgentID != t.Mgr.AgentID() && t.EnsureAgentRuntime != nil {
		if err := t.EnsureAgentRuntime(targetAgentID); err != nil {
			return "", err
		}
	}
	if targetAgentID != "" && targetAgentID != t.Mgr.AgentID() && t.Mgr.Get(requestedID) == nil {
		return "", fmt.Errorf("target agent runtime is not loaded: %s", targetAgentID)
	}
	if targetAgentID != "" && requestedID != targetAgentID && t.Mgr.Get(requestedID) == nil {
		return "", fmt.Errorf("target session is not loaded for agent: %s", targetAgentID)
	}
	sess, err := func() (*Session, error) {
		id, e := t.EnsureSession(requestedID)
		if e != nil {
			return nil, e
		}
		return t.Mgr.Get(id), nil
	}()
	if err != nil {
		return "", err
	}
	if sess == nil || (targetAgentID != "" && strings.TrimSpace(sess.AgentID) != targetAgentID) {
		return "", fmt.Errorf("target session does not belong to agent: %s", targetAgentID)
	}
	return sess.ID, nil
}

// SubmitTriggerMessage 将 trigger 作为 FIFO 外部输入提交。
func (t *TriggerSubmitter) SubmitTriggerMessage(sessionID, triggerID, content string) error {
	return t.Mgr.EnqueueTriggerMessage(sessionID, triggerID, content, "")
}

func (t *TriggerSubmitter) SubmitTriggerMessageWithDelivery(sessionID, triggerID, deliveryID, content string) error {
	return t.Mgr.EnqueueTriggerMessage(sessionID, triggerID, content, deliveryID)
}

func (t *TriggerSubmitter) SubmitAutoTriggerMessage(sessionID, triggerID, deliveryID, content string) error {
	return t.Mgr.EnqueueAutoTriggerMessage(sessionID, triggerID, content, deliveryID)
}

// EnqueueTriggerMessage 将 trigger 任务写入 session InputBox；session 不存在时会先 Create。

// 逻辑：
// 1. 校验 content 非空；
// 2. EnsureSession（空 ID 则新建）；
// 3. 按 session input_seq FIFO，Envelope.TriggerID 供消费后清除 pending。
func (m *Manager) EnqueueTriggerMessage(sessionID, triggerID, content string, deliveryID ...string) error {
	return m.enqueueTriggerMessage(sessionID, triggerID, content, "", "", deliveryID...)
}

// EnqueueAutoTriggerMessage marks a system-owned Auto wakeup at ingress.
func (m *Manager) EnqueueAutoTriggerMessage(sessionID, triggerID, content string, deliveryID ...string) error {
	return m.enqueueTriggerMessageKind(InputKindSystemAuto, sessionID, triggerID, content, "", "", deliveryID...)
}

// EnqueueGoalTriggerMessage carries durable goal/run identity through the
// existing InputBox so lifecycle observers can reconcile the real Turn.
func (m *Manager) EnqueueGoalTriggerMessage(sessionID, triggerID, goalID, runID, content string, deliveryID ...string) error {
	return m.enqueueTriggerMessage(sessionID, triggerID, content, goalID, runID, deliveryID...)
}

func (m *Manager) enqueueTriggerMessage(sessionID, triggerID, content, goalID, runID string, deliveryID ...string) error {
	return m.enqueueTriggerMessageKind(InputKindTrigger, sessionID, triggerID, content, goalID, runID, deliveryID...)
}

func (m *Manager) enqueueTriggerMessageKind(kind InputKind, sessionID, triggerID, content, goalID, runID string, deliveryID ...string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("empty trigger content")
	}
	sess, _, err := m.Create(strings.TrimSpace(sessionID))
	if err != nil {
		return err
	}
	rt := m.getRuntime(sess.ID)
	if rt == nil {
		return fmt.Errorf("agent_not_found")
	}
	id := ""
	if len(deliveryID) > 0 {
		id = strings.TrimSpace(deliveryID[0])
	}
	env := queue.Envelope{RequestType: queue.RequestTypeMessage, Content: content, TriggerID: strings.TrimSpace(triggerID), DeliveryID: id, GoalID: strings.TrimSpace(goalID), RunID: strings.TrimSpace(runID), UserName: llm.UserNameTrigger}
	_, err = rt.appendInput(kind, env)
	return err
}
