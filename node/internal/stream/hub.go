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
const CurrentEventVersion = 1

// Event 为写入 SSE 的标准事件结构。线协议使用 agent_id 标识对话/Agent
// 实例；进程内订阅过滤也使用同一个字段。
type Event struct {
	AgentID      string         `json:"agent_id"`
	Type         string         `json:"type"`
	Seq          int            `json:"seq"`
	AgentSeq     int            `json:"agent_seq,omitempty"`
	EventVersion int            `json:"event_version"`
	StreamEpoch  string         `json:"stream_epoch"`
	Delivery     string         `json:"delivery"`
	TS           string         `json:"ts"`
	Data         map[string]any `json:"data"`
}

// Subscription is the result of a cursor-aware subscription. Events is kept
// as a channel so existing in-process consumers can continue to range over it.
// ResyncRequired means the requested replay cursor is older than the retained
// history and the client must hydrate before trusting the live projection.
type Subscription struct {
	Events          chan Event
	ResyncRequired  bool
	StreamEpoch     string
	CurrentSeq      int
	CurrentAgentSeq int
}

type subscriber struct {
	ch          chan Event
	agentFilter string // 空 = 接收全部（Shell 全局订阅）
}

// Hub 维护进程内全局 seq、每 Agent 的可重放 cursor、历史缓冲与订阅者 fan-out。
type Hub struct {
	mu        sync.RWMutex
	seq       int
	agentSeq  map[string]int
	epoch     string
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
		agentSeq: make(map[string]int),
		epoch:    itoa(int(time.Now().UnixNano())),
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
	agentSeq := 0
	if replayable && strings.TrimSpace(agentID) != "" {
		h.agentSeq[agentID]++
		agentSeq = h.agentSeq[agentID]
	}
	ev := Event{
		AgentID:      agentID,
		Type:         eventType,
		Seq:          h.seq,
		AgentSeq:     agentSeq,
		EventVersion: CurrentEventVersion,
		StreamEpoch:  h.epoch,
		Delivery:     deliveryKind(replayable),
		TS:           time.Now().UTC().Format(time.RFC3339Nano),
		Data:         data,
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

// CurrentAgentSeq returns the replayable cursor for one Agent. Ephemeral
// events deliberately do not advance this cursor.
func (h *Hub) CurrentAgentSeq(agentID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.agentSeq[strings.TrimSpace(agentID)]
}

// Epoch identifies the current in-process event stream. It changes whenever
// a Hub is created, so a restarted Node cannot be confused with the old stream.
func (h *Hub) Epoch() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.epoch
}

func deliveryKind(replayable bool) string {
	if replayable {
		return "replayable"
	}
	return "ephemeral"
}

func isCriticalSSEType(eventType string) bool {
	switch eventType {
	case "turn_finished", "error", "hitl_required", "turn_state", "notification_changed", "resync_required":
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

// SubscribeAgent 注册订阅者；agentFilter 非空时仅接收该 Agent 的事件（含历史回放）。
// 它保留给进程内消费者；HTTP Agent 流应使用 SubscribeAgentCursor/Live。
func (h *Hub) SubscribeAgent(afterSeq int, agentFilter string) chan Event {
	filter := strings.TrimSpace(agentFilter)
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.subscribeLocked(afterSeq, 0, filter, true, false).Events
}

// SubscribeAgentCursor subscribes with a global cursor for unfiltered streams
// and an Agent cursor for filtered streams. The Agent cursor is the one that
// must be persisted by the Web UI because global seq also contains other
// Agents and ephemeral traffic.
func (h *Hub) SubscribeAgentCursor(afterGlobalSeq, afterAgentSeq int, agentFilter string) *Subscription {
	filter := strings.TrimSpace(agentFilter)
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.subscribeLocked(afterGlobalSeq, afterAgentSeq, filter, true, filter != "")
}

// SubscribeAgentLive subscribes at the current cursor while holding the Hub
// lock, so no event can land between taking the live snapshot and registering.
func (h *Hub) SubscribeAgentLive(agentFilter string) *Subscription {
	filter := strings.TrimSpace(agentFilter)
	h.mu.Lock()
	defer h.mu.Unlock()
	afterAgent := 0
	if filter != "" {
		afterAgent = h.agentSeq[filter]
	}
	return h.subscribeLocked(h.seq, afterAgent, filter, false, false)
}

func (h *Hub) subscribeLocked(afterGlobalSeq, afterAgentSeq int, filter string, checkRetention, useAgentCursor bool) *Subscription {
	buffer := defaultSubscriberBuffer
	if len(h.history)+1 > buffer {
		buffer = len(h.history) + 1
	}
	ch := make(chan Event, buffer)
	sub := &subscriber{ch: ch, agentFilter: filter}
	resync := false
	if checkRetention && len(h.history) > 0 {
		first := h.history[0]
		if filter == "" {
			resync = afterGlobalSeq < first.Seq-1
		} else if useAgentCursor {
			firstAgent := firstRetainedAgentSeq(h.history, filter)
			currentAgent := h.agentSeq[filter]
			if currentAgent > afterAgentSeq {
				resync = firstAgent == 0 || afterAgentSeq < firstAgent-1
			}
		} else {
			resync = afterGlobalSeq < first.Seq-1
		}
	}
	for _, ev := range h.history {
		if filter == "" || !useAgentCursor {
			if ev.Seq <= afterGlobalSeq {
				continue
			}
		} else if ev.AgentSeq <= afterAgentSeq {
			continue
		}
		if filter != "" && !subscriberMatches(sub, ev.AgentID) {
			continue
		}
		ch <- ev
	}
	h.subs[ch] = sub
	return &Subscription{
		Events:          ch,
		ResyncRequired:  resync,
		StreamEpoch:     h.epoch,
		CurrentSeq:      h.seq,
		CurrentAgentSeq: h.agentSeq[filter],
	}
}

func firstRetainedAgentSeq(history []Event, agentID string) int {
	for _, ev := range history {
		if ev.AgentID == agentID && ev.AgentSeq > 0 {
			return ev.AgentSeq
		}
	}
	return 0
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
