package browser

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

// Manager 管理 browser session 与 remote 驱动（dagents-browser）。
type Manager struct {
	cfg    config.BrowserConfig
	fsRoot string
	driver Driver

	mu       sync.Mutex
	sessions map[string]struct{}
}

// NewManager 创建 BrowserManager；enabled=false 时 driver 可为 nil。
func NewManager(cfg *config.Config, driver Driver) (*Manager, error) {
	if cfg == nil || !cfg.BrowserEnabled() {
		return &Manager{cfg: config.BrowserConfig{}}, nil
	}
	fsRoot := cfg.RuntimeDir()
	m := &Manager{
		cfg:      cfg.Browser,
		fsRoot:   fsRoot,
		driver:   driver,
		sessions: make(map[string]struct{}),
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
		n = 1
	}
	return n
}

func (m *Manager) defaultTimeoutMS() int {
	if m == nil || m.cfg.DefaultTimeoutMS <= 0 {
		return 30000
	}
	return m.cfg.DefaultTimeoutMS
}

func (m *Manager) headedDefault() bool {
	if m == nil || m.cfg.Headed == nil {
		return true
	}
	return *m.cfg.Headed
}

func (m *Manager) outputDir() string {
	dir := strings.TrimSpace(m.cfg.OutputDir)
	if dir == "" {
		return "browser"
	}
	return strings.Trim(dir, "/")
}

func (m *Manager) allowedSchemes() []string {
	if m == nil || len(m.cfg.AllowedURLSchemes) == 0 {
		return []string{"https", "http"}
	}
	out := make([]string, 0, len(m.cfg.AllowedURLSchemes))
	for _, s := range m.cfg.AllowedURLSchemes {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return []string{"https", "http"}
	}
	return out
}

// ValidateNavigateURL 校验 navigate 目标 URL。
func (m *Manager) ValidateNavigateURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	allowed := false
	for _, s := range m.allowedSchemes() {
		if scheme == s {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("url scheme %q not allowed", scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("url missing host")
	}
	return nil
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
	out, err := m.call(ctx, Request{
		Op:         "start",
		SessionKey: sessionKey,
		Headed:     &h,
		ViewportW:  viewportW,
		ViewportH:  viewportH,
	})
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
	out, err := m.call(ctx, Request{Op: "stop", SessionKey: sessionKey})
	m.mu.Lock()
	delete(m.sessions, sessionKey)
	m.mu.Unlock()
	return out, err
}

// Navigate 打开 URL。
func (m *Manager) Navigate(ctx context.Context, sessionKey, targetURL, waitUntil string) (ToolResult, error) {
	if err := m.ValidateNavigateURL(targetURL); err != nil {
		return ToolResult{OK: false, Error: err.Error()}, nil
	}
	if waitUntil == "" {
		waitUntil = "load"
	}
	return m.call(ctx, Request{
		Op:         "navigate",
		SessionKey: sessionKey,
		URL:        targetURL,
		WaitUntil:  waitUntil,
	})
}

// Click 点击元素；index 优先（来自 browser_snapshot），selector 为 fallback。
func (m *Manager) Click(ctx context.Context, sessionKey string, index int, selector string, fallbacks []string) (ToolResult, error) {
	selector = strings.TrimSpace(selector)
	if index <= 0 && selector == "" {
		return ToolResult{OK: false, Error: "index or selector is required (prefer index from browser_snapshot)"}, nil
	}
	return m.call(ctx, Request{
		Op:         "click",
		SessionKey: sessionKey,
		Index:      index,
		Selector:   selector,
		Fallbacks:  fallbacks,
	})
}

// Fill 填写输入框；index 优先，selector 为 fallback。
func (m *Manager) Fill(ctx context.Context, sessionKey string, index int, selector, text string) (ToolResult, error) {
	selector = strings.TrimSpace(selector)
	if index <= 0 && selector == "" {
		return ToolResult{OK: false, Error: "index or selector is required (prefer index from browser_snapshot)"}, nil
	}
	return m.call(ctx, Request{
		Op:         "fill",
		SessionKey: sessionKey,
		Index:      index,
		Selector:   selector,
		Text:       text,
	})
}

// Press 按键。
func (m *Manager) Press(ctx context.Context, sessionKey, key string) (ToolResult, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return ToolResult{OK: false, Error: "key is required"}, nil
	}
	return m.call(ctx, Request{
		Op:         "press",
		SessionKey: sessionKey,
		Key:        key,
	})
}

