package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/manage"
)

const (
	mcpHealthProbeTimeout = 35 * time.Second
	mcpHealthPollInterval = 30 * time.Second
)

// Handler 返回可用于 http.Server 的根 Handler（含 onboarding gate 与 access log）。
func (s *Server) Handler() http.Handler {
	return accessLogMiddleware(s.logger, s.onboardingGateMiddleware(s.mux))
}

// ListenAndServe 在配置的 listen 地址启动 HTTP 服务；ctx 取消时触发优雅关闭。
//
// 生命周期分为两段：
// 1. 先启动 Manage sidecar 和 HTTP listener；
// 2. ctx 取消后停止后台运行时、关闭持久化资源，最后关闭 HTTP listener。
func (s *Server) ListenAndServe(ctx context.Context) error {
	addr := s.cfg.ListenAddr()
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	regCtx, regCancel := context.WithCancel(ctx)
	defer regCancel()
	s.manageMu.Lock()
	s.manageCtx = regCtx
	s.manageCancel = regCancel
	s.manageStarted = false
	s.manageMu.Unlock()
	s.maybeStartManageSidecars()
	s.startMCPHealthMonitor(regCtx)
	if s.updateChecker != nil && !manage.UpdateDelegatedToShell() {
		s.updateChecker.Start(regCtx)
	}
	go func() {
		s.logger.Info("agent node listening", "addr", addr, "agent_id", s.cfg.NodeID)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		} else {
			errCh <- nil
		}
	}()

	select {
	case <-ctx.Done():
		s.logger.Info("agent node shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		regCancel()
		if s.workgroupDialer != nil {
			s.workgroupDialer.Close()
		}
		if s.registrar != nil {
			s.registrar.Stop(shutdownCtx)
		}
		// 与启动顺序相反：先停后台任务与会话，再关 HTTP 监听。
		if s.triggerSched != nil {
			s.triggerSched.Stop()
		}
		if s.terminals != nil {
			s.terminals.closeAll()
		}
		s.sessions.Stop()
		if s.tools != nil {
			_ = s.tools.CloseBrowser()
		}
		if s.backgroundJobs != nil {
			_ = s.backgroundJobs.Close()
		}
		if s.store != nil {
			_ = s.store.Close()
		}
		if s.agents != nil {
			_ = s.agents.Close()
		}
		if s.llmConfigs != nil {
			_ = s.llmConfigs.Close()
		}
		if s.nodeSettings != nil {
			_ = s.nodeSettings.Close()
		}
		if s.mcpManager != nil {
			s.mcpManager.Close()
		}
		if s.mcpServers != nil {
			_ = s.mcpServers.Close()
		}
		if s.linuxChannels != nil {
			_ = s.linuxChannels.Close()
		}
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return <-errCh
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("listen %s: %w", addr, err)
		}
		return nil
	}
}

// startMCPHealthMonitor restores MCP connections after every Node process
// restart and retries services that were temporarily offline. Configure only
// restores persisted definitions; the child process/HTTP client itself is
// intentionally recreated here, after the Node has entered its serving
// lifecycle and the status SSE listener is installed.
func (s *Server) startMCPHealthMonitor(ctx context.Context) {
	if s == nil || s.mcpManager == nil || ctx == nil {
		return
	}
	go func() {
		probe := func() {
			views := s.mcpManager.List()
			changed := false
			for _, before := range views {
				if !before.Enabled {
					continue
				}
				probeCtx, cancel := context.WithTimeout(ctx, mcpHealthProbeTimeout)
				after, err := s.mcpManager.RefreshIfNeeded(probeCtx, before.ID)
				cancel()
				if err != nil {
					s.logger.Warn("startup MCP health probe failed", "server_id", before.ID, "error", err)
				}
				if after.Status != before.Status {
					changed = true
				}
			}
			// A successful startup refresh may expose a catalog to Agents that
			// were loaded before the background probe completed.
			if changed && ctx.Err() == nil {
				s.reloadMCPBoundAgents(ctx)
			}
		}

		probe()
		ticker := time.NewTicker(mcpHealthPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				probe()
			}
		}
	}()
}

// maybeStartManageSidecars 在首配完成且 Manage 已启用时启动 registrar / workgroup dialer（可热启动一次）。
func (s *Server) maybeStartManageSidecars() {
	if s == nil {
		return
	}
	s.manageMu.Lock()
	defer s.manageMu.Unlock()
	if s.manageStarted || s.manageCtx == nil {
		return
	}
	if s.cfg == nil || !s.cfg.NodeProfileCompleted() {
		if s.cfg != nil && !s.cfg.NodeProfileCompleted() {
			s.logger.Info("manage registrar/dialer deferred until node profile onboarding completes")
		}
		return
	}
	if s.registrar != nil {
		s.registrar.Start(s.manageCtx)
	}
	if s.workgroupDialer != nil {
		ctx := s.manageCtx
		go func() {
			_ = s.workgroupDialer.Run(ctx, func(err error, backoff time.Duration) {
				s.logger.Warn("workgroup dialer disconnected; retrying",
					"error", err, "backoff", backoff.String())
			})
		}()
	}
	s.manageStarted = true
}

// Close 释放 SQLite / 会话等资源。ListenAndServe 退出路径会调用；httptest 测试需显式 Cleanup。
func (s *Server) Close() {
	if s == nil {
		return
	}
	if s.triggerSched != nil {
		s.triggerSched.Stop()
	}
	if s.terminals != nil {
		s.terminals.closeAll()
	}
	if s.sessions != nil {
		s.sessions.Stop()
	}
	if s.tools != nil {
		_ = s.tools.CloseBrowser()
	}
	if s.backgroundJobs != nil {
		_ = s.backgroundJobs.Close()
	}
	if s.store != nil {
		_ = s.store.Close()
	}
	if s.agents != nil {
		_ = s.agents.Close()
	}
	if s.llmConfigs != nil {
		_ = s.llmConfigs.Close()
	}
	if s.nodeSettings != nil {
		_ = s.nodeSettings.Close()
	}
	if s.mcpManager != nil {
		s.mcpManager.Close()
	}
	if s.mcpServers != nil {
		_ = s.mcpServers.Close()
	}
	if s.linuxChannels != nil {
		_ = s.linuxChannels.Close()
	}
}
