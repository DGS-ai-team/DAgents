package api

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/workgroup"
)

// workgroupAgentBridge is the Node-side execution adapter for the new
// Workgroup AgentRef protocol. Manage owns assignment state; Node owns the
// actual Agent session and only emits frames through the already-connected
// outbound WebSocket.
type workgroupAgentBridge struct {
	server *Server

	openMu     sync.Mutex // serializes runtime construction and binding for session IDs
	mu         sync.Mutex
	bindings   map[string]workgroupAgentBinding // session_id -> binding
	registries map[string]*tools.Registry       // session_id -> scoped tool registry
	turns      map[string]workgroupAgentTurn    // assign_id -> turn
	completed  map[string]map[string]any        // assign_id -> terminal result, for replay
	emit       func(map[string]any) error
}

type workgroupAgentBinding struct {
	workgroupID string
	memberID    string
	agentID     string
	sessionID   string
}

type workgroupAgentTurn struct {
	binding     workgroupAgentBinding
	childTurnID string
	attemptID   string
	cancel      func()
}

type agentSessionRuntimeOwnership struct {
	built       agentruntime.Built
	transferred bool
}

func (o *agentSessionRuntimeOwnership) closeIfUntransferred() {
	if o != nil && !o.transferred {
		_ = o.built.Close()
	}
}

func newWorkgroupAgentBridge(server *Server) *workgroupAgentBridge {
	return &workgroupAgentBridge{
		server:     server,
		bindings:   make(map[string]workgroupAgentBinding),
		registries: make(map[string]*tools.Registry),
		turns:      make(map[string]workgroupAgentTurn),
		completed:  make(map[string]map[string]any),
	}
}

// SetAgentEventEmitter is called by Workgroup Worker when a Dialer connection
// is replaced. Existing sessions stay local; only their outbound event sink
// changes, which is what makes reconnect safe without Manage→Node requests.
func (b *workgroupAgentBridge) SetAgentEventEmitter(emit func(map[string]any) error) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.emit = emit
	b.mu.Unlock()
}

func (b *workgroupAgentBridge) emitFrame(frame map[string]any) error {
	b.mu.Lock()
	emit := b.emit
	b.mu.Unlock()
	if emit == nil {
		return fmt.Errorf("workgroup ws is not connected")
	}
	return emit(frame)
}

