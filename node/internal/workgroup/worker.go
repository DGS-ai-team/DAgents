package workgroup

import "sync"

// Worker owns the Node side of the current AgentRef session protocol.
type Worker struct {
	NodeID        string
	Capabilities  []string
	Session       Session
	AgentSessions AgentSessionHandler
	// OnTimelineEvent forwards public Manage Timeline frames to the local Web UI.
	OnTimelineEvent func(WSEnvelope)

	outboundMu sync.RWMutex
	outbound   func(map[string]any) error
}

// Config 构造 Worker。
type Config struct {
	NodeID        string
	AgentSessions AgentSessionHandler
}

// NewWorker creates the session protocol worker.
func NewWorker(cfg Config) *Worker {
	w := &Worker{
		NodeID: cfg.NodeID,
	}
	w.AgentSessions = cfg.AgentSessions
	if setter, ok := w.AgentSessions.(AgentEventEmitter); ok {
		setter.SetAgentEventEmitter(w.EmitAgentFrame)
	}
	w.Capabilities = []string{
		"resume",
		"timeline",
		"agent_session",
	}
	return w
}

// SetOutbound installs the writer for the currently established Node→Manage
// WebSocket. It is intentionally connection-scoped and may be replaced on
// reconnect; Agent handlers do not need to know about the socket.
func (w *Worker) SetOutbound(writer func(map[string]any) error) {
	if w == nil {
		return
	}
	w.outboundMu.Lock()
	w.outbound = writer
	w.outboundMu.Unlock()
	if setter, ok := w.AgentSessions.(AgentEventEmitter); ok {
		setter.SetAgentEventEmitter(w.EmitAgentFrame)
	}
}

// EmitAgentFrame sends an ephemeral agent session response/event when the
// Dialer is connected. A missing writer is reported so callers can retain
// their durable local state and rely on reconnect/resume.
func (w *Worker) EmitAgentFrame(frame map[string]any) error {
	if w == nil {
		return errf(CodeNotAuthorized, "workgroup ws is not connected")
	}
	w.outboundMu.RLock()
	writer := w.outbound
	w.outboundMu.RUnlock()
	if writer == nil {
		return errf(CodeNotAuthorized, "workgroup ws is not connected")
	}
	// The bridge may be called by a goroutine long after the request frame was
	// decoded. Stamp the current generation at the final WS boundary so Manage
	// can fence events from a replaced connection.
	copyFrame := make(map[string]any, len(frame)+1)
	for key, value := range frame {
		copyFrame[key] = value
	}
	if payload, ok := copyFrame["payload"].(map[string]any); ok {
		copyPayload := make(map[string]any, len(payload)+1)
		for key, value := range payload {
			copyPayload[key] = value
		}
		if _, exists := copyPayload["connection_generation"]; !exists || asInt64Value(copyPayload["connection_generation"]) == 0 {
			copyPayload["connection_generation"] = w.Session.Generation()
		}
		copyFrame["payload"] = copyPayload
	}
	return writer(copyFrame)
}

func asInt64Value(value any) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	default:
		return 0
	}
}

// Connect 模拟 session.hello，返回 connection_generation。
func (w *Worker) Connect() int64 {
	return w.Session.Hello(w.NodeID)
}
