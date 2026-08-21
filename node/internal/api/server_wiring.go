package api

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
	"github.com/DGS-ai-team/DAgents/node/internal/wecom"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

const desktopFocusRelayURL = "http://127.0.0.1:18767/v1/desktop/ui/focus"

// handleDesktopUIFocus 将远端浏览器的桌面焦点请求转发给本机 Shell。
func (s *Server) handleDesktopUIFocus(w http.ResponseWriter, r *http.Request) {
	var body io.Reader = http.NoBody
	if r.Body != nil {
		body = io.LimitReader(r.Body, 16<<10)
	}
	payload, err := io.ReadAll(body)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_focus_request", err.Error(), nil)
		return
	}
	request, err := http.NewRequestWithContext(r.Context(), http.MethodPost, desktopFocusRelayURL, bytes.NewReader(payload))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "desktop_relay_failed", err.Error(), nil)
		return
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "desktop_unavailable", "Shell desktop API is unavailable", nil)
		return
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 32<<10))
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "desktop_relay_failed", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(responseBody)
}

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
	if s.backgroundJobs != nil {
		bindErr := error(nil)
		if reg == s.tools {
			bindErr = reg.WithBackgroundJobStore(s.backgroundJobs)
		} else {
			bindErr = reg.WithBackgroundJobStoreForSession(s.backgroundJobs, targetAgentID)
		}
		if bindErr != nil && s.logger != nil {
			s.logger.Warn("agent tools background job store bind failed", "error", bindErr)
		}
	}
	attachTriggerRuntime(reg, s.triggerStore, s.triggerSched, targetAgentID)
	attachWeComRuntime(reg, s.cfg)
	attachBackgroundJobNotifier(reg, s.sessions, s.logger)
	attachProcessEventSink(reg, s.stream, s.store, s.logger)
	reg.SetTerminalSessionBroker(s.terminals)
	if s.mediaRegister != nil {
		reg.SetMediaRegister(s.mediaRegister)
	}
	if s.browserMgr != nil {
		reg.SetBrowserManager(s.browserMgr)
	}
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

// attachBackgroundJobNotifier 将后台 bash 完成回调挂到 Registry（默认工具表与 per-agent Registry 均需挂载）。
func attachBackgroundJobNotifier(reg *tools.Registry, mgr *session.Manager, logger *slog.Logger) {
	if reg == nil || mgr == nil {
		return
	}
	reg.SetBackgroundJobNotifier(func(sessionID string, done tools.BackgroundJobDone) {
		if err := mgr.EnqueueAsyncToolResult(sessionID, queue.AsyncToolResultPayload{
			JobID:                  done.JobID,
			ToolName:               done.ToolName,
			ToolCallID:             done.ToolCallID,
			Status:                 done.Status,
			ResultText:             done.ResultText,
			ErrorText:              done.ErrorText,
			OutputCompressSavedPct: done.OutputCompressSavedPct,
			OutputCompressRawRunes: done.OutputCompressRawRunes,
			OutputCompressOutRunes: done.OutputCompressOutRunes,
		}); err != nil && logger != nil {
			logger.Warn("background tool completion enqueue failed", "session_id", sessionID, "error", err)
		}
	})
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
