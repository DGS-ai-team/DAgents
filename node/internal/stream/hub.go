package stream

import (
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/logx"
)

const defaultHistorySize = 256
const defaultSubscriberBuffer = 256

// Event 为写入 SSE 的标准事件结构。
// 线协议仅暴露 agent_id（对话/Agent 实例 id）；SessionID 仅供进程内路由（notify/filter）。
type Event struct {
	SessionID string         `json:"-"`
	AgentID   string         `json:"agent_id"`
	Type      string         `json:"type"`
	Seq       int            `json:"seq"`
	TS        string         `json:"ts"`
	Data      map[string]any `json:"data"`
}

type subscriber struct {
	ch          chan Event
	agentFilter string // 空 = 接收全部（Shell 全局订阅）
}

// Hub 维护全局递增 seq、历史缓冲与订阅者 fan-out。
type Hub struct {
	mu        sync.RWMutex
	seq       int
	history   []Event
	historyN  int
	subs      map[chan Event]*subscriber
	logger    *slog.Logger
	onPublish func(Event)
}

// NewHub 创建事件总线；historyN 为回放上限，≤0 时用默认值；logger 可为 nil。
func NewHub(historyN int, logger *slog.Logger) *Hub {
	if historyN <= 0 {
		historyN = defaultHistorySize
	}
	return &Hub{
		historyN: historyN,
		subs:     make(map[chan Event]*subscriber),
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

// Publish 分配 seq、写入历史并投递给匹配的订阅者。
// agentID 为对话/Agent 实例 id（线协议 agent_id）。
func (h *Hub) Publish(agentID, eventType string, data map[string]any) Event {
	return h.publish(agentID, eventType, data, true)
}

// PublishEphemeral publishes to current subscribers without retaining the event
// for after_seq replay. It is intended for high-frequency transient state such
// as typing/tool deltas whose durable result is represented elsewhere.
func (h *Hub) PublishEphemeral(agentID, eventType string, data map[string]any) Event {
	return h.publish(agentID, eventType, data, false)
}

func (h *Hub) publish(agentID, eventType string, data map[string]any, replayable bool) Event {
	if data == nil {
		data = map[string]any{}
	}
	h.mu.Lock()

	h.seq++
	ev := Event{
		SessionID: agentID,
		AgentID:   agentID,
		Type:      eventType,
		Seq:       h.seq,
		TS:        time.Now().UTC().Format(time.RFC3339Nano),
		Data:      data,
	}
	if replayable {
		h.history = append(h.history, ev)
		if len(h.history) > h.historyN {
			h.history = h.history[len(h.history)-h.historyN:]
		}
	}
	var deferredCritical []chan Event
	for _, sub := range h.subs {
		if !subscriberMatches(sub, agentID) {
			continue
		}
		select {
		case sub.ch <- ev:
		default:
			if isCriticalSSEType(eventType) {
				// 锁外再投一次；避免在持锁时阻塞，也避免 done/error/hitl 被静默丢掉
				deferredCritical = append(deferredCritical, sub.ch)
			}
			// 非关键事件：慢消费者丢弃，避免阻塞 turn
		}
	}
	listener := h.onPublish
	h.mu.Unlock()

	for _, ch := range deferredCritical {
		deliverCriticalSSE(ch, ev, h.logger)
	}
	h.logger.Debug("stream publish",
		"agent_id", agentID,
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

func isCriticalSSEType(eventType string) bool {
	switch eventType {
	case "done", "error", "hitl_required", "turn_state":
		return true
	default:
		return false
	}
}

func subscriberMatches(sub *subscriber, agentID string) bool {
	if sub == nil {
		return false
	}
	filter := strings.TrimSpace(sub.agentFilter)
	if filter == "" {
		return true
	}
	id := strings.TrimSpace(agentID)
	return id != "" && (filter == id)
}

func deliverCriticalSSE(ch chan Event, ev Event, logger *slog.Logger) {
	defer func() {
		// Unsubscribe 可能已 close channel
		_ = recover()
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		select {
		case ch <- ev:
			return
		default:
			if time.Now().After(deadline) {
				if logger != nil {
					logger.Warn("stream drop critical event after wait",
						"agent_id", ev.AgentID,
						"type", ev.Type,
						"seq", ev.Seq,
					)
				}
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// Subscribe 注册全局订阅者并回放 seq > afterSeq 的历史事件。
func (h *Hub) Subscribe(afterSeq int) chan Event {
	return h.SubscribeAgent(afterSeq, "")
}

// SubscribeAgent 注册订阅者；agentFilter 非空时仅接收该 Agent 的事件（含历史回放），
// 避免多 Agent 流量占满缓冲导致本 Agent 的 done/hitl 被挤掉。
func (h *Hub) SubscribeAgent(afterSeq int, agentFilter string) chan Event {
	filter := strings.TrimSpace(agentFilter)
	ch := make(chan Event, defaultSubscriberBuffer)
	sub := &subscriber{ch: ch, agentFilter: filter}
	h.mu.Lock()
	for _, ev := range h.history {
		if ev.Seq <= afterSeq {
			continue
		}
		if filter != "" && !subscriberMatches(sub, ev.AgentID) {
			continue
		}
		ch <- ev
	}
	h.subs[ch] = sub
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
