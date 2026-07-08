// Package stream 提供进程内 SSE 事件总线（N1 全局单流；事件带 session_id）。
package stream

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/logx"
)

const defaultHistorySize = 256

// Event 为写入 SSE 的标准事件结构（无 connection_id）。
type Event struct {
	SessionID string         `json:"session_id"`
	AgentID   string         `json:"agent_id"`
	Type      string         `json:"type"`
	Seq       int            `json:"seq"`
	TS        string         `json:"ts"`
	Data      map[string]any `json:"data"`
}

// Hub 维护全局递增 seq、历史缓冲与订阅者 fan-out。
type Hub struct {
	mu       sync.RWMutex
	seq      int
	history  []Event
	historyN int
	subs     map[chan Event]struct{}
	logger   *slog.Logger
	onPublish func(Event)
}

// NewHub 创建事件总线；historyN 为回放上限，≤0 时用默认值；logger 可为 nil。
func NewHub(historyN int, logger *slog.Logger) *Hub {
	if historyN <= 0 {
		historyN = defaultHistorySize
	}
	return &Hub{
		historyN: historyN,
		subs:     make(map[chan Event]struct{}),
		logger:   logx.OrDefault(logger),
	}
}

// SetEventListener 注册 Publish 后回调（如 F-E13 notify_seq 推进）；fn 可为 nil。
func (h *Hub) SetEventListener(fn func(Event)) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.onPublish = fn
	h.mu.Unlock()
}

// Publish 分配 seq、写入历史并投递给全部订阅者。

// 逻辑：
// 1. 构造 Event 并递增 seq；
// 2. 追加 history（超上限则截断头部）；
// 3. 非阻塞写入各订阅 channel，慢消费者丢事件。
//
// 副作用：修改 hub.seq 与 hub.history。
func (h *Hub) Publish(sessionID, agentID, eventType string, data map[string]any) Event {
	if data == nil {
		data = map[string]any{}
	}
	h.mu.Lock()

	h.seq++
	ev := Event{
		SessionID: sessionID,
		AgentID:   agentID,
		Type:      eventType,
		Seq:       h.seq,
		TS:        time.Now().UTC().Format(time.RFC3339Nano),
		Data:      data,
	}
	h.history = append(h.history, ev)
	if len(h.history) > h.historyN {
		h.history = h.history[len(h.history)-h.historyN:]
	}
	for ch := range h.subs {
		select {
		case ch <- ev:
		default:
			// 慢消费者丢弃，避免阻塞 turn。
		}
	}
	listener := h.onPublish
	h.mu.Unlock()

	h.logger.Debug("stream publish",
		"session_id", sessionID,
		"type", eventType,
		"seq", ev.Seq,
	)
	if listener != nil {
		listener(ev)
	}
	return ev
}

// CurrentSeq 返回 hub 当前已分配的最大 seq（无事件时为 0）。
func (h *Hub) CurrentSeq() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.seq
}

// Subscribe 注册订阅者并回放 seq > afterSeq 的历史事件。

// 返回的 channel 在 Unsubscribe 前持续接收 Publish 事件；调用方应在 ctx 结束时 Unsubscribe。
func (h *Hub) Subscribe(afterSeq int) chan Event {
	ch := make(chan Event, 64)
	h.mu.Lock()
	for _, ev := range h.history {
		if ev.Seq > afterSeq {
			ch <- ev
		}
	}
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

// Unsubscribe 移除订阅并关闭 channel；重复调用安全。
func (h *Hub) Unsubscribe(ch chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[ch]; !ok {
		return
	}
	delete(h.subs, ch)
	close(ch)
}

// FormatSSE 将事件编码为 SSE 帧（含 id / event / data 三行）。
func (e Event) FormatSSE() string {
	payload, _ := json.Marshal(e)
	return "id: " + itoa(e.Seq) + "\nevent: " + e.Type + "\ndata: " + string(payload) + "\n\n"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
