// Package hitl 提供 turn 暂停时 resume 等待与投递（N4）。
package hitl

import (
	"context"
	"sync"
)

// Waiter 允许 orchestrator 阻塞等待 Client resume；EnqueueMessage resume 可直投。
type Waiter struct {
	mu sync.Mutex
	ch chan map[string]any
}

// Wait 阻塞直到 Deliver 或 ctx 取消。

// 副作用：注册内部 channel；返回后清除注册。
func (w *Waiter) Wait(ctx context.Context) (map[string]any, error) {
	w.mu.Lock()
	w.ch = make(chan map[string]any, 1)
	ch := w.ch
	w.mu.Unlock()

	defer w.clear()

	select {
	case v := <-ch:
		return v, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Deliver 向等待中的 turn 投递 resume_value；无等待者时返回 false。
func (w *Waiter) Deliver(value map[string]any) bool {
	w.mu.Lock()
	ch := w.ch
	w.mu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case ch <- value:
		return true
	default:
		return false
	}
}

// IsWaiting 表示是否有 turn 正在等待 resume。
func (w *Waiter) IsWaiting() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ch != nil
}

func (w *Waiter) clear() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ch = nil
}