// Screenshot 保存当前页截图到 fs_root。
func (m *Manager) Screenshot(ctx context.Context, sessionKey, name string) (ToolResult, error) {
	abs, rel, err := SessionScreenshotPath(m.fsRoot, m.outputDir(), sessionKey, name)
	if err != nil {
		return ToolResult{OK: false, Error: err.Error()}, nil
	}
	out, err := m.call(ctx, Request{
		Op:         "screenshot",
		SessionKey: sessionKey,
		Path:       abs,
	})
	if err != nil {
		return out, err
	}
	if out.OK {
		out.ScreenshotPath = rel
	}
	return out, nil
}

// Wait 等待 index 对应元素、selector 或 load state。
func (m *Manager) Wait(ctx context.Context, sessionKey string, index int, selector, loadState string, timeoutMS int) (ToolResult, error) {
	if timeoutMS <= 0 {
		timeoutMS = m.defaultTimeoutMS()
	}
	if index <= 0 && strings.TrimSpace(selector) == "" && strings.TrimSpace(loadState) == "" {
		return ToolResult{OK: false, Error: "index, selector, or load_state is required"}, nil
	}
	return m.call(ctx, Request{
		Op:         "wait",
		SessionKey: sessionKey,
		Index:      index,
		Selector:   selector,
		LoadState:  loadState,
		TimeoutMS:  timeoutMS,
	})
}

// Snapshot 返回 browser-use 页面状态；includeScreenshot 为 true 时一并保存 PNG。
func (m *Manager) Snapshot(ctx context.Context, sessionKey string, maxElements int, includeScreenshot bool, screenshotName string) (ToolResult, error) {
	req := Request{
		Op:          "snapshot",
		SessionKey:  sessionKey,
		MaxElements: maxElements,
	}
	var screenshotRel string
	if includeScreenshot {
		name := strings.TrimSpace(screenshotName)
		if name == "" {
			name = ScreenshotName("snap")
		}
		abs, rel, err := SessionScreenshotPath(m.fsRoot, m.outputDir(), sessionKey, name)
		if err != nil {
			return ToolResult{OK: false, Error: err.Error()}, nil
		}
		req.IncludeScreenshot = true
		req.Path = abs
		screenshotRel = rel
	}
	out, err := m.call(ctx, req)
	if err != nil {
		return out, err
	}
	if out.OK && screenshotRel != "" {
		out.ScreenshotPath = screenshotRel
	}
	return out, err
}

// ClickCoordinate 在视口坐标 (x,y) 点击（视觉模式主路径）。
func (m *Manager) ClickCoordinate(ctx context.Context, sessionKey string, x, y int, button string) (ToolResult, error) {
	if x < 0 || y < 0 {
		return ToolResult{OK: false, Error: "coordinate_x and coordinate_y must be non-negative"}, nil
	}
	button = strings.TrimSpace(button)
	if button == "" {
		button = "left"
	}
	switch button {
	case "left", "right", "middle":
	default:
		return ToolResult{OK: false, Error: `button must be "left", "right", or "middle"`}, nil
	}
	return m.call(ctx, Request{
		Op:         "click_coordinate",
		SessionKey: sessionKey,
		CoordX:     x,
		CoordY:     y,
		Button:     button,
	})
}

// ScreenshotName 生成带时间戳的默认截图名。
func ScreenshotName(prefix string) string {
	p := strings.TrimSpace(prefix)
	if p == "" {
		p = "shot"
	}
	return fmt.Sprintf("%s-%d", p, time.Now().UnixMilli())
}
