package hooks

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"plugin"
	"strings"
)

const pluginRegisterSymbol = "Register"

// PluginRegistrar 供 .so 插件注册 Hook（与内置 RegisterPhaseHook 同路径）。
type PluginRegistrar struct {
	registry      *Registry
	opts          RegisterOpts
	prefix        string
	allowedPhases []Phase // 非空时与插件声明 phases 取交集
}

// Register 注册单条 Hook。
func (r *PluginRegistrar) Register(h Hook, opts RegisterOpts) {
	if r == nil || r.registry == nil || h == nil {
		return
	}
	opts = opts.normalized()
	if opts.Priority == 0 {
		opts.Priority = r.opts.Priority
	}
	if opts.Timeout <= 0 && r.opts.Timeout > 0 {
		opts.Timeout = r.opts.Timeout
	}
	if opts.OnError == "" && r.opts.OnError != "" {
		opts.OnError = r.opts.OnError
	}
	wrapped := Hook(h)
	if len(r.allowedPhases) > 0 {
		wrapped = newPhaseConstrainedHook(h, r.allowedPhases)
	}
	if r.prefix != "" {
		wrapped = &prefixedHook{prefix: r.prefix, inner: wrapped}
	}
	r.registry.RegisterPhaseHook(wrapped, opts)
}

type prefixedHook struct {
	prefix string
	inner  Hook
}

func (h *prefixedHook) Name() string {
	if h == nil || h.inner == nil {
		return ""
	}
	name := h.inner.Name()
	if h.prefix != "" && !strings.HasPrefix(name, h.prefix) {
		return h.prefix + name
	}
	return name
}

func (h *prefixedHook) Phases() []Phase {
	if h == nil || h.inner == nil {
		return nil
	}
	return h.inner.Phases()
}

func (h *prefixedHook) Run(ctx context.Context, hc *Context, host Host) (Result, error) {
	if h == nil || h.inner == nil {
		return Result{Action: ActionContinue}, nil
	}
	return h.inner.Run(ctx, hc, host)
}

type phaseConstrainedHook struct {
	inner   Hook
	allowed map[Phase]struct{}
}

func newPhaseConstrainedHook(inner Hook, allowed []Phase) Hook {
	set := make(map[Phase]struct{}, len(allowed))
	for _, p := range allowed {
		set[p] = struct{}{}
	}
	return &phaseConstrainedHook{inner: inner, allowed: set}
}

func (h *phaseConstrainedHook) Name() string {
	if h == nil || h.inner == nil {
		return ""
	}
	return h.inner.Name()
}

func (h *phaseConstrainedHook) Phases() []Phase {
	if h == nil || h.inner == nil {
		return nil
	}
	inner := h.inner.Phases()
	if len(h.allowed) == 0 {
		return inner
	}
	out := make([]Phase, 0, len(inner))
	for _, p := range inner {
		if _, ok := h.allowed[p]; ok {
			out = append(out, p)
		}
	}
	return out
}

func (h *phaseConstrainedHook) Run(ctx context.Context, hc *Context, host Host) (Result, error) {
	if h == nil || h.inner == nil {
		return Result{Action: ActionContinue}, nil
	}
	return h.inner.Run(ctx, hc, host)
}

// RegisterPlugins 从配置加载全局 .so 插件。
func RegisterPlugins(r *Registry, cfg PluginsConfig, logger *slog.Logger) error {
	if r == nil || len(cfg.Plugins) == 0 {
		return nil
	}
	for _, entry := range cfg.Plugins {
		if err := loadPluginFile(r, entry, "", logger); err != nil {
			if logger != nil {
				logger.Warn("hooks plugin load failed", "path", entry.Path, "error", err)
			}
			if entry.OnError == OnErrorAbort {
				return err
			}
		}
	}
	return nil
}

// LoadSkillPluginsFromDir 扫描 skills/<name>/hooks/*.so 并注册。
func LoadSkillPluginsFromDir(r *Registry, hooksDir, skillName string, logger *slog.Logger) error {
	result := LoadSkillPluginsFromDirDetailed(r, hooksDir, skillName, logger)
	if len(result.Failed) == 0 {
		return nil
	}
	return fmt.Errorf("%s: %s", result.Failed[0].Path, result.Failed[0].Error)
}

// SkillPluginLoadFailure identifies one hook plugin that failed to register.
type SkillPluginLoadFailure struct {
	Path  string
	Error string
}

// SkillPluginLoadResult keeps successful and failed plugin files separate so
// callers can report the actual registration result instead of treating a
// skill directory with no hooks as a loaded hook.
type SkillPluginLoadResult struct {
	Loaded []string
	Failed []SkillPluginLoadFailure
}

// LoadSkillPluginsFromDirDetailed scans skills/<name>/hooks/*.so and returns
// per-file registration results while continuing after one bad plugin.
func LoadSkillPluginsFromDirDetailed(r *Registry, hooksDir, skillName string, logger *slog.Logger) SkillPluginLoadResult {
	result := SkillPluginLoadResult{}
	if r == nil || strings.TrimSpace(hooksDir) == "" || strings.TrimSpace(skillName) == "" {
		return result
	}
	entries, err := filepath.Glob(filepath.Join(hooksDir, "*.so"))
	if err != nil {
		result.Failed = append(result.Failed, SkillPluginLoadFailure{Path: hooksDir, Error: err.Error()})
		return result
	}
	prefix := SkillHookNamePrefix + strings.TrimSpace(skillName) + "/"
	for _, path := range entries {
		entry := PluginHookEntry{Path: path}
		if err := loadPluginFile(r, entry, prefix, logger); err != nil {
			result.Failed = append(result.Failed, SkillPluginLoadFailure{Path: path, Error: err.Error()})
			if logger != nil {
				logger.Warn("skill hook plugin load failed", "skill", skillName, "path", path, "error", err)
			}
			continue
		}
		result.Loaded = append(result.Loaded, path)
	}
	return result
}

func loadPluginFile(r *Registry, entry PluginHookEntry, namePrefix string, logger *slog.Logger) error {
	path := strings.TrimSpace(entry.Path)
	if path == "" {
		return fmt.Errorf("hooks: empty plugin path")
	}
	p, err := plugin.Open(path)
	if err != nil {
		return fmt.Errorf("plugin.Open %q: %w", path, err)
	}
	sym, err := p.Lookup(pluginRegisterSymbol)
	if err != nil {
		return fmt.Errorf("plugin.Lookup Register: %w", err)
	}
	regFn, ok := sym.(func(*PluginRegistrar) error)
	if !ok {
		return fmt.Errorf("plugin Register symbol has wrong type")
	}
	reg := &PluginRegistrar{
		registry:      r,
		opts:          pluginRegisterOpts(entry),
		prefix:        namePrefix,
		allowedPhases: append([]Phase(nil), entry.Phases...),
	}
	if err := regFn(reg); err != nil {
		return fmt.Errorf("plugin Register: %w", err)
	}
	if logger != nil {
		logger.Info("hooks plugin loaded", "path", path, "prefix", namePrefix)
	}
	return nil
}
