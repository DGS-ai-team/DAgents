package manage

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

const callerInputPollWait = 25 * time.Second

// InboxConsultationRunner 执行 A2A inbox 对应的本地 turn（流式 LLM + HITL 中继）。
type InboxConsultationRunner interface {
	RunInboxTurn(ctx context.Context, taskID, content string, resume map[string]any) (session.InboxTurnResult, error)
}

// ComplianceExecutor 合规助手 inbox 处理器：经 session turn loop 调用 LLM；HITL 时经 Manage awaiting_caller 中继至 caller。
type ComplianceExecutor struct {
	replier  *taskReplier
	sessions InboxConsultationRunner
	logger   *slog.Logger
}

// NewComplianceExecutor 构造合规 inbox 处理器。
func NewComplianceExecutor(cfg *config.Config, sessions InboxConsultationRunner, logger *slog.Logger) *ComplianceExecutor {
	if logger == nil {
		logger = slog.Default()
	}
	return &ComplianceExecutor{
		replier:  newTaskReplier(cfg, logger),
		sessions: sessions,
		logger:   logger,
	}
}

// HandleTask 实现 InboxTaskHandler。
func (e *ComplianceExecutor) HandleTask(ctx context.Context, task InboxTask) error {
	if err := e.replier.ack(ctx, task.TaskID); err != nil {
		e.logger.Warn("a2a ack failed", "task_id", task.TaskID, "error", err)
	}
	if e.sessions == nil {
		return e.replier.reply(ctx, task, "failed", "", "", "inbox consultation runner is not configured")
	}

	sessionID := inboxSessionIDForTask(task.TaskID)
	content := strings.TrimSpace(task.Content)
	var resume map[string]any

	for {
		step, err := e.sessions.RunInboxTurn(ctx, task.TaskID, content, resume)
		content = ""
		resume = nil
		if err != nil {
			e.logger.Warn("compliance inbox turn step failed",
				"task_id", task.TaskID,
				"from", task.FromAgentID,
				"error", err,
			)
			return e.replier.reply(ctx, task, "failed", step.Text, sessionID, err.Error())
		}
		if step.HITL != nil {
			payload, err := encodeRequiresInputPayload(task, sessionID, step.HITL)
			if err != nil {
				return e.replier.reply(ctx, task, "failed", "", sessionID, err.Error())
			}
			if err := e.replier.replyRequiresInput(ctx, task, payload, sessionID); err != nil {
				return err
			}
			e.logger.Info("compliance inbox awaiting caller HITL",
				"task_id", task.TaskID,
				"awaiting", step.HITL.Awaiting,
				"callee_session_id", sessionID,
			)
			for {
				resume, err = e.replier.pollCallerInput(ctx, task.TaskID, callerInputPollWait)
				if err != nil {
					return err
				}
				if resume != nil {
					break
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
			}
			continue
		}
		e.logger.Info("compliance inbox turn reply",
			"task_id", task.TaskID,
			"from", task.FromAgentID,
			"result_len", len(step.Text),
		)
		return e.replier.reply(ctx, task, "completed", step.Text, sessionID, "")
	}
}

func encodeRequiresInputPayload(task InboxTask, calleeSessionID string, hitl *session.InboxHITLPause) (string, error) {
	if hitl == nil {
		return "", fmt.Errorf("hitl payload is nil")
	}
	payload := map[string]any{
		"hitl_kind":         hitl.Awaiting,
		"task_id":           task.TaskID,
		"callee_session_id": calleeSessionID,
		"caller_session_id": strings.TrimSpace(task.CallerSessionID),
		"event_type":        hitl.EventType,
		"event_data":        hitl.Data,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func inboxSessionIDForTask(taskID string) string {
	return session.InboxSessionID(taskID)
}

func agentRole(cfg *config.Config) string {
	if card, _ := LoadAgentCard(cfg.Manage.Registration.AgentCardPath); card != nil {
		return card.role()
	}
	return ""
}

// ResolveInboxHandler 按 Agent Card metadata.role 选择 inbox 处理器。
func ResolveInboxHandler(cfg *config.Config, sessions *session.Manager, logger *slog.Logger) InboxTaskHandler {
	switch agentRole(cfg) {
	case "compliance":
		return NewComplianceExecutor(cfg, sessions, logger).HandleTask
	default:
		return nil
	}
}
