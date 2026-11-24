package stream

import (
	"encoding/json"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/logx"
)

const defaultHistorySize = 256
const defaultSubscriberBuffer = 256
const CurrentEventVersion = 1
const maxRetiredDraftAttemptsPerAgent = 256
const maxClosedDraftTurnsPerAgent = 64

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

// AssistantDraftSnapshot is the latest in-progress assistant item state at a
// Hub cursor. It lets a hydrate response cover replayable delta events that
// have not yet been committed as transcript records.
type AssistantDraftSnapshot struct {
	ItemID           string `json:"item_id"`
	AttemptID        string `json:"attempt_id"`
	TurnID           string `json:"turn_id,omitempty"`
	Part             string `json:"part"`
	Text             string `json:"text"`
	ThroughChunkSeq  uint64 `json:"through_chunk_seq"`
	ThroughAgentSeq  int    `json:"through_agent_seq"`
	ThroughStreamSeq int    `json:"through_stream_seq"`
}

// AgentSyncSnapshot captures a per-Agent replay cursor and open assistant
// drafts under the same Hub read lock.
type AgentSyncSnapshot struct {
	StreamEpoch      string                   `json:"stream_epoch"`
	CoveredAgentSeq  int                      `json:"covered_agent_seq"`
	CoveredStreamSeq int                      `json:"covered_stream_seq"`
	AssistantDrafts  []AssistantDraftSnapshot `json:"assistant_drafts"`
}

type assistantDraft struct {
	AssistantDraftSnapshot
	StepID string
	chunks []string
}

type subscriber struct {
	ch          chan Event
	agentFilter string // 空 = 接收全部（Shell 全局订阅）
}

// Hub 维护进程内全局 seq、每 Agent 的可重放 cursor、历史缓冲与订阅者 fan-out。
type Hub struct {
	mu                  sync.RWMutex
	seq                 int
	agentSeq            map[string]int
	drafts              map[string]map[string]assistantDraft
	activeAttempts      map[string]map[string]string
	retiredAttempts     map[string]map[string]struct{}
	retiredAttemptOrder map[string][]string
	closedTurns         map[string]map[string]struct{}
	closedTurnOrder     map[string][]string
	epoch               string
	history             []Event
	historyN            int
	subs                map[chan Event]*subscriber
	logger              *slog.Logger
	onPublish           func(Event)
}

