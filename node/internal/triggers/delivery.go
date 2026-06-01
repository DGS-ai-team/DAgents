package triggers

import "sync"

// DeliveryTracker 跟踪 trigger 消息是否仍在 session 队列中待 dequeue 消费。

// 仅进程内有效；Node 重启后队列为空，pending 一并清空。
type DeliveryTracker interface {
	HasPendingDelivery(triggerID string) bool
	MarkPendingDelivery(triggerID string)
	ClearPendingDelivery(triggerID string)
}

// pendingDelivery 为 Store 上的待消费标记（不写入 triggers.json）。
type pendingDelivery struct {
	mu      sync.RWMutex
	pending map[string]struct{}
}

func newPendingDelivery() *pendingDelivery {
	return &pendingDelivery{pending: make(map[string]struct{})}
}

// HasPendingDelivery 判断 trigger 是否已有尚未 dequeue 的投递。
func (p *pendingDelivery) HasPendingDelivery(triggerID string) bool {
	if p == nil || triggerID == "" {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.pending[triggerID]
	return ok
}

// MarkPendingDelivery 在成功入队后标记 trigger 待消费。
func (p *pendingDelivery) MarkPendingDelivery(triggerID string) {
	if p == nil || triggerID == "" {
		return
	}
	p.mu.Lock()
	p.pending[triggerID] = struct{}{}
	p.mu.Unlock()
}

// ClearPendingDelivery 在 session 从队列取出 trigger 消息后清除标记。
func (p *pendingDelivery) ClearPendingDelivery(triggerID string) {
	if p == nil || triggerID == "" {
		return
	}
	p.mu.Lock()
	delete(p.pending, triggerID)
	p.mu.Unlock()
}