func (b *workgroupAgentBridge) OpenAgentSession(ctx context.Context, req workgroup.AgentSessionOpenRequest) (workgroup.AgentSessionResult, error) {
	if b == nil || b.server == nil || b.server.sessions == nil {
		return workgroup.AgentSessionResult{}, fmt.Errorf("node session manager unavailable")
	}
	req.WorkgroupID = strings.TrimSpace(req.WorkgroupID)
	req.MemberID = strings.TrimSpace(req.MemberID)
	req.AgentID = strings.TrimSpace(req.AgentID)
	req.SessionID = strings.TrimSpace(req.SessionID)
	if req.WorkgroupID == "" || req.MemberID == "" || req.AgentID == "" || req.SessionID == "" {
		return workgroup.AgentSessionResult{}, fmt.Errorf("workgroup_id, member_id, agent_id and session_id are required")
	}
	b.openMu.Lock()
	defer b.openMu.Unlock()
	binding := workgroupAgentBinding{
		workgroupID: req.WorkgroupID,
		memberID:    req.MemberID,
		agentID:     req.AgentID,
		sessionID:   req.SessionID,
	}
	b.mu.Lock()
	existing, bound := b.bindings[req.SessionID]
	registry := b.registries[req.SessionID]
	b.mu.Unlock()
	if bound {
		if existing != binding {
			return workgroup.AgentSessionResult{}, fmt.Errorf("agent session is already bound to a different workgroup member")
		}
		if registry == nil {
			return workgroup.AgentSessionResult{}, fmt.Errorf("agent session binding has no tool registry")
		}
		if _, _, _, runtimeErr := b.server.sessions.RuntimeInfo(req.SessionID); runtimeErr == nil {
			return readyAgentSessionResult(binding), nil
		}
		// Idle-session release removes the manager runtime but leaves this
		// adapter's binding cache. Drop that stale pair so the next open can
		// restore history into a fresh runtime.
		b.mu.Lock()
		if b.bindings[req.SessionID] == existing && b.registries[req.SessionID] == registry {
			delete(b.bindings, req.SessionID)
			delete(b.registries, req.SessionID)
		}
		b.mu.Unlock()
	} else if registry != nil {
		return workgroup.AgentSessionResult{}, fmt.Errorf("agent session registry exists without a binding")
	}
	if b.server.agents == nil {
		return workgroup.AgentSessionResult{}, fmt.Errorf("agent store unavailable")
	}
	rec, err := b.server.agents.Get(ctx, req.AgentID)
	if err != nil {
		return workgroup.AgentSessionResult{}, err
	}
	if rec == nil || rec.Archived {
		return workgroup.AgentSessionResult{}, fmt.Errorf("agent %q not found or archived", req.AgentID)
	}

	// A workgroup gets a separate durable session namespace. The Agent's
	// configured prompt/tools remain the source of truth, but history, queue,
	// compression and turn lifecycle are isolated by session_id.
	runtime, err := b.server.buildAgentSessionRuntime(ctx, rec, agentSessionRuntimeOptions{
		// Workgroup sessions use the Agent's tools and policy but never its
		// personal memory store.
		DisableMemory:       true,
		RequireStoreContext: true,
	})
	if err != nil {
		return workgroup.AgentSessionResult{}, err
	}
	ownership := agentSessionRuntimeOwnership{built: runtime.Built}
	defer ownership.closeIfUntransferred()
	policyEngine := runtime.Policy
	if policyEngine == nil {
		policyEngine = policy.NewEngineFromMaps(policy.LoadSeedMaps())
	}
	_, _, reused, err := b.server.sessions.CreateWithOptionsAndLLMReuse(
		req.SessionID, runtime.Built.TurnOptions, runtime.Built.Registry, policyEngine, runtime.LLM, req.AgentID,
	)
	if err != nil {
		return workgroup.AgentSessionResult{}, err
	}
	if reused {
		// The manager retained its original executor. Without a bridge binding,
		// this call cannot safely claim ownership or replace it.
		return workgroup.AgentSessionResult{}, fmt.Errorf("agent session runtime already exists without a matching workgroup binding")
	}
	ownership.transferred = true
	registry = runtime.Built.Registry

	b.mu.Lock()
	b.registries[req.SessionID] = registry
	b.bindings[req.SessionID] = binding
	b.mu.Unlock()
	return readyAgentSessionResult(binding), nil
}

func readyAgentSessionResult(binding workgroupAgentBinding) workgroup.AgentSessionResult {
	return workgroup.AgentSessionResult{
		WorkgroupID: binding.workgroupID,
		MemberID:    binding.memberID,
		AgentID:     binding.agentID,
		SessionID:   binding.sessionID,
		Status:      "ready",
	}
}

// registryForSession lets Node-owned brokers open a resource on behalf of a
// workgroup session without falling back to the personal Agent runtime. The
// workgroup session owns the tool snapshot and policy; the terminal registry
// still owns the long-lived PTY itself.
func (b *workgroupAgentBridge) registryForSession(sessionID string) *tools.Registry {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	registry := b.registries[strings.TrimSpace(sessionID)]
	b.mu.Unlock()
	return registry
}

