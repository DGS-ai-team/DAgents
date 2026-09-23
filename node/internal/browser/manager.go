package browser

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

// Manager 管理按 Agent Session 隔离的 Playwright browser session。
// 主 Agent 通过 browser_call/browser_evaluate 直接驱动确定性浏览器协议。
type Manager struct {
	cfg    config.BrowserConfig
	driver Driver

	mu       sync.Mutex
	sessions map[string]struct{}
	locks    map[string]*sync.Mutex
}

// NewManager 创建 BrowserManager；enabled=false 时 driver 可为 nil。
func NewManager(cfg *config.Config, driver Driver) (*Manager, error) {
	if cfg == nil || !cfg.BrowserEnabled() {
		return &Manager{cfg: config.BrowserConfig{}}, nil
	}
	m := &Manager{
		cfg:      cfg.Browser,
		driver:   driver,
		sessions: make(map[string]struct{}),
		locks:    make(map[string]*sync.Mutex),
	}
	if driver == nil {
		d, err := NewDriver(cfg)
		if err != nil {
			return nil, err
		}
		m.driver = d
	}
	return m, nil
}

// Enabled 是否启用 browser 能力。
func (m *Manager) Enabled() bool {
	return m != nil && m.driver != nil
}

// Close 关闭全部 session 与 CDP 连接。
func (m *Manager) Close() error {
	if m == nil || m.driver == nil {
		return nil
	}
	m.mu.Lock()
	keys := make([]string, 0, len(m.sessions))
	for k := range m.sessions {
		keys = append(keys, k)
	}
	m.mu.Unlock()
	for _, k := range keys {
		_, _ = m.Stop(context.Background(), k)
	}
	return m.driver.Close()
}

func (m *Manager) maxSessions() int {
	if m == nil {
		return 0
	}
	n := m.cfg.MaxSessions
	if n <= 0 {
		n = 8
	}
	return n
}

func (m *Manager) defaultTimeoutMS() int {
	if m == nil || m.cfg.DefaultTimeoutMS <= 0 {
		return 30000
	}
	return m.cfg.DefaultTimeoutMS
}

func (m *Manager) sessionLock(key string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.locks == nil {
		m.locks = make(map[string]*sync.Mutex)
	}
	if lock := m.locks[key]; lock != nil {
		return lock
	}
	lock := &sync.Mutex{}
	m.locks[key] = lock
	return lock
}

func (m *Manager) headedDefault() bool {
	if m == nil || m.cfg.Headed == nil {
		return true
	}
	return *m.cfg.Headed
}

func (m *Manager) call(ctx context.Context, req Request) (ToolResult, error) {
	if !m.Enabled() {
		return ToolResult{OK: false, Error: "browser tools disabled (set browser.enabled: true)"}, nil
	}
	if strings.TrimSpace(req.SessionKey) == "" {
		return ToolResult{OK: false, Error: "session_key is required"}, nil
	}
	if req.TimeoutMS <= 0 {
		req.TimeoutMS = m.defaultTimeoutMS()
	}
	resp, err := m.driver.Call(ctx, req)
	if err != nil {
		return ToolResult{OK: false, Error: err.Error()}, nil
	}
	return toolResultFromResponse(resp), nil
}

// Call executes one browser_call batch. The sidecar validates every action;
// the Manager only supplies the session owner and serializes same-session
// calls.
func (m *Manager) Call(ctx context.Context, sessionKey, callID string, actions []Action) (ToolResult, error) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ToolResult{OK: false, Error: "session_key is required"}, nil
	}
	if len(actions) == 0 {
		return ToolResult{OK: false, Error: "actions must not be empty"}, nil
	}
	lock := m.sessionLock(sessionKey)
	lock.Lock()
	defer lock.Unlock()
	out, err := m.call(ctx, Request{Op: "call", SessionKey: sessionKey, CallID: strings.TrimSpace(callID), Actions: actions})
	m.updateSessionBookkeeping(sessionKey, actions, out)
	return out, err
}

func (m *Manager) updateSessionBookkeeping(sessionKey string, actions []Action, out ToolResult) {
	if m == nil || !out.OK {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions == nil {
		m.sessions = make(map[string]struct{})
	}
	for _, action := range actions {
		switch strings.TrimSpace(action.Op) {
		case "start":
			m.sessions[sessionKey] = struct{}{}
		case "stop":
			delete(m.sessions, sessionKey)
		}
	}
}

// Evaluate executes one browser_evaluate request. It never retries and shares
// the same per-session serialization as browser_call.
func (m *Manager) Evaluate(ctx context.Context, sessionKey, callID, script string, arg any, pageID string, target map[string]any, timeoutMS int) (ToolResult, error) {
	sessionKey = strings.TrimSpace(sessionKey)
	script = strings.TrimSpace(script)
	if sessionKey == "" {
		return ToolResult{OK: false, Error: "session_key is required"}, nil
	}
	if script == "" {
		return ToolResult{OK: false, Error: "script is required"}, nil
	}
	lock := m.sessionLock(sessionKey)
	lock.Lock()
	defer lock.Unlock()
	return m.call(ctx, Request{Op: "evaluate", SessionKey: sessionKey, CallID: strings.TrimSpace(callID), Script: script, Arg: arg, PageID: strings.TrimSpace(pageID), Target: target, TimeoutMS: timeoutMS})
}

// Start 启动绑定 session 的本机 Chrome（CDP）。
func (m *Manager) Start(ctx context.Context, sessionKey string, headed *bool, viewportW, viewportH int) (ToolResult, error) {
	sessionKey = strings.TrimSpace(sessionKey)
	m.mu.Lock()
	if _, ok := m.sessions[sessionKey]; ok {
		m.mu.Unlock()
		return ToolResult{OK: true, Detail: map[string]any{"already_started": true}}, nil
	}
	if len(m.sessions) >= m.maxSessions() {
		m.mu.Unlock()
		return ToolResult{OK: false, Error: fmt.Sprintf("browser session limit reached (max %d)", m.maxSessions())}, nil
	}
	m.sessions[sessionKey] = struct{}{}
	m.mu.Unlock()

	h := m.headedDefault()
	if headed != nil {
		h = *headed
	}
	if viewportW <= 0 {
		viewportW = 1280
	}
	if viewportH <= 0 {
		viewportH = 720
	}
	out, err := m.call(ctx, Request{Op: "call", SessionKey: sessionKey, Headed: &h, ViewportW: viewportW, ViewportH: viewportH,
		Actions: []Action{{Op: "start"}}})
	if err != nil || !out.OK {
		m.mu.Lock()
		delete(m.sessions, sessionKey)
		m.mu.Unlock()
	}
	return out, err
}

// Stop 关闭 session 对应浏览器。
func (m *Manager) Stop(ctx context.Context, sessionKey string) (ToolResult, error) {
	sessionKey = strings.TrimSpace(sessionKey)
	lock := m.sessionLock(sessionKey)
	lock.Lock()
	out, err := m.call(ctx, Request{Op: "call", SessionKey: sessionKey, Actions: []Action{{Op: "stop"}}})
	lock.Unlock()
	m.mu.Lock()
	delete(m.sessions, sessionKey)
	m.mu.Unlock()
	return out, err
}
