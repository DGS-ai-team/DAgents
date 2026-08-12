// Package events 为 Shell 提供常驻 SSE 订阅与重连（F-E1/E4）。
package events

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/nodeclient"
	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/pending"
)

const (
	reconnectDelay = 5 * time.Second
	syncInterval   = 60 * time.Second
)

// Subscriber 在 Node 可用时维持全局 SSE，并维护 Agent 待办表。
type Subscriber struct {
	client   *nodeclient.Client
	store    *pending.Store
	onChange func()

	mu     sync.Mutex
	cancel context.CancelFunc
}

// NewSubscriber 构造 SSE 订阅器；onChange 在待办表变更时调用（可 nil）。
func NewSubscriber(client *nodeclient.Client, store *pending.Store, onChange func()) *Subscriber {
	return &Subscriber{
		client:   client,
		store:    store,
		onChange: onChange,
	}
}

// Start 启动后台 SSE 循环与 60s agents 轮询兜底（F-E5）。
func (s *Subscriber) Start(parent context.Context) {
	if s == nil || s.client == nil || s.store == nil {
		return
	}
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.mu.Unlock()

	go s.pollLoop(ctx)
	go s.loop(ctx)
}

// Stop 停止 SSE 订阅与轮询。
func (s *Subscriber) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Subscriber) loop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		if err := s.connectOnce(ctx); err != nil && ctx.Err() == nil {
			log.Printf("shell sse: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(reconnectDelay):
		}
	}
}

func (s *Subscriber) pollLoop(ctx context.Context) {
	s.syncAgents(ctx)
	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.syncAgents(ctx)
		}
	}
}

func (s *Subscriber) connectOnce(ctx context.Context) error {
	s.syncAgents(ctx)
	return s.client.StreamEvents(ctx, func(ev nodeclient.StreamEvent) bool {
		if ctx.Err() != nil {
			return false
		}
		if pending.ShouldSyncOnEvent(ev) || shouldSyncWhileHITLPending(s.store, ev) {
			s.syncAgents(ctx)
		}
		return true
	})
}

// shouldSyncWhileHITLPending：本地仍记着 HITL 时，审批后的 tool_result/tool_call 也立即向 Node 对账，
// 避免等 done/60s 轮询期间因焦点变化重复弹「待处理」Toast。
func shouldSyncWhileHITLPending(store *pending.Store, ev nodeclient.StreamEvent) bool {
	if store == nil || !store.HasPendingHITL() {
		return false
	}
	switch ev.Type {
	case "tool_result", "tool_call":
		return pending.EventHasAgent(ev)
	default:
		return false
	}
}

func (s *Subscriber) syncAgents(ctx context.Context) {
	syncCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	agents, err := s.client.ListAgents(syncCtx)
	if err != nil {
		log.Printf("shell sync agents: %v", err)
		return
	}
	if pending.SyncFromAgents(s.store, agents) {
		s.notifyChange()
	}
}

func (s *Subscriber) notifyChange() {
	if s.onChange != nil {
		s.onChange()
	}
}
