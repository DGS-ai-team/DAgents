package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultOperationTimeout = 30 * time.Second

type operationError struct {
	Stage       string
	FailureKind string
	Retryable   bool
	Stderr      string
	ExitCode    *int
	Err         error
}

func (e *operationError) Error() string {
	if e == nil || e.Err == nil {
		return "mcp operation failed"
	}
	return e.Err.Error()
}

func (e *operationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type operationDiagnostic struct {
	Stage       string
	FailureKind string
	Retryable   bool
	Stderr      string
	ExitCode    *int
}

func diagnosticForError(err error) operationDiagnostic {
	if err == nil {
		return operationDiagnostic{}
	}
	var opErr *operationError
	if !errors.As(err, &opErr) {
		return operationDiagnostic{Stage: "unknown", FailureKind: "unknown", Retryable: true}
	}
	diagnostic := operationDiagnostic{Stage: opErr.Stage, FailureKind: opErr.FailureKind, Retryable: opErr.Retryable, Stderr: opErr.Stderr, ExitCode: opErr.ExitCode}
	if diagnostic.Stage == "" {
		diagnostic.Stage = "unknown"
	}
	if diagnostic.FailureKind == "" {
		diagnostic.FailureKind = "unknown"
	}
	return diagnostic
}

type Manager struct {
	mu             sync.Mutex
	configs        map[string]ServerConfig
	clients        map[string]Client
	catalogs       map[string][]Tool
	views          map[string]ServerView
	refreshMu      map[string]*sync.Mutex
	logger         *slog.Logger
	statusRevision uint64
	onStatusChange func(StatusEvent)
	closed         bool
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

// SetStatusListener receives Node-level MCP health transitions. The callback
// is invoked outside the manager mutex and must not mutate the Manager.
func (m *Manager) SetStatusListener(listener func(StatusEvent)) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.onStatusChange = listener
	m.mu.Unlock()
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

// Health returns an aggregate over enabled services. Disabled services are
// configuration state, not an outage; with no enabled service the Node is
// reported as unconfigured.
func (m *Manager) Health() HealthView {
	if m == nil {
		return HealthView{Status: HealthUnconfigured}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.healthLocked()
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
	m.updateView(id, StatusChecking, "", nil)
	client, tools, err := m.startAndList(ctx, cfg)
	if err != nil {
		view := m.updateViewWithFailure(id, StatusError, err, nil)
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
	m.updateView(id, StatusChecking, "", nil)
	client, tools, err := m.startAndList(ctx, cfg)
	if client != nil {
		_ = client.Close()
	}
	if err != nil {
		view := m.updateViewWithFailure(id, StatusError, err, nil)
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
	result, err := client.CallTool(ctx, toolName, args)
	if err == nil {
		return result, nil
	}
	failure := classifyClientOperationError("call", err, client)
	m.mu.Lock()
	if m.clients[serverID] == client {
		delete(m.clients, serverID)
	}
	m.mu.Unlock()
	_ = client.Close()
	m.updateViewWithFailure(serverID, StatusError, failure, catalog)
	return CallResult{}, failure
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
		return nil, nil, &operationError{Stage: "configure", FailureKind: "configuration", Retryable: false, Err: err}
	}
	startCtx, cancel := context.WithTimeout(ctx, defaultOperationTimeout)
	defer cancel()
	if err := client.Start(startCtx); err != nil {
		failure := classifyClientOperationError("initialize", err, client)
		_ = client.Close()
		return nil, nil, failure
	}
	listCtx, cancelList := context.WithTimeout(ctx, defaultOperationTimeout)
	defer cancelList()
	tools, err := client.ListTools(listCtx)
	if err != nil {
		failure := classifyClientOperationError("list_tools", err, client)
		_ = client.Close()
		return nil, nil, failure
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
			return nil, nil, &operationError{Stage: "catalog", FailureKind: "invalid_catalog", Retryable: false, Err: err}
		}
		if previous, exists := seenQualified[qualified]; exists {
			_ = client.Close()
			return nil, nil, &operationError{Stage: "catalog", FailureKind: "invalid_catalog", Retryable: false, Err: fmt.Errorf("mcp server %q returned tools %q and %q with the same LLM name %q", cfg.ID, previous, tools[i].Name, qualified)}
		}
		seenQualified[qualified] = tools[i].Name
		if _, exists := seen[tools[i].Name]; exists {
			_ = client.Close()
			return nil, nil, &operationError{Stage: "catalog", FailureKind: "invalid_catalog", Retryable: false, Err: fmt.Errorf("mcp server %q returned duplicate tool %q", cfg.ID, tools[i].Name)}
		}
		seen[tools[i].Name] = struct{}{}
	}
	return client, tools, nil
}

func classifyOperationError(stage string, err error) error {
	if err == nil {
		return nil
	}
	lower := strings.ToLower(err.Error())
	kind := "transport"
	retryable := true
	switch {
	case strings.Contains(lower, "postinstall") || strings.Contains(lower, "npm install") || strings.Contains(lower, "install script"):
		kind = "installation"
		retryable = false
	case strings.Contains(lower, "deadline") || strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out"):
		kind = "timeout"
	case strings.Contains(lower, "auth") || strings.Contains(lower, "credential") || strings.Contains(lower, "permission") || strings.Contains(lower, "forbidden") || strings.Contains(lower, "401") || strings.Contains(lower, "403"):
		kind = "authentication"
		retryable = false
	case strings.Contains(lower, "invalid") || strings.Contains(lower, "unsupported") || strings.Contains(lower, "duplicate"):
		kind = "configuration"
		retryable = false
	}
	return &operationError{Stage: stage, FailureKind: kind, Retryable: retryable, Err: err}
}

func classifyClientOperationError(stage string, err error, client Client) error {
	classified := classifyOperationError(stage, err)
	opErr, ok := classified.(*operationError)
	if !ok || client == nil {
		return classified
	}
	if provider, ok := client.(DiagnosticsProvider); ok {
		diagnostics := provider.Diagnostics()
		opErr.Stderr = diagnostics.Stderr
		opErr.ExitCode = diagnostics.ExitCode
		combined := strings.TrimSpace(strings.Join([]string{err.Error(), diagnostics.Stderr}, "\n"))
		if combined != "" {
			reclassified := classifyOperationError(stage, errors.New(combined)).(*operationError)
			reclassified.Stderr = diagnostics.Stderr
			reclassified.ExitCode = diagnostics.ExitCode
			return reclassified
		}
	}
	return opErr
}

func (m *Manager) updateView(id, status, lastError string, tools []Tool) ServerView {
	var failure error
	if strings.TrimSpace(lastError) != "" {
		failure = errors.New(lastError)
	}
	return m.updateViewWithFailure(id, status, failure, tools)
}

func (m *Manager) updateViewWithFailure(id, status string, failure error, tools []Tool) ServerView {
	var event StatusEvent
	var listener func(StatusEvent)
	m.mu.Lock()
	view := m.views[id]
	previousStatus := view.Status
	previousError := view.LastError
	previousStage := view.HealthStage
	previousFailureKind := view.FailureKind
	previousRetryable := view.Retryable
	view.Status = status
	view.LastError = ""
	if failure != nil {
		view.LastError = strings.TrimSpace(failure.Error())
	}
	view.HealthStage = ""
	view.FailureKind = ""
	view.Retryable = false
	view.StderrSummary = ""
	view.ExitCode = nil
	if status == StatusChecking {
		view.HealthStage = "checking"
	} else if status == StatusReady {
		view.HealthStage = "ready"
	} else if failure != nil {
		diagnostic := diagnosticForError(failure)
		view.HealthStage = diagnostic.Stage
		view.FailureKind = diagnostic.FailureKind
		view.Retryable = diagnostic.Retryable
		view.StderrSummary = diagnostic.Stderr
		view.ExitCode = diagnostic.ExitCode
	}
	if tools != nil {
		view.Tools = append([]Tool(nil), tools...)
	}
	view.ToolCount = len(view.Tools)
	view.EnabledToolCount = countEnabledTools(view.Tools)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	view.LastChecked = now
	view.ObservedAt = now
	if status == StatusReady {
		view.LastRefresh = now
	}
	if previousStatus != view.Status || previousError != view.LastError || previousStage != view.HealthStage || previousFailureKind != view.FailureKind || previousRetryable != view.Retryable {
		m.statusRevision++
		view.StatusRevision = m.statusRevision
		event = StatusEvent{ServerID: id, View: view, Revision: m.statusRevision, ObservedAt: now}
		event.Health = m.healthLockedWithOverride(id, view)
		listener = m.onStatusChange
	}
	m.views[id] = view
	m.mu.Unlock()
	if listener != nil && event.Revision > 0 {
		listener(event)
	}
	return view
}

func (m *Manager) healthLocked() HealthView {
	health := HealthView{Status: HealthUnconfigured, Revision: m.statusRevision}
	for _, view := range m.views {
		health.ServerCount++
		if !view.Enabled {
			continue
		}
		health.EnabledCount++
		switch view.Status {
		case StatusReady:
			health.HealthyCount++
		case StatusChecking:
			health.CheckingCount++
		case StatusError, StatusOffline:
			health.ProblemCount++
			if view.Retryable {
				health.RetryableProblemCount++
			}
		}
		if view.ObservedAt > health.ObservedAt {
			health.ObservedAt = view.ObservedAt
		}
	}
	if health.EnabledCount == 0 {
		health.Status = HealthUnconfigured
	} else if health.CheckingCount > 0 {
		health.Status = HealthChecking
	} else if health.ProblemCount > 0 {
		health.Status = HealthDegraded
	} else if health.HealthyCount == health.EnabledCount {
		health.Status = HealthHealthy
	} else {
		health.Status = HealthDegraded
	}
	return health
}

func (m *Manager) healthLockedWithOverride(id string, override ServerView) HealthView {
	original, exists := m.views[id]
	m.views[id] = override
	health := m.healthLocked()
	if exists {
		m.views[id] = original
	} else {
		delete(m.views, id)
	}
	return health
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