// NewHub 创建事件总线；historyN 为回放上限，≤0 时用默认值；logger 可为 nil。
func NewHub(historyN int, logger *slog.Logger) *Hub {
	if historyN <= 0 {
		historyN = defaultHistorySize
	}
	return &Hub{
		historyN:            historyN,
		agentSeq:            make(map[string]int),
		drafts:              make(map[string]map[string]assistantDraft),
		activeAttempts:      make(map[string]map[string]string),
		retiredAttempts:     make(map[string]map[string]struct{}),
		retiredAttemptOrder: make(map[string][]string),
		closedTurns:         make(map[string]map[string]struct{}),
		closedTurnOrder:     make(map[string][]string),
		epoch:               itoa(int(time.Now().UnixNano())),
		subs:                make(map[chan Event]*subscriber),
		logger:              logx.OrDefault(logger),
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
	if replayable || eventType == "conversation_delta" {
		h.updateAssistantDraftsLocked(ev)
	}
	for ch, sub := range h.subs {
		if !subscriberMatches(sub, agentID) {
			continue
		}
		select {
		case sub.ch <- ev:
		default:
			// A dropped event creates a sequence gap. Disconnect the slow
			// subscriber so it can replay from its last delivered cursor or
			// hydrate if that cursor has fallen out of retention. Never retry
			// outside the lock: a later event could otherwise overtake this one.
			delete(h.subs, ch)
			close(ch)
			if h.logger != nil {
				h.logger.Warn("stream subscriber disconnected after queue overflow",
					"agent_id", ev.AgentID,
					"type", ev.Type,
					"seq", ev.Seq,
				)
			}
		}
	}
	listener := h.onPublish
	h.mu.Unlock()

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

// CaptureAgentSyncSnapshot establishes a consistent cursor/draft baseline.
// Callers should capture it before building the corresponding runtime hydrate
// projection, then replay later events with a greater Agent sequence.
func (h *Hub) CaptureAgentSyncSnapshot(agentID string) AgentSyncSnapshot {
	if h == nil {
		return AgentSyncSnapshot{AssistantDrafts: []AssistantDraftSnapshot{}}
	}
	agentID = strings.TrimSpace(agentID)
	h.mu.RLock()
	drafts := h.drafts[agentID]
	copyDrafts := make([]assistantDraft, 0, len(drafts))
	for _, draft := range drafts {
		if draft.ItemID == "" || draft.AttemptID == "" || draft.ThroughChunkSeq == 0 {
			continue
		}
		draft.chunks = append([]string(nil), draft.chunks...)
		copyDrafts = append(copyDrafts, draft)
	}
	snapshot := AgentSyncSnapshot{
		StreamEpoch: h.epoch, CoveredAgentSeq: h.agentSeq[agentID], CoveredStreamSeq: h.seq,
	}
	h.mu.RUnlock()
	out := make([]AssistantDraftSnapshot, 0, len(copyDrafts))
	for _, draft := range copyDrafts {
		var text strings.Builder
		for _, chunk := range draft.chunks {
			text.WriteString(chunk)
		}
		draft.Text = text.String()
		out = append(out, draft.AssistantDraftSnapshot)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ItemID != out[j].ItemID {
			return out[i].ItemID < out[j].ItemID
		}
		return out[i].Part < out[j].Part
	})
	snapshot.AssistantDrafts = out
	return snapshot
}

func (h *Hub) updateAssistantDraftsLocked(ev Event) {
	agentID := strings.TrimSpace(ev.AgentID)
	if agentID == "" {
		return
	}
	switch ev.Type {
	case "conversation_delta":
		data := ev.Data
		if data == nil {
			return
		}
		itemID, attemptID := strings.TrimSpace(stringValue(data["item_id"])), strings.TrimSpace(stringValue(data["attempt_id"]))
		part, text := strings.TrimSpace(stringValue(data["part"])), stringValue(data["content"])
		chunkSeq, ok := uint64Value(data["chunk_seq"])
		if itemID == "" || attemptID == "" || !ok || chunkSeq == 0 || (part != "content" && part != "reasoning") {
			return
		}
		if !validAssistantDeltaType(part, stringValue(data["display_type"])) {
			return
		}
		turnID := strings.TrimSpace(stringValue(data["turn_id"]))
		if turnID != "" && h.isClosedTurnLocked(agentID, turnID) {
			return
		}
		if h.isRetiredAttemptLocked(agentID, itemID, attemptID) {
			return
		}
		stepID := strings.TrimSpace(stringValue(data["step_id"]))
		if stepID != "" {
			if active := h.activeAttempts[agentID][stepID]; active != "" && active != attemptID {
				return
			}
		}
		if h.drafts[agentID] == nil {
			h.drafts[agentID] = make(map[string]assistantDraft)
		}
		key := itemID + "\x00" + part
		current := h.drafts[agentID][key]
		if current.AttemptID != attemptID {
			current = assistantDraft{AssistantDraftSnapshot: AssistantDraftSnapshot{
				ItemID: itemID, AttemptID: attemptID, TurnID: turnID, Part: part,
			}}
		}
		if chunkSeq == current.ThroughChunkSeq+1 {
			current.chunks = append(current.chunks, text)
			current.ThroughChunkSeq = chunkSeq
			current.ThroughAgentSeq = ev.AgentSeq
			current.ThroughStreamSeq = ev.Seq
			if current.TurnID == "" {
				current.TurnID = turnID
			}
			current.StepID = stepID
			h.drafts[agentID][key] = current
		}
	case "turn_state":
		data := ev.Data
		if data == nil {
			return
		}
		if boolValue(data["terminal"]) {
			h.closeTurnLocked(agentID, strings.TrimSpace(stringValue(data["turn_id"])))
			delete(h.drafts, agentID)
			delete(h.activeAttempts, agentID)
			return
		}
		command := strings.TrimSpace(stringValue(data["command"]))
		stepID := strings.TrimSpace(stringValue(data["step_id"]))
		attemptID := strings.TrimSpace(stringValue(data["attempt_id"]))
		if command == "model_request_started" {
			if stepID != "" && attemptID != "" {
				if h.activeAttempts[agentID] == nil {
					h.activeAttempts[agentID] = make(map[string]string)
				}
				if previous := h.activeAttempts[agentID][stepID]; previous != "" && previous != attemptID {
					for _, itemID := range assistantIDsForStep(stepID) {
						h.retireAttemptLocked(agentID, itemID, previous)
					}
				}
				h.activeAttempts[agentID][stepID] = attemptID
			}
		}
		if command == "model_request_started" || command == "assistant_received" {
			activeAttempt := h.activeAttempts[agentID][stepID]
			if command == "assistant_received" && stepID != "" {
				for _, itemID := range assistantIDsForStep(stepID) {
					h.retireAttemptLocked(agentID, itemID, activeAttempt)
				}
				delete(h.activeAttempts[agentID], stepID)
			}
			for key, draft := range h.drafts[agentID] {
				if stepID != "" && (draft.StepID == stepID || strings.HasPrefix(draft.ItemID, stepID+"/assistant/")) {
					if command == "model_request_started" && draft.AttemptID != attemptID {
						h.retireAttemptLocked(agentID, draft.ItemID, draft.AttemptID)
					}
					delete(h.drafts[agentID], key)
				}
			}
		}
	case "turn_finished":
		h.closeTurnLocked(agentID, strings.TrimSpace(stringValue(ev.Data["turn_id"])))
		delete(h.drafts, agentID)
		delete(h.activeAttempts, agentID)
	case "conversation_patch":
		data := ev.Data
		if data == nil {
			return
		}
		if turnState, ok := data["turn_state"].(map[string]any); ok && boolValue(turnState["terminal"]) {
			h.closeTurnLocked(agentID, strings.TrimSpace(stringValue(turnState["turn_id"])))
			delete(h.drafts, agentID)
			delete(h.activeAttempts, agentID)
		}
	case "runtime/notice":
		if strings.TrimSpace(stringValue(ev.Data["message_kind"])) == "context_boundary" {
			delete(h.drafts, agentID)
			delete(h.activeAttempts, agentID)
		}
	}
}

func validAssistantDeltaType(part, displayType string) bool {
	if part == "content" {
		return displayType == "delta"
	}
	return part == "reasoning" && displayType == "reasoning"
}

func assistantIDsForStep(stepID string) []string {
	if stepID == "" {
		return nil
	}
	return []string{stepID + "/assistant/content", stepID + "/assistant/reasoning"}
}

func (h *Hub) isRetiredAttemptLocked(agentID, itemID, attemptID string) bool {
	if attemptID == "" {
		return false
	}
	_, exists := h.retiredAttempts[agentID][itemID+"\x00"+attemptID]
	return exists
}

func (h *Hub) retireAttemptLocked(agentID, itemID, attemptID string) {
	if agentID == "" || itemID == "" || attemptID == "" {
		return
	}
	key := itemID + "\x00" + attemptID
	if h.retiredAttempts[agentID] == nil {
		h.retiredAttempts[agentID] = make(map[string]struct{})
	}
	if _, exists := h.retiredAttempts[agentID][key]; exists {
		return
	}
	h.retiredAttempts[agentID][key] = struct{}{}
	order := append(h.retiredAttemptOrder[agentID], key)
	if len(order) > maxRetiredDraftAttemptsPerAgent {
		delete(h.retiredAttempts[agentID], order[0])
		order = order[1:]
	}
	h.retiredAttemptOrder[agentID] = order
}

func (h *Hub) isClosedTurnLocked(agentID, turnID string) bool {
	if turnID == "" {
		return false
	}
	_, exists := h.closedTurns[agentID][turnID]
	return exists
}

func (h *Hub) closeTurnLocked(agentID, turnID string) {
	if turnID == "" {
		return
	}
	if h.closedTurns[agentID] == nil {
		h.closedTurns[agentID] = make(map[string]struct{})
	}
	if _, exists := h.closedTurns[agentID][turnID]; exists {
		return
	}
	h.closedTurns[agentID][turnID] = struct{}{}
	order := append(h.closedTurnOrder[agentID], turnID)
	if len(order) > maxClosedDraftTurnsPerAgent {
		delete(h.closedTurns[agentID], order[0])
		order = order[1:]
	}
	h.closedTurnOrder[agentID] = order
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func uint64Value(value any) (uint64, bool) {
	switch number := value.(type) {
	case uint64:
		return number, true
	case uint:
		return uint64(number), true
	case int:
		if number >= 0 {
			return uint64(number), true
		}
	case int64:
		if number >= 0 {
			return uint64(number), true
		}
	case float64:
		if number >= 0 && number == float64(uint64(number)) {
			return uint64(number), true
		}
	}
	return 0, false
}

func boolValue(value any) bool {
	flag, _ := value.(bool)
	return flag
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
	if checkRetention && filter != "" && useAgentCursor {
		// Agent cursors are process-local. After a Node restart the new Hub can
		// have a lower cursor than the browser's persisted value. Treat that as
		// an epoch discontinuity even when the new Hub has no history yet;
		// otherwise every new event is filtered by the stale high watermark.
		currentAgent := h.agentSeq[filter]
		if afterAgentSeq > currentAgent {
			resync = true
			// The cursor belongs to an earlier Node process. Clamp it so future
			// events from this process are delivered immediately after resync.
			afterAgentSeq = currentAgent
		} else if len(h.history) > 0 && currentAgent > afterAgentSeq {
			firstAgent := firstRetainedAgentSeq(h.history, filter)
			resync = firstAgent == 0 || afterAgentSeq < firstAgent-1
		}
	} else if checkRetention {
		if afterGlobalSeq > h.seq {
			// A global cursor above the current sequence indicates a process
			// restart. Reset it to this Hub's current sequence after requesting
			// a hydrate, otherwise the client would silently filter new events.
			resync = true
			afterGlobalSeq = h.seq
		} else if len(h.history) > 0 {
			first := h.history[0]
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
