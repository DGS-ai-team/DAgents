package api

import (
	"context"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/browser"
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
	// Todo tools are available only on a real Auto main runtime. Goal and
	// ordinary runtimes deliberately receive no independent autonomy store.
	if s.autonomyStore != nil && s.agents != nil {
		if rec, err := s.agents.Get(context.Background(), targetAgentID); err == nil && rec != nil {
			if snap, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot); err == nil && strings.EqualFold(strings.TrimSpace(snap.AgentType), "auto") {
				reg.SetAutonomyTodoStore(s.autonomyStore)
			}
		}
	}
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
	attachProcessEventSink(reg, s.stream)
	reg.SetTerminalSessionBroker(s.terminals)
	if s.mediaRegister != nil {
		reg.SetMediaRegister(s.mediaRegister)
	}
	reg.SetBrowserManager(s.browserManager())
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

// installBrowserRuntime atomically swaps the process-level browser manager
// and rebinds all loaded Agent/workgroup registries. The session manager
// requests a model-context refresh rather than mutating an active Step's
// frozen tool snapshot.
func (s *Server) installBrowserRuntime(next *browser.Manager, service *browser.ServiceProcess, endpoint string) {
	if s == nil {
		if next != nil {
			_ = next.Close()
		}
		_ = service.Close()
		return
	}
	s.browserMu.Lock()
	previous, previousService := s.browserMgr, s.browserService
	if service == nil && next != nil && previousService != nil && strings.TrimRight(previousService.Endpoint(), "/") == strings.TrimRight(endpoint, "/") {
		service = previousService
	}
	s.browserMgr = next
	s.browserService = service
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
	if previousService != nil && previousService != service {
		if err := previousService.Close(); err != nil && s.logger != nil {
			s.logger.Warn("previous browser service stop failed", "error", err)
		}
	}
}

func (s *Server) closeBrowserService() {
	if s == nil {
		return
	}
	s.browserMu.Lock()
	service := s.browserService
	s.browserService = nil
	s.browserMu.Unlock()
	if err := service.Close(); err != nil && s.logger != nil {
		s.logger.Warn("browser service stop failed", "error", err)
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
// 输出片段使用 ephemeral SSE，生命周期事件可回放。
func attachProcessEventSink(reg *tools.Registry, hub *stream.Hub) {
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
	})
}
