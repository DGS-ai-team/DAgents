package hooks

import (
	"log/slog"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

const (
	promptBuildPriorityBuiltin = -1000
	defaultExternalHookPriority = 1000
	defaultHTTPHookTimeout      = 5 * time.Second
)

// ExternalHooksConfig 为 YAML 驱动的外部 Hook 注册配置。
type ExternalHooksConfig struct {
	Enabled    bool
	RuntimeDir string
	Entries    []ExternalHookEntry
}

// ExternalHookEntry 为单条外部 Hook 配置（由 shared/config 映射而来）。
type ExternalHookEntry struct {
	Name         string
	Type         string
	Phases       []Phase
	Priority     int
	OnError      OnErrorPolicy
	Timeout      time.Duration
	URL          string
	Command      []string
	AllowedPaths []string
	JournalPath  string
}

// ExternalDeps 为外部 Hook 运行时依赖。
type ExternalDeps struct {
	Logger *slog.Logger
}

// ExternalHooksConfigFromShared 将 shared/config HooksConfig 映射为 hooks 包配置。
func ExternalHooksConfigFromShared(h config.HooksConfig, runtimeDir string) ExternalHooksConfig {
	out := ExternalHooksConfig{
		Enabled:    h.Enabled != nil && *h.Enabled,
		RuntimeDir: strings.TrimSpace(runtimeDir),
	}
	for _, e := range h.Entries {
		entry := ExternalHookEntry{
			Name:         strings.TrimSpace(e.Name),
			Type:         strings.ToLower(strings.TrimSpace(e.Type)),
			Priority:     e.Priority,
			OnError:      parseOnErrorPolicy(e.OnError),
			URL:          strings.TrimSpace(e.URL),
			Command:      append([]string(nil), e.Command...),
			AllowedPaths: append([]string(nil), e.AllowedPaths...),
			JournalPath:  strings.TrimSpace(e.JournalPath),
		}
		if e.TimeoutMS > 0 {
			entry.Timeout = time.Duration(e.TimeoutMS) * time.Millisecond
		}
		for _, ph := range e.Phases {
			if p, ok := ParsePhase(ph); ok {
				entry.Phases = append(entry.Phases, p)
			}
		}
		if entry.Name == "" || entry.Type == "" || len(entry.Phases) == 0 {
			continue
		}
		out.Entries = append(out.Entries, entry)
	}
	return out
}

func parseOnErrorPolicy(raw string) OnErrorPolicy {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "abort", "fail", "fail_closed", "fail-closed":
		return OnErrorAbort
	default:
		return OnErrorContinue
	}
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

// RegisterExternalEntries 按 YAML 配置注册 journal / http / command Hook。
func RegisterExternalEntries(r *Registry, cfg ExternalHooksConfig, deps ExternalDeps) {
	if r == nil || len(cfg.Entries) == 0 {
		return
	}
	for _, entry := range cfg.Entries {
		registerExternalEntry(r, cfg, entry, deps)
	}
}

func registerExternalEntry(r *Registry, cfg ExternalHooksConfig, entry ExternalHookEntry, deps ExternalDeps) {
	switch entry.Type {
	case "journal":
		h := newJournalHook(entry, cfg.RuntimeDir, deps.Logger)
		if h == nil {
			return
		}
		r.RegisterPhaseHook(h, externalRegisterOpts(entry))
	case "http":
		if !cfg.Enabled {
			return
		}
		h := newHTTPHook(entry, deps.Logger)
		if h == nil {
			return
		}
		r.RegisterPhaseHook(h, externalRegisterOpts(entry))
	case "command":
		if !cfg.Enabled {
			return
		}
		h := newCommandHook(entry, deps.Logger)
		if h == nil {
			return
		}
		r.RegisterPhaseHook(h, externalRegisterOpts(entry))
	}
}

func externalRegisterOpts(entry ExternalHookEntry) RegisterOpts {
	timeout := entry.Timeout
	if timeout <= 0 {
		timeout = DefaultInlineHookTimeout
	}
	if entry.Type == "http" && entry.Timeout <= 0 {
		timeout = defaultHTTPHookTimeout
	}
	priority := entry.Priority
	if priority == 0 && entry.Type != "journal" {
		priority = defaultExternalHookPriority
	}
	opts := RegisterOpts{
		Priority: priority,
		Timeout:  timeout,
		OnError:  entry.OnError,
	}
	if entry.Type == "journal" {
		opts.SideEffect = false
	}
	return opts
}
