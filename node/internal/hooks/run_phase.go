package hooks

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// DefaultInlineHookTimeout 为内置 / command hook 建议的单 hook 超时（设计 §5）。
const DefaultInlineHookTimeout = 500 * time.Millisecond

// PhaseAbortError 表示 Hook 链以 abort_* action 终止 phase。
type PhaseAbortError struct {
	Phase  Phase
	Action Action
	Hook   string
	Cause  error
}

func (e *PhaseAbortError) Error() string {
	if e == nil {
		return "hooks: phase aborted"
	}
	msg := fmt.Sprintf("hooks: phase %s aborted by %q (%s)", e.Phase, e.Hook, e.Action)
	if e.Cause != nil {
		return msg + ": " + e.Cause.Error()
	}
	return msg
}

func (e *PhaseAbortError) Unwrap() error { return e.Cause }

// RegisterPhaseHook 按 priority 升序注册通用 phase Hook（数值越小越先执行）。
func (r *Registry) RegisterPhaseHook(h Hook, opts RegisterOpts) {
	if r == nil || h == nil {
		return
	}
	opts = opts.normalized()
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultInlineHookTimeout
	}
	r.phaseHooks = append(r.phaseHooks, registeredPhaseHook{
		hook:       h,
		opts:       opts,
		timeoutDur: timeout,
	})
}

// SetExecutionJournal 注入幂等 journal；nil 时使用 NoopExecutionJournal。
func (r *Registry) SetExecutionJournal(j ExecutionJournal) {
	if r == nil {
		return
	}
	if j == nil {
		r.journal = NoopExecutionJournal{}
		return
	}
	r.journal = j
}

// RunPhase 按 priority 顺序执行匹配 phase 的 Hook 链，合并 mutation 并处理 Action 语义。
func (r *Registry) RunPhase(ctx context.Context, phase Phase, hc *Context) (Context, error) {
	if hc == nil {
		return Context{}, fmt.Errorf("hooks: RunPhase requires non-nil Context")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	out := *hc
	out.Phase = phase
	if r == nil {
		return out, nil
	}
	journal := r.journal
	if journal == nil {
		journal = NoopExecutionJournal{}
	}
	for _, reg := range r.phaseHooksFor(phase) {
		name := reg.hook.Name()
		if reg.opts.SideEffect && journal.HasExecuted(out.TurnID, phase, name) {
			continue
		}
		hookCtx := ctx
		var cancel context.CancelFunc
		if reg.timeoutDur > 0 {
			hookCtx, cancel = context.WithTimeout(ctx, reg.timeoutDur)
		}
		result, err := reg.hook.Run(hookCtx, &out)
		if cancel != nil {
			cancel()
		}
		if err != nil {
			if reg.opts.OnError == OnErrorAbort {
				return out, fmt.Errorf("hooks: %q on %s: %w", name, phase, err)
			}
			continue
		}
		if err := applyMutations(&out, result.Mutations); err != nil {
			if reg.opts.OnError == OnErrorAbort {
				return out, fmt.Errorf("hooks: %q mutation on %s: %w", name, phase, err)
			}
			continue
		}
		action := normalizeAction(result.Action)
		switch action {
		case ActionContinue:
			// fall through to journal mark
		case ActionSkip:
			if reg.opts.SideEffect {
				journal.MarkExecuted(out.TurnID, phase, name)
			}
			return out, nil
		default:
			if action.IsAbort() {
				cause := result.Err
				if cause == nil {
					cause = errors.New(string(action))
				}
				return out, &PhaseAbortError{
					Phase:  phase,
					Action: action,
					Hook:   name,
					Cause:  cause,
				}
			}
		}
		if reg.opts.SideEffect {
			journal.MarkExecuted(out.TurnID, phase, name)
		}
	}
	return out, nil
}

func (r *Registry) phaseHooksFor(phase Phase) []registeredPhaseHook {
	if r == nil || len(r.phaseHooks) == 0 {
		return nil
	}
	var matched []registeredPhaseHook
	for _, reg := range r.phaseHooks {
		if hookSupportsPhase(reg.hook, phase) {
			matched = append(matched, reg)
		}
	}
	if len(matched) <= 1 {
		return matched
	}
	// insertion sort by priority (stable, small n)
	for i := 1; i < len(matched); i++ {
		cur := matched[i]
		j := i - 1
		for j >= 0 && matched[j].opts.Priority > cur.opts.Priority {
			matched[j+1] = matched[j]
			j--
		}
		matched[j+1] = cur
	}
	return matched
}

func hookSupportsPhase(h Hook, phase Phase) bool {
	if h == nil {
		return false
	}
	for _, p := range h.Phases() {
		if p == phase {
			return true
		}
	}
	return false
}