func (b *workgroupAgentBridge) StartAgentTurn(_ context.Context, req workgroup.AgentTurnStartRequest) error {
	if b == nil || b.server == nil || b.server.sessions == nil || b.server.stream == nil {
		return fmt.Errorf("node turn runtime unavailable")
	}
	b.mu.Lock()
	binding, ok := b.bindings[req.SessionID]
	if !ok || binding.agentID != req.AgentID || binding.memberID != req.MemberID || binding.workgroupID != req.WorkgroupID {
		b.mu.Unlock()
		return fmt.Errorf("agent session is not bound")
	}
	if active, running := b.turns[req.AssignID]; running {
		// Manage may replay an unacknowledged start after reconnect.  The
		// original turn owns this assign, so accepting the duplicate is safer
		// than starting a second model turn or reporting a false failure.
		if active.binding == binding {
			b.mu.Unlock()
			return nil
		}
		b.mu.Unlock()
		return fmt.Errorf("agent assign is already running")
	}
	if result, finished := b.completed[req.AssignID]; finished {
		// A result may have been produced while Manage was disconnected.  Replay
		// it when the reliable start frame is delivered again.
		result = cloneMap(result)
		b.mu.Unlock()
		_ = b.emitFrame(map[string]any{
			"type":    "agent.turn.result",
			"payload": result,
		})
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, active, _, err := b.server.sessions.RuntimeInfo(req.SessionID)
	if err != nil {
		cancel()
		b.mu.Unlock()
		return fmt.Errorf("inspect agent turn state: %w", err)
	}
	b.turns[req.AssignID] = workgroupAgentTurn{
		binding:     binding,
		childTurnID: req.ChildTurnID,
		attemptID:   req.AttemptID,
		cancel:      cancel,
	}
	b.mu.Unlock()

	// Register the filtered subscription before enqueueing the turn. The live
	// cursor is captured under the Hub lock, so the first turn_state cannot be
	// lost in the enqueue/subscribe gap.
	ch := b.server.stream.SubscribeAgentLive(req.SessionID).Events
	go b.runAgentTurn(ctx, req, ch, !active)
	return nil
}

func (b *workgroupAgentBridge) runAgentTurn(
	ctx context.Context,
	req workgroup.AgentTurnStartRequest,
	ch chan stream.Event,
	enqueue bool,
) {
	defer b.server.stream.Unsubscribe(ch)
	defer func() {
		b.mu.Lock()
		delete(b.turns, req.AssignID)
		b.mu.Unlock()
	}()
	if enqueue {
		_, err := b.server.sessions.EnqueueMessage(
			ctx, req.SessionID, "message", req.UserMessage, nil, nil, "human",
		)
		if err != nil {
			b.publishTurnResult(req, b.turnResultPayload(req, "failed", "", err.Error(), "enqueue_failed"))
			return
		}
	}

	var assistantText strings.Builder
	for {
		select {
		case <-ctx.Done():
			b.publishTurnResult(req, b.turnResultPayload(req, "canceled", "", "agent turn canceled", "canceled"))
			return
		case ev, open := <-ch:
			if !open {
				return
			}
			data := cloneMap(ev.Data)
			isChildEvent := strings.TrimSpace(stringValue(data["child_agent_id"])) != ""
			if ev.Type == "assistant" && !isChildEvent {
				assistantText.WriteString(stringValue(data["content"]))
			}
			_ = b.emitFrame(map[string]any{
				"type": "agent.turn.event",
				"payload": map[string]any{
					"workgroup_id":          req.WorkgroupID,
					"member_id":             req.MemberID,
					"agent_id":              req.AgentID,
					"session_id":            req.SessionID,
					"assign_id":             req.AssignID,
					"source":                req.Source,
					"parent_turn_id":        req.ParentTurnID,
					"child_turn_id":         req.ChildTurnID,
					"attempt_id":            req.AttemptID,
					"event_type":            ev.Type,
					"event_seq":             ev.Seq,
					"event_id":              agentEventID(ev),
					"stream_epoch":          ev.StreamEpoch,
					"data":                  data,
					"connection_generation": 0,
				},
			})
			if ev.Type == "turn_finished" {
				// A workgroup assignment owns the whole Agent turn. HITL pauses do
				// not emit turn_finished, so this event is unambiguously terminal.
				text := strings.TrimSpace(assistantText.String())
				if text == "" {
					// The stream event and durable history are committed by adjacent
					// lifecycle operations.  A short retry closes that ordering gap
					// without making the final result depend on a single snapshot read.
					text = b.lastAssistantTextWithRetry(req.SessionID)
				}
				b.publishTurnResult(req, b.turnResultPayload(req, "succeeded", text, "", stringValue(data["finish_reason"])))
				return
			}
			if ev.Type == "error" {
				message := stringValue(data["message"])
				b.publishTurnResult(req, b.turnResultPayload(req, "failed", "", message, "runtime_error"))
				return
			}
		}
	}
}

func (b *workgroupAgentBridge) publishTurnResult(req workgroup.AgentTurnStartRequest, payload map[string]any) {
	b.mu.Lock()
	if len(b.completed) >= 512 {
		// The result is only a reconnect replay cache.  The durable transcript
		// remains in the Node session, so evicting an old entry is safe.
		for assignID := range b.completed {
			delete(b.completed, assignID)
			break
		}
	}
	b.completed[req.AssignID] = cloneMap(payload)
	b.mu.Unlock()
	_ = b.emitFrame(map[string]any{
		"type":    "agent.turn.result",
		"payload": cloneMap(payload),
	})
}

func (b *workgroupAgentBridge) CancelAgentTurn(_ context.Context, req workgroup.AgentTurnCancelRequest) error {
	b.mu.Lock()
	turnState, ok := b.turns[req.AssignID]
	b.mu.Unlock()
	if !ok {
		return nil
	}
	if turnState.binding.sessionID != req.SessionID || turnState.childTurnID != req.ChildTurnID || turnState.attemptID != req.AttemptID {
		return fmt.Errorf("assign session mismatch")
	}
	turnState.cancel()
	b.server.sessions.CancelTurn(req.SessionID)
	return nil
}

func (b *workgroupAgentBridge) CancelAgentTool(_ context.Context, req workgroup.AgentToolCancelRequest) error {
	if b == nil || b.server == nil || b.server.sessions == nil {
		return fmt.Errorf("node turn runtime unavailable")
	}
	b.mu.Lock()
	binding, ok := b.bindings[req.SessionID]
	b.mu.Unlock()
	if !ok || binding.agentID != req.AgentID || binding.memberID != req.MemberID || binding.workgroupID != req.WorkgroupID {
		return fmt.Errorf("agent session is not bound")
	}
	if strings.TrimSpace(req.ToolName) != "bash_run" {
		return fmt.Errorf("tool %q does not support independent cancellation", req.ToolName)
	}
	registry := b.server.sessions.SessionTools(req.SessionID)
	if registry == nil {
		return fmt.Errorf("agent session tools unavailable")
	}
	if err := registry.CancelSyncBash(req.SessionID, req.ToolCallID); err != nil {
		return err
	}
	return nil
}

func (b *workgroupAgentBridge) ResumeAgentTurn(_ context.Context, req workgroup.AgentTurnResumeRequest) error {
	if b == nil || b.server == nil || b.server.sessions == nil {
		return fmt.Errorf("node turn runtime unavailable")
	}
	b.mu.Lock()
	binding, bound := b.bindings[req.SessionID]
	_, running := b.turns[req.AssignID]
	b.mu.Unlock()
	if !bound || binding.agentID != req.AgentID || binding.memberID != req.MemberID ||
		binding.workgroupID != req.WorkgroupID {
		return fmt.Errorf("agent session is not bound")
	}
	if !running {
		return fmt.Errorf("agent assign is not running")
	}
	if turnState, ok := b.turns[req.AssignID]; !ok || turnState.childTurnID != req.ChildTurnID || turnState.attemptID != req.AttemptID {
		return fmt.Errorf("agent turn identity mismatch")
	}
	if _, err := b.server.sessions.EnqueueMessage(
		context.Background(), req.SessionID, "resume", "", nil, req.ResumeValue, "human",
	); err != nil {
		// The Manage hub deliberately does both live delivery and reconnect
		// gap-fill. A resume can therefore be delivered twice around the ACK;
		// the first copy consumes the pending HITL and the second copy observes
		// no_pending_hitl. Once the assignment is still running, that duplicate
		// is already satisfied and must not turn a successful recovery into a
		// failed assignment.
		if err.Error() == "no_pending_hitl" {
			return nil
		}
		return err
	}
	return nil
}

func (b *workgroupAgentBridge) CloseAgentSession(_ context.Context, req workgroup.AgentSessionOpenRequest) error {
	if b == nil || b.server == nil || b.server.sessions == nil {
		return fmt.Errorf("node session manager unavailable")
	}
	b.openMu.Lock()
	defer b.openMu.Unlock()
	b.mu.Lock()
	binding, bound := b.bindings[req.SessionID]
	if !bound {
		b.mu.Unlock()
		return nil
	}
	if binding.workgroupID != strings.TrimSpace(req.WorkgroupID) || binding.memberID != strings.TrimSpace(req.MemberID) || binding.agentID != strings.TrimSpace(req.AgentID) {
		b.mu.Unlock()
		return fmt.Errorf("agent session is not bound to this workgroup member")
	}
	for _, active := range b.turns {
		if active.binding.sessionID == req.SessionID {
			b.mu.Unlock()
			return fmt.Errorf("cannot close busy agent session")
		}
	}
	queuePending, active, _, runtimeErr := b.server.sessions.RuntimeInfo(req.SessionID)
	if runtimeErr == nil && (active || queuePending > 0) {
		b.mu.Unlock()
		return fmt.Errorf("cannot close busy agent session")
	}
	if runtimeErr == nil {
		if _, err := b.server.sessions.Release(req.SessionID); err != nil {
			b.mu.Unlock()
			return fmt.Errorf("release agent session: %w", err)
		}
	}
	delete(b.bindings, req.SessionID)
	delete(b.registries, req.SessionID)
	b.mu.Unlock()
	// Release persists session history and drops the isolated runtime, allowing
	// a later open to restore that history with a fresh registry.
	return nil
}

func (b *workgroupAgentBridge) lastAssistantText(sessionID string) string {
	_, messages, err := b.server.sessions.ContextSummary(sessionID)
	if err != nil {
		return ""
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" && strings.TrimSpace(messages[i].Content) != "" {
			return messages[i].Content
		}
	}
	return ""
}

