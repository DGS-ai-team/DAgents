package hooks

import (
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

const (
	promptBuildPriorityBuiltin = -1000
	defaultPluginHookPriority  = 1000
)

// PluginHookEntry 为单条 in-process plugin 配置。
type PluginHookEntry struct {
	Path     string
	Phases   []Phase
	Priority int
	OnError  OnErrorPolicy
	Timeout  time.Duration
}

// PluginsConfig 为 YAML 驱动的全局 plugin 列表。
type PluginsConfig struct {
	RuntimeDir string
	Plugins    []PluginHookEntry
}

// PluginsConfigFromShared 将 shared/config HooksConfig 映射为 plugin 配置。
func PluginsConfigFromShared(h config.HooksConfig, runtimeDir string) PluginsConfig {
	out := PluginsConfig{
		RuntimeDir: strings.TrimSpace(runtimeDir),
	}
	for _, p := range h.Plugins {
		entry, ok := PluginHookEntryFromConfig(p, out.RuntimeDir)
		if !ok {
			continue
		}
		out.Plugins = append(out.Plugins, entry)
	}
	return out
}

// PluginHookEntryFromConfig 映射单条 plugin 配置。
func PluginHookEntryFromConfig(p config.HookPluginConfig, runtimeDir string) (PluginHookEntry, bool) {
	path := strings.TrimSpace(p.Path)
	if path == "" {
		return PluginHookEntry{}, false
	}
	if !filepathIsAbs(path) && runtimeDir != "" && !strings.HasPrefix(path, "/") {
		path = joinPath(runtimeDir, path)
	}
	entry := PluginHookEntry{
		Path:     path,
		Priority: p.Priority,
		OnError:  parseOnErrorPolicy(p.OnError),
	}
	if p.TimeoutMS > 0 {
		entry.Timeout = time.Duration(p.TimeoutMS) * time.Millisecond
	}
	for _, ph := range p.Phases {
		if phase, ok := ParsePhase(ph); ok {
			entry.Phases = append(entry.Phases, phase)
		}
	}
	if len(entry.Phases) == 0 {
		return PluginHookEntry{}, false
	}
	return entry, true
}

// PromptBuildPriorityBuiltin 为 builtin system_prompt hook 的 priority（最先执行）。
func PromptBuildPriorityBuiltin() int { return promptBuildPriorityBuiltin }

// ParsePhase 解析 phase 字符串。
func ParsePhase(raw string) (Phase, bool) {
	p := Phase(strings.TrimSpace(raw))
	switch p {
	case PhaseMessageEnqueued, PhaseTurnBeforeCompress, PhaseTurnBeforeStep,
		PhasePromptBuild, PhaseLLMBeforeCall, PhaseLLMAfterCall,
		PhaseToolBeforeEach, PhaseToolAfterEach,
		PhaseHITLBeforePause, PhaseHITLAfterResume,
		PhaseTurnDone, PhaseTurnError, PhaseTurnCancel, PhaseSessionLifecycle:
		return p, true
	default:
		return "", false
	}
}

func parseOnErrorPolicy(raw string) OnErrorPolicy {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "abort", "fail", "fail_closed", "fail-closed":
		return OnErrorAbort
	default:
		return OnErrorContinue
	}
}

func pluginRegisterOpts(entry PluginHookEntry) RegisterOpts {
	timeout := entry.Timeout
	if timeout <= 0 {
		timeout = DefaultInlineHookTimeout
	}
	priority := entry.Priority
	if priority == 0 {
		priority = defaultPluginHookPriority
	}
	return RegisterOpts{
		Priority: priority,
		Timeout:  timeout,
		OnError:  entry.OnError,
	}
}

func filepathIsAbs(path string) bool {
	return strings.HasPrefix(path, "/") || (len(path) >= 2 && path[1] == ':')
}

func joinPath(base, rel string) string {
	if base == "" {
		return rel
	}
	if rel == "" {
		return base
	}
	if strings.HasSuffix(base, "/") {
		return base + strings.TrimPrefix(rel, "/")
	}
	return base + "/" + strings.TrimPrefix(rel, "/")
}
