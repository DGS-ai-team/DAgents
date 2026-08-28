// Package queue 提供 per-session 优先级消息队列（对齐 Python MessageQueue 语义子集）。
package queue

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// Priority 为入队优先级标签。
type Priority string

const (
	PriorityContinuation    Priority = "continuation"
	PriorityHuman           Priority = "human"
	PriorityResume          Priority = "resume"
	PriorityAsyncCompletion Priority = "async_completion"
	PriorityOther           Priority = "other"
)

// Envelope 为单条入队载荷。
type Envelope struct {
	RequestType string
	// SessionEpoch 用于使 clear-context 之前排队的事件失效。
	SessionEpoch uint64
	// TurnID/Generation 仅用于需要绑定当前 turn 的内部 continuation。
	// async callback 只绑定 SessionEpoch，因为它可以在原 Turn cancel 后恢复。
	TurnID                   string
	Generation               uint64
	Content                  string
	ContentParts             []llm.ContentPart
	UserName                 string // request_type=message 时写入 llm.Message.Name；空串由 runtime 规范为 human
	ResumeValue              map[string]any
	TriggerID                string // 非空表示 trigger fire 投递；输入被消费后清除 pending 标记
	AsyncToolResult          *AsyncToolResultPayload
	SideEffectContinueSource string // side_effect_continue 来源（task_complete_produce / cancel_recovery 等）
}

type queuedItem struct {
	priority int
	seq      uint64
	env      Envelope
}

// MessageQueue 进程内优先级队列；仅入队与阻塞出队，无内嵌 consumer。
type MessageQueue struct {
	mu     sync.Mutex
	seq    uint64
	items  []queuedItem
	notify chan struct{}
	closed bool
}

// NewMessageQueue 创建空队列。
func NewMessageQueue() *MessageQueue {
	return &MessageQueue{notify: make(chan struct{}, 1)}
}

// Enqueue 非阻塞入队；队列关闭后返回 error。
func (q *MessageQueue) Enqueue(env Envelope, priority Priority) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return errors.New("queue closed")
	}
	q.seq++
	item := queuedItem{
		priority: priorityValue(priority),
		seq:      q.seq,
		env:      env,
	}
	q.items = append(q.items, item)
	sort.SliceStable(q.items, func(i, j int) bool {
		if q.items[i].priority != q.items[j].priority {
			return q.items[i].priority < q.items[j].priority
		}
		return q.items[i].seq < q.items[j].seq
	})
	select {
	case q.notify <- struct{}{}:
	default:
	}
	return nil
}

// Dequeue 阻塞直到有消息或 ctx 取消/队列关闭。
func (q *MessageQueue) Dequeue(ctx context.Context) (Envelope, error) {
	for {
		q.mu.Lock()
		if len(q.items) > 0 {
			env := q.items[0].env
			q.items = q.items[1:]
			q.mu.Unlock()
			return env, nil
		}
		if q.closed {
			q.mu.Unlock()
			return Envelope{}, errors.New("queue closed")
		}
		q.mu.Unlock()

		select {
		case <-ctx.Done():
			return Envelope{}, ctx.Err()
		case <-q.notify:
		}
	}
}

// Wake exposes the coalesced enqueue notification to a runtime that also
// listens to another input source.  The notification is only a hint; callers
// must re-check Len and then call Dequeue so no payload is lost.
func (q *MessageQueue) Wake() <-chan struct{} {
	if q == nil {
		return nil
	}
	return q.notify
}

// Closed reports whether no further envelopes can be enqueued.
func (q *MessageQueue) Closed() bool {
	if q == nil {
		return true
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.closed
}

// Close 关闭队列；唤醒等待中的 Dequeue。
func (q *MessageQueue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

// CountByRequestType 统计队列中指定 request_type 的条数（诊断 resume 是否重复入队）。
func (q *MessageQueue) CountByRequestType(requestType string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := 0
	for _, item := range q.items {
		if item.env.RequestType == requestType {
			n++
		}
	}
	return n
}

// Len 返回当前队列深度（测试/观测用）。
func (q *MessageQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// TotalEnqueued 返回累计入队次数（含已被 consumer 取走的条目；测试断言「只入队一次」时用）。
func (q *MessageQueue) TotalEnqueued() uint64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.seq
}

func priorityValue(p Priority) int {
	switch p {
	case PriorityContinuation:
		return -1
	case PriorityHuman:
		return 0
	case PriorityResume:
		return 1
	case PriorityAsyncCompletion:
		return 2
	default:
		return 10
	}
}

// ParsePriority 解析显式 priority 字段；空串或未知值返回 false。
func ParsePriority(raw string) (Priority, bool) {
	p := Priority(strings.TrimSpace(raw))
	switch p {
	case PriorityContinuation, PriorityHuman, PriorityResume, PriorityAsyncCompletion, PriorityOther:
		return p, true
	default:
		return "", false
	}
}

// PriorityForRequestType 将 HTTP request_type 映射为队列优先级。
func PriorityForRequestType(requestType string) (Priority, error) {
	switch requestType {
	case "message":
		return PriorityHuman, nil
	case "resume":
		return PriorityResume, nil
	default:
		return "", fmt.Errorf("invalid_request_type")
	}
}
