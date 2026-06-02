package repl

import (
	"context"
	"sync"
)

// turnGate 在「用户已提交消息 → 收到 done」期间阻塞主循环读 stdin，
// 以便 HITL（审批/询问）独占 stdin 完成整条 turn。
type turnGate struct {
	mu      sync.Mutex
	waiting bool
	done    chan struct{}
}

// begin 标记 turn 开始；重复调用忽略。
func (g *turnGate) begin() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.waiting {
		return
	}
	g.waiting = true
	g.done = make(chan struct{})
}

// finish 通知 turn 结束；无进行中的 turn 时为 no-op。
func (g *turnGate) finish() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.waiting {
		return
	}
	close(g.done)
	g.waiting = false
	g.done = nil
}

// wait 阻塞至 finish 或 ctx 取消。
func (g *turnGate) wait(ctx context.Context) error {
	g.mu.Lock()
	ch := g.done
	g.mu.Unlock()
	if ch == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
		return nil
	}
}
