package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultOperationTimeout = 30 * time.Second

type Manager struct {
	mu        sync.Mutex
	configs   map[string]ServerConfig
	clients   map[string]Client
	catalogs  map[string][]Tool
	views     map[string]ServerView
	refreshMu map[string]*sync.Mutex
	logger    *slog.Logger
	closed    bool
}

func NewManager(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		configs:   map[string]ServerConfig{},
		clients:   map[string]Client{},
		catalogs:  map[string][]Tool{},
		views:     map[string]ServerView{},
		refreshMu: map[string]*sync.Mutex{},
		logger:    logger,
	}
}

func (m *Manager) Configure(configs []ServerConfig) error {
	if m == nil {
		return fmt.Errorf("mcp manager is nil")
	}
	validated := make(map[string]ServerConfig, len(configs))
	for _, raw := range configs {
		cfg, err := ValidateServerConfig(raw)
		if err != nil {
			return err
		}
		if _, exists := validated[cfg.ID]; exists {
			return fmt.Errorf("duplicate mcp server id %q", cfg.ID)
		}
		validated[cfg.ID] = cfg
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, client := range m.clients {
		if _, keep := validated[id]; !keep || !sameConnectionConfig(m.configs[id], validated[id]) {
			_ = client.Close()
			delete(m.clients, id)
			delete(m.catalogs, id)
		}
	}
	m.configs = validated
	for id, cfg := range validated {
		view := m.views[id]
		view.ServerConfig = cfg
		if !cfg.Enabled {
			view.Status = StatusDisabled
		} else if view.Status == "" {
			view.Status = StatusOffline
		}
		catalog := append([]Tool(nil), m.catalogs[id]...)
		markEnabledTools(catalog, cfg.EnabledTools)
		m.catalogs[id] = catalog
		view.Tools = append([]Tool(nil), catalog...)
		markEnabledTools(view.Tools, cfg.EnabledTools)
		view.ToolCount = len(view.Tools)
		view.EnabledToolCount = countEnabledTools(view.Tools)
		m.views[id] = view
	}
	for id := range m.views {
		if _, keep := validated[id]; !keep {
			delete(m.views, id)
		}
	}
	return nil
}

func (m *Manager) Upsert(cfg ServerConfig) error {
	m.mu.Lock()
	configs := make([]ServerConfig, 0, len(m.configs)+1)
	for _, existing := range m.configs {
		if existing.ID != cfg.ID {
			configs = append(configs, existing)
		}
	}
	configs = append(configs, cfg)
	m.mu.Unlock()
	return m.Configure(configs)
}

func (m *Manager) Delete(id string) error {
	id = strings.TrimSpace(id)
	m.mu.Lock()
	configs := make([]ServerConfig, 0, len(m.configs))
	for _, cfg := range m.configs {
		if cfg.ID != id {
			configs = append(configs, cfg)
		}
	}
	_, exists := m.configs[id]
	m.mu.Unlock()
	if !exists {
		return fmt.Errorf("mcp server %q not found", id)
	}
	return m.Configure(configs)
}

func (m *Manager) List() []ServerView {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ServerView, 0, len(m.views))
	for _, view := range m.views {
		view.Tools = append([]Tool(nil), view.Tools...)
		out = append(out, view)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *Manager) Get(id string) (ServerView, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	view, ok := m.views[strings.TrimSpace(id)]
	return view, ok
}

func (m *Manager) Refresh(ctx context.Context, id string) (ServerView, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	id = strings.TrimSpace(id)
	lock := m.refreshLock(id)
	lock.Lock()
	defer lock.Unlock()
	m.mu.Lock()
	cfg, ok := m.configs[id]
	old := m.clients[id]
	delete(m.clients, id)
	delete(m.catalogs, id)
	m.mu.Unlock()
	if !ok {
		return ServerView{}, fmt.Errorf("mcp server %q not found", id)
	}
	if old != nil {
		_ = old.Close()
	}
	if !cfg.Enabled {
		return m.updateView(id, StatusDisabled, "", nil), nil
	}
	client, tools, err := m.startAndList(ctx, cfg)
	if err != nil {
		view := m.updateView(id, StatusError, err.Error(), nil)
		return view, err
	}
	markEnabledTools(tools, cfg.EnabledTools)
	m.mu.Lock()
	m.clients[id] = client
	m.catalogs[id] = append([]Tool(nil), tools...)
	m.mu.Unlock()
	return m.updateView(id, StatusReady, "", tools), nil
}

func (m *Manager) Test(ctx context.Context, id string) (ServerView, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	cfg, ok := m.configs[strings.TrimSpace(id)]
	m.mu.Unlock()
	if !ok {
		return ServerView{}, fmt.Errorf("mcp server %q not found", id)
	}
	if !cfg.Enabled {
		return m.updateView(id, StatusDisabled, "", nil), nil
	}
	client, tools, err := m.startAndList(ctx, cfg)
	if client != nil {
		_ = client.Close()
	}
	if err != nil {
		view := m.updateView(id, StatusError, err.Error(), nil)
		return view, err
	}
	markEnabledTools(tools, cfg.EnabledTools)
	m.mu.Lock()
	m.catalogs[id] = append([]Tool(nil), tools...)
	m.mu.Unlock()
	return m.updateView(id, StatusReady, "", tools), nil
}

func (m *Manager) EffectiveTools(ctx context.Context, bindings []Binding) ([]EffectiveTool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	bindings = append([]Binding(nil), bindings...)
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].ServerID < bindings[j].ServerID })
	var out []EffectiveTool
	seenQualified := make(map[string]string)
	for _, rawBinding := range bindings {
		binding := rawBinding.normalized()
		if !binding.Enabled || binding.ServerID == "" {
			continue
		}
		m.mu.Lock()
		cfg, ok := m.configs[binding.ServerID]
		m.mu.Unlock()
		if !ok {
			m.logger.Warn("mcp binding refers to a missing server", "server_id", binding.ServerID)
			continue
		}
		if !cfg.Enabled {
			continue
		}
		view, err := m.RefreshIfNeeded(ctx, binding.ServerID)
		if err != nil {
			// A binding is configuration, not a requirement that the external
			// process be alive during Agent startup. Keep a stale catalog when
			// possible; a later refresh will make new tools visible.
			m.logger.Warn("mcp server unavailable while building agent tools", "server_id", binding.ServerID, "error", err)
			m.mu.Lock()
			view = m.views[binding.ServerID]
			m.mu.Unlock()
			if view.Status == StatusError && len(view.Tools) == 0 {
				continue
			}
		}
		for _, remote := range view.Tools {
			if !remote.Enabled {
				continue
			}
			if !toolAllowed(binding, remote.Name) {
				continue
			}
			qualified, err := QualifiedToolName(binding.ServerID, remote.Name)
			if err != nil {
				return nil, err
			}
			if previous, exists := seenQualified[qualified]; exists {
				return nil, fmt.Errorf("MCP tool name collision after LLM normalization: %q (%s and %s)", qualified, previous, remote.Name)
			}
			seenQualified[qualified] = remote.Name
			serverID, remoteName := binding.ServerID, remote.Name
			out = append(out, EffectiveTool{
				QualifiedName: qualified,
				ServerID:      serverID,
				RemoteName:    remoteName,
				Description:   remote.Description,
				InputSchema:   remote.InputSchema,
				Call: func(callCtx context.Context, args json.RawMessage) (CallResult, error) {
					return m.Call(callCtx, serverID, remoteName, args)
				},
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].QualifiedName < out[j].QualifiedName })
	return out, nil
}

