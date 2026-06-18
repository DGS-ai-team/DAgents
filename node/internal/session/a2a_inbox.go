package session

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

const defaultInboxTurnTimeout = 5 * time.Minute

var inboxSessionIDSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// InboxHITLPause 表示 inbox turn 在 HITL 处暂停，需 caller 经 Manage caller_resume 续跑。
type InboxHITLPause struct {
	Awaiting  string         `json:"awaiting"`
	EventType string         `json:"event_type"`
	Data      map[string]any `json:"data"`
}

// InboxTurnResult 为单次 inbox turn 等待结果。
type InboxTurnResult struct {
	Complete bool
	Text     string
	HITL     *InboxHITLPause
}

// RunInboxConsultation 兼容旧接口：首条 message 入队并等待终态（不含 HITL 中继时等同 RunInboxTurn）。
func (m *Manager) RunInboxConsultation(ctx context.Context, taskID, content string) (string, error) {
	out, err := m.RunInboxTurn(ctx, taskID, content, nil)
	if err != nil {
		return out.Text, err
	}
	if out.HITL != nil {
		return out.Text, fmt.Errorf("inbox turn paused for HITL (%s) without relay", out.HITL.Awaiting)
	}
	return out.Text, nil
}

// RunInboxTurn 执行 inbox turn 一步：content 非空时投递首条 user message；resume 非空时投递 HITL resume。
func (m *Manager) RunInboxTurn(ctx context.Context, taskID, content string, resume map[string]any) (InboxTurnResult, error) {
	if m == nil {
		return InboxTurnResult{}, fmt.Errorf("session manager is nil")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return InboxTurnResult{}, fmt.Errorf("task_id is required")
	}
	sessionID := inboxSessionID(taskID)
	content = strings.TrimSpace(content)

	afterSeq := m.hub.CurrentSeq()
	sub := m.hub.Subscribe(afterSeq)
	defer m.hub.Unsubscribe(sub)

	switch {
	case content != "":
		if _, _, err := m.prepareInboxSession(sessionID); err != nil {
			return InboxTurnResult{}, err
		}
		if _, err := m.EnqueueMessage(ctx, sessionID, "message", content, nil, llm.UserNameA2AInbox); err != nil {
			return InboxTurnResult{}, err
		}
	case resume != nil:
		if m.getRuntime(sessionID) == nil {
			return InboxTurnResult{}, fmt.Errorf("inbox session %q not found", sessionID)
		}
		if _, err := m.EnqueueMessage(ctx, sessionID, "resume", "", resume, ""); err != nil {
			return InboxTurnResult{}, err
		}
	default:
		return InboxTurnResult{}, fmt.Errorf("content or resume is required")
	}

	return m.waitInboxTurnWithSub(ctx, sessionID, sub)
}

func (m *Manager) waitInboxTurnWithSub(ctx context.Context, sessionID string, sub <-chan stream.Event) (InboxTurnResult, error) {
	deadline := time.NewTimer(inboxTurnTimeout(ctx))
	defer deadline.Stop()

	var assistant strings.Builder
	var hitl *InboxHITLPause

	for {
		select {
		case <-ctx.Done():
			return InboxTurnResult{Text: strings.TrimSpace(assistant.String())}, ctx.Err()
		case <-deadline.C:
			return InboxTurnResult{Text: strings.TrimSpace(assistant.String())}, nil
		case ev := <-sub:
			if ev.SessionID != sessionID {
				continue
			}
			switch ev.Type {
			case "assistant":
				if c, ok := ev.Data["content"].(string); ok {
					assistant.WriteString(c)
				}
			case "user_information_required":
				hitl = &InboxHITLPause{
					Awaiting:  "user_information",
					EventType: "user_information_required",
					Data:      cloneEventData(ev.Data),
				}
			case "approval_required":
				hitl = &InboxHITLPause{
					Awaiting:  "tool_approval",
					EventType: "approval_required",
					Data:      cloneEventData(ev.Data),
				}
			case "done":
				turnComplete, _ := ev.Data["turn_complete"].(bool)
				awaiting, _ := ev.Data["awaiting"].(string)
				if !turnComplete && strings.TrimSpace(awaiting) != "" {
					if hitl == nil {
						// hub 非阻塞投递时 done 可能先于 approval_required；继续等待 HITL 事件。
						continue
					}
					return InboxTurnResult{HITL: hitl}, nil
				}
				return InboxTurnResult{Complete: true, Text: strings.TrimSpace(assistant.String())}, nil
			}
		}
	}
}

func cloneEventData(data map[string]any) map[string]any {
	if data == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(data))
	for k, v := range data {
		out[k] = v
	}
	return out
}

func (m *Manager) prepareInboxSession(sessionID string) (*Session, bool, error) {
	if rt := m.getRuntime(sessionID); rt != nil {
		if _, err := m.ClearContext(sessionID); err != nil {
			return nil, false, err
		}
	}
	return m.Create(sessionID)
}

func inboxSessionID(taskID string) string {
	safe := inboxSessionIDSanitizer.ReplaceAllString(taskID, "-")
	safe = strings.Trim(safe, "-")
	if safe == "" {
		safe = "task"
	}
	return "a2a-" + safe
}

// InboxSessionID 返回 Task 对应的 inbox 本地 session id。
func InboxSessionID(taskID string) string {
	return inboxSessionID(taskID)
}

func inboxTurnTimeout(ctx context.Context) time.Duration {
	if deadline, ok := ctx.Deadline(); ok {
		if d := time.Until(deadline); d > 0 {
			return d
		}
	}
	return defaultInboxTurnTimeout
}
