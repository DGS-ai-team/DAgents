package hooks

import (
	"context"
	"time"
)

// Hook 为通用 phase Hook 契约。
type Hook interface {
	Name() string
	Phases() []Phase
	Run(ctx context.Context, hc *Context) (Result, error)
}

// OnErrorPolicy 控制 Hook 执行失败时的 fail-open / fail-closed 策略。
type OnErrorPolicy string

const (
	OnErrorContinue OnErrorPolicy = "continue"
	OnErrorAbort    OnErrorPolicy = "abort"
)

// RegisterOpts 注册通用 phase Hook 时的选项。
type RegisterOpts struct {
	Priority   int
	Timeout    time.Duration
	OnError    OnErrorPolicy
	SideEffect bool // 为 true 时写入 ExecutionJournal，resume 跳过重复副作用
}

func (o RegisterOpts) normalized() RegisterOpts {
	out := o
	if out.OnError == "" {
		out.OnError = OnErrorContinue
	}
	return out
}

type registeredPhaseHook struct {
	hook       Hook
	opts       RegisterOpts
	timeoutDur time.Duration
}