// ValidateBindings checks only persisted configuration. It intentionally does
// not start child processes, so an Agent can be configured while a server is
// temporarily offline.
func (m *Manager) ValidateBindings(bindings []Binding) error {
	if m == nil {
		return fmt.Errorf("mcp manager is nil")
	}
	seen := map[string]struct{}{}
	for _, raw := range bindings {
		binding := raw.normalized()
		if !binding.Enabled || binding.ServerID == "" {
			continue
		}
		if _, exists := seen[binding.ServerID]; exists {
			return fmt.Errorf("duplicate MCP binding %q", binding.ServerID)
		}
		seen[binding.ServerID] = struct{}{}
		m.mu.Lock()
		cfg, ok := m.configs[binding.ServerID]
		m.mu.Unlock()
		if !ok {
			return fmt.Errorf("mcp server %q is not configured", binding.ServerID)
		}
		if !cfg.Enabled {
			return fmt.Errorf("mcp server %q is disabled", binding.ServerID)
		}
	}
	return nil
}

// ToolNames returns cached qualified names for policy presentation. It does
// not connect to servers; unknown/offline catalogs are therefore omitted.
func (m *Manager) ToolNames(bindings []Binding) []string {
	if m == nil {
		return nil
	}
	var names []string
	for _, raw := range bindings {
		binding := raw.normalized()
		if !binding.Enabled {
			continue
		}
		m.mu.Lock()
		cfg, configured := m.configs[binding.ServerID]
		tools := append([]Tool(nil), m.catalogs[binding.ServerID]...)
		m.mu.Unlock()
		if !configured || !cfg.Enabled {
			continue
		}
		markEnabledTools(tools, cfg.EnabledTools)
		for _, remote := range tools {
			if !remote.Enabled {
				continue
			}
			if !toolAllowed(binding, remote.Name) {
				continue
			}
			if qualified, err := QualifiedToolName(binding.ServerID, remote.Name); err == nil {
				names = append(names, qualified)
			}
		}
	}
	sort.Strings(names)
	return names
}

