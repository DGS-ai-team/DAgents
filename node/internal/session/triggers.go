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
}

// EnsureSession 创建或复用 session 并返回 ID。
func (t *TriggerSubmitter) EnsureSession(requestedID string) (string, error) {
	sess, _, err := t.Mgr.Create(strings.TrimSpace(requestedID))
	if err != nil {
		return "", err
	}
	return sess.ID, nil
}

// SubmitTriggerMessage 以 other 优先级入队 trigger 渲染后的 user 消息。
func (t *TriggerSubmitter) SubmitTriggerMessage(sessionID, triggerID, content string) error {
	return t.Mgr.EnqueueTriggerMessage(sessionID, triggerID, content)
}

// EnqueueTriggerMessage 将 trigger 任务入队；session 不存在时会先 Create。

// 逻辑：
// 1. 校验 content 非空；
// 2. EnsureSession（空 ID 则新建）；
// 3. 以 PriorityOther 入队，Envelope.TriggerID 供 dequeue 后清除 pending。
func (m *Manager) EnqueueTriggerMessage(sessionID, triggerID, content string) error {
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
		return fmt.Errorf("session_not_found")
	}
	env := queue.Envelope{RequestType: queue.RequestTypeTriggerMessage, Content: content, TriggerID: strings.TrimSpace(triggerID), UserName: llm.UserNameTrigger}
	return rt.enqueue(env, queue.PriorityOther)
}
