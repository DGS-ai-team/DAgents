package api

import (
	"context"
	"log/slog"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/browser"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
	"github.com/DGS-ai-team/DAgents/node/internal/wecom"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

// attachNodeRuntimeDeps 将 Node 级运行时依赖挂到工具 Registry（默认表与 per-agent 共用）。
func (s *Server) attachNodeRuntimeDeps(reg *tools.Registry, targetAgentID string) {
	if s == nil || reg == nil {
		return
	}
	reg.SetAgentID(targetAgentID)
	if s.linuxProvider != nil {
		if err := reg.WithLinuxShellProvider(s.linuxProvider); err != nil && s.logger != nil {
			s.logger.Warn("agent linux provider bind failed", "agent_id", targetAgentID, "error", err)
		}
	}
	if s.transfers != nil {
		if err := reg.WithLinuxTransferManager(s.transfers); err != nil && s.logger != nil {
			s.logger.Warn("agent linux transfer manager bind failed", "agent_id", targetAgentID, "error", err)
		}
	}
	if s.linuxChannels != nil {
		reg.SetTerminalConfigResolver(s.linuxChannels)
	}
	attachTriggerRuntime(reg, s.triggerStore, s.triggerSched, targetAgentID)
	attachWeComRuntime(reg, s.cfg)
	attachBrowserTaskNotifier(reg, s.sessions, s.logger)
	attachProcessEventSink(reg, s.stream, s.store, s.logger)
	reg.SetTerminalSessionBroker(s.terminals)
	if s.mediaRegister != nil {
		reg.SetMediaRegister(s.mediaRegister)
	}
	reg.SetBrowserManager(s.browserManager())
	reg.SetBrowserLLMResolver(func(ctx context.Context) (*browser.LLMSettings, error) {
		return s.browserLLMForAgent(ctx, targetAgentID)
	})
	if s.agents != nil {
		agents := s.agents
		reg.SetBrowserCompanionExists(func(ctx context.Context, companionAgentID string) (bool, error) {
			rec, err := agents.Get(ctx, companionAgentID)
			if err != nil {
				return false, err
			}
			return rec != nil && !rec.Archived, nil
		})
	}
}

// attachBrowserTaskNotifier 将 browser_run_task(wait=false) 的终态回灌到
// Agent session；Node 只向已建立的本地 runtime 入队，不需要 sidecar 主动访问 Node。
func attachBrowserTaskNotifier(reg *tools.Registry, mgr *session.Manager, logger *slog.Logger) {
	if reg == nil || mgr == nil {
		return
	}
	reg.SetBrowserTaskNotifier(func(sessionID string, done tools.BrowserTaskDone) {
		if err := mgr.EnqueueAsyncToolResult(sessionID, queue.AsyncToolResultPayload{
			JobID:      done.TaskID,
			ToolName:   "browser_run_task",
			ToolCallID: done.ToolCallID,
			Status:     done.Status,
			ResultText: done.ResultText,
			ErrorText:  done.ErrorText,
		}); err != nil && logger != nil {
			logger.Warn("browser task completion enqueue failed", "session_id", sessionID, "task_id", done.TaskID, "error", err)
		}
	})
}

// browserManager returns the current process-level browser manager. Browser
// capability settings can be changed without restarting Node, so callers
// must not retain the field directly while a settings patch is in flight.
func (s *Server) browserManager() *browser.Manager {
	if s == nil {
		return nil
	}
	s.browserMu.RLock()
	defer s.browserMu.RUnlock()
	return s.browserMgr
}

// installBrowserManager atomically swaps the process-level browser manager
// and rebinds all loaded Agent/workgroup registries. The session manager
// requests a model-context refresh rather than mutating an active Step's
// frozen tool snapshot.
func (s *Server) installBrowserManager(next *browser.Manager) {
	if s == nil {
		if next != nil {
			_ = next.Close()
		}
		return
	}
	s.browserMu.Lock()
	previous := s.browserMgr
	s.browserMgr = next
	s.browserMu.Unlock()

	if s.tools != nil {
		s.tools.SetBrowserManager(next)
	}
	if s.sessions != nil {
		s.sessions.SetBrowserManager(next)
	}
	if previous != nil && previous != next {
		if err := previous.Close(); err != nil && s.logger != nil {
			s.logger.Warn("previous browser manager close failed", "error", err)
		}
	}
}

// attachTriggerRuntime 为工具 Registry 注入触发器 store；targetAgentID 为空时用 node_id。
func attachTriggerRuntime(reg *tools.Registry, store *triggers.Store, sched *triggers.Scheduler, targetAgentID string) {
	if reg == nil || store == nil {
		return
	}
	agentID := strings.TrimSpace(targetAgentID)
	reg.SetTriggerRuntime(store, sched, agentID)
}

// attachWeComRuntime 按 Node 配置注入企业微信 webhook 客户端。
func attachWeComRuntime(reg *tools.Registry, cfg *config.Config) {
	if reg == nil {
		return
	}
	reg.SetWeComClient(wecom.NewClientFromConfig(cfg))
}

// attachProcessEventSink 将执行生命周期事件接入 Node stream。
// 输出片段使用 ephemeral SSE，生命周期事件可回放并写入审计库。
func attachProcessEventSink(reg *tools.Registry, hub *stream.Hub, auditStore *store.SQLiteStore, logger *slog.Logger) {
	if reg == nil || hub == nil {
		return
	}
	reg.SetProcessEventSink(func(ev tools.ProcessEvent) {
		sessionID := strings.TrimSpace(ev.Context.SessionID)
		if sessionID == "" {
			return
		}
		payload := map[string]any{
			"process_id":   ev.ProcessID,
			"event":        string(ev.Type),
			"seq":          ev.Seq,
			"stream":       ev.Stream,
			"output_bytes": ev.OutputBytes,
		}
		if ev.Context.CommandDigest != "" {
			payload["command_digest"] = ev.Context.CommandDigest
		}
		if len(ev.Data) > 0 {
			payload["data"] = ev.Data
		}
		if ev.Exit != nil {
			payload["exit_status"] = map[string]any{
				"code":  ev.Exit.Code,
				"error": ev.Exit.Error,
			}
		}
		if ev.Type == tools.ProcessEventOutput {
			hub.PublishEphemeral(sessionID, "execution", payload)
			return
		}
		hub.Publish(sessionID, "execution", payload)
		if auditStore == nil {
			return
		}
		agentID := strings.TrimSpace(ev.Context.AgentID)
		if agentID == "" {
			agentID = sessionID
		}
		var exitCode *int
		exitError := ""
		if ev.Exit != nil {
			code := ev.Exit.Code
			exitCode = &code
			exitError = ev.Exit.Error
		}
		if err := auditStore.AppendExecutionEvent(context.Background(), store.ExecutionEventRecord{
			AgentID:        agentID,
			SessionID:      sessionID,
			ProcessID:      ev.ProcessID,
			ProcessSeq:     ev.Seq,
			EventType:      string(ev.Type),
			Stream:         ev.Stream,
			TurnID:         ev.Context.TurnID,
			ToolCallID:     ev.Context.ToolCallID,
			TargetKind:     ev.Context.Target.Kind,
			TargetID:       ev.Context.Target.ID,
			PolicyDecision: ev.Context.PolicyDecision,
			ApprovalID:     ev.Context.ApprovalID,
			RiskLevel:      ev.Context.RiskLevel,
			CommandDigest:  ev.Context.CommandDigest,
			OutputBytes:    ev.OutputBytes,
			ExitCode:       exitCode,
			ExitError:      exitError,
		}); err != nil && logger != nil {
			logger.Warn("execution event audit persist failed",
				"session_id", sessionID,
				"process_id", ev.ProcessID,
				"seq", ev.Seq,
				"error", err,
			)
		}
	})
}