func (m *Manager) RefreshIfNeeded(ctx context.Context, id string) (ServerView, error) {
	m.mu.Lock()
	view, ok := m.views[id]
	client := m.clients[id]
	m.mu.Unlock()
	if !ok {
		return ServerView{}, fmt.Errorf("mcp server %q not found", id)
	}
	if client != nil && view.Status == StatusReady {
		return view, nil
	}
	return m.Refresh(ctx, id)
}

func (m *Manager) refreshLock(id string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	if lock := m.refreshMu[id]; lock != nil {
		return lock
	}
	lock := &sync.Mutex{}
	m.refreshMu[id] = lock
	return lock
}

func (m *Manager) Call(ctx context.Context, serverID, toolName string, args json.RawMessage) (CallResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	serverID = strings.TrimSpace(serverID)
	m.mu.Lock()
	cfg, configured := m.configs[serverID]
	catalog := append([]Tool(nil), m.catalogs[serverID]...)
	m.mu.Unlock()
	if !configured || !cfg.Enabled || !enabledTool(cfg.EnabledTools, toolName) {
		return CallResult{}, fmt.Errorf("mcp tool %q is disabled for server %q", toolName, serverID)
	}
	// If a catalog is already available, reject stale/unknown names before
	// touching the remote connection. An empty catalog is allowed here because
	// lazy connection may still be needed to discover it.
	if len(catalog) > 0 && !catalogHasEnabledTool(catalog, cfg.EnabledTools, toolName) {
		return CallResult{}, fmt.Errorf("mcp tool %q is not in the enabled catalog for server %q", toolName, serverID)
	}
	m.mu.Lock()
	client := m.clients[serverID]
	m.mu.Unlock()
	if client == nil {
		if _, err := m.RefreshIfNeeded(ctx, serverID); err != nil {
			return CallResult{}, err
		}
		m.mu.Lock()
		client = m.clients[serverID]
		m.mu.Unlock()
	}
	if client == nil {
		return CallResult{}, fmt.Errorf("mcp server %q is not connected", serverID)
	}
	m.mu.Lock()
	cfg, configured = m.configs[serverID]
	enabled := configured && cfg.Enabled && enabledTool(cfg.EnabledTools, toolName)
	m.mu.Unlock()
	if !enabled {
		return CallResult{}, fmt.Errorf("mcp tool %q is disabled for server %q", toolName, serverID)
	}
	m.mu.Lock()
	catalog = append([]Tool(nil), m.catalogs[serverID]...)
	m.mu.Unlock()
	if !catalogHasEnabledTool(catalog, cfg.EnabledTools, toolName) {
		return CallResult{}, fmt.Errorf("mcp tool %q is not in the enabled catalog for server %q", toolName, serverID)
	}
	return client.CallTool(ctx, toolName, args)
}