func (b *workgroupAgentBridge) lastAssistantTextWithRetry(sessionID string) string {
	for attempt := 0; attempt < 10; attempt++ {
		if text := b.lastAssistantText(sessionID); strings.TrimSpace(text) != "" {
			return text
		}
		time.Sleep(10 * time.Millisecond)
	}
	return ""
}

func (b *workgroupAgentBridge) turnResultPayload(
	req workgroup.AgentTurnStartRequest,
	status, text, message, finishReason string,
) map[string]any {
	return map[string]any{
		"workgroup_id":   req.WorkgroupID,
		"member_id":      req.MemberID,
		"agent_id":       req.AgentID,
		"session_id":     req.SessionID,
		"assign_id":      req.AssignID,
		"source":         req.Source,
		"parent_turn_id": req.ParentTurnID,
		"child_turn_id":  req.ChildTurnID,
		"attempt_id":     req.AttemptID,
		"status":         status,
		"final_text":     text,
		"message":        message,
		"finish_reason":  finishReason,
	}
}

func agentEventID(ev stream.Event) string {
	return fmt.Sprintf("node:%s:%d", strings.TrimSpace(ev.StreamEpoch), ev.Seq)
}

func cloneMap(src map[string]any) map[string]any {
	if src == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func stringValue(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

var _ workgroup.AgentSessionHandler = (*workgroupAgentBridge)(nil)
var _ workgroup.AgentEventEmitter = (*workgroupAgentBridge)(nil)