func (m *Manager) startAndList(ctx context.Context, cfg ServerConfig) (Client, []Tool, error) {
	var client Client
	var err error
	switch cfg.Transport {
	case TransportStdio:
		client, err = NewStdioClient(cfg)
	case TransportStreamableHTTP:
		client, err = NewStreamableHTTPClient(cfg)
	default:
		err = fmt.Errorf("unsupported mcp transport %q", cfg.Transport)
	}
	if err != nil {
		return nil, nil, err
	}
	startCtx, cancel := context.WithTimeout(ctx, defaultOperationTimeout)
	defer cancel()
	if err := client.Start(startCtx); err != nil {
		_ = client.Close()
		return nil, nil, err
	}
	listCtx, cancelList := context.WithTimeout(ctx, defaultOperationTimeout)
	defer cancelList()
	tools, err := client.ListTools(listCtx)
	if err != nil {
		_ = client.Close()
		return nil, nil, err
	}
	seen := map[string]struct{}{}
	seenQualified := map[string]string{}
	for i := range tools {
		if tools[i].InputSchema == nil {
			tools[i].InputSchema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		qualified, err := QualifiedToolName(cfg.ID, tools[i].Name)
		if err != nil {
			_ = client.Close()
			return nil, nil, err
		}
		if previous, exists := seenQualified[qualified]; exists {
			_ = client.Close()
			return nil, nil, fmt.Errorf("mcp server %q returned tools %q and %q with the same LLM name %q", cfg.ID, previous, tools[i].Name, qualified)
		}
		seenQualified[qualified] = tools[i].Name
		if _, exists := seen[tools[i].Name]; exists {
			_ = client.Close()
			return nil, nil, fmt.Errorf("mcp server %q returned duplicate tool %q", cfg.ID, tools[i].Name)
		}
		seen[tools[i].Name] = struct{}{}
	}
	return client, tools, nil
}

func (m *Manager) updateView(id, status, lastError string, tools []Tool) ServerView {
	m.mu.Lock()
	defer m.mu.Unlock()
	view := m.views[id]
	view.Status = status
	view.LastError = strings.TrimSpace(lastError)
	if tools != nil {
		view.Tools = append([]Tool(nil), tools...)
	}
	view.ToolCount = len(view.Tools)
	view.EnabledToolCount = countEnabledTools(view.Tools)
	if status == StatusReady {
		view.LastRefresh = time.Now().UTC().Format(time.RFC3339Nano)
	}
	m.views[id] = view
	return view
}

func markEnabledTools(tools []Tool, enabledNames []string) {
	for i := range tools {
		tools[i].Enabled = enabledTool(enabledNames, tools[i].Name)
	}
}

func enabledTool(enabledNames []string, name string) bool {
	name = strings.TrimSpace(name)
	for _, enabled := range enabledNames {
		if strings.TrimSpace(enabled) == name {
			return true
		}
	}
	return false
}

func countEnabledTools(tools []Tool) int {
	count := 0
	for _, tool := range tools {
		if tool.Enabled {
			count++
		}
	}
	return count
}

func catalogHasEnabledTool(tools []Tool, enabledNames []string, name string) bool {
	for _, tool := range tools {
		if tool.Name == name && enabledTool(enabledNames, tool.Name) {
			return true
		}
	}
	return false
}

func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	clients := make([]Client, 0, len(m.clients))
	for _, client := range m.clients {
		clients = append(clients, client)
	}
	m.clients = map[string]Client{}
	m.mu.Unlock()
	for _, client := range clients {
		_ = client.Close()
	}
}

func sameConfig(a, b ServerConfig) bool {
	return reflect.DeepEqual(a.normalized(), b.normalized())
}

func sameConnectionConfig(a, b ServerConfig) bool {
	a.EnabledTools = nil
	b.EnabledTools = nil
	return sameConfig(a, b)
}
