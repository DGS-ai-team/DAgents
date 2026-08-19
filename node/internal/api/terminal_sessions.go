package api

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const (
	terminalResumeGrace    = 30 * time.Second
	terminalReplayBytes    = 1 << 20
	terminalInterruptGrace = 1500 * time.Millisecond
	terminalForceTimeout   = 2 * time.Second
	terminalForceWait      = 500 * time.Millisecond
	terminalIdleTimeout    = 10 * time.Minute
)

type terminalOutputFrame struct {
	seq  uint64
	data []byte
}

// terminalSession owns the PTY independently of any WebSocket connection.
// This separation is what makes a reconnect safe: output is still drained
// while detached and a bounded replay buffer is retained for the next client.
type terminalSession struct {
	mu           sync.Mutex
	writeMu      sync.Mutex
	id           string
	agentID      string
	terminal     tools.Terminal
	output       io.ReadCloser
	conn         *websocket.Conn
	frames       []terminalOutputFrame
	frameBytes   int
	nextSeq      uint64
	toolSeq      uint64
	dropped      bool
	exited       *terminalWSEvent
	closed       bool
	expiry       *time.Timer
	idleTimer    *time.Timer
	lastActivity time.Time
	registry     *terminalSessionRegistry
	outputDone   chan struct{}
	targetKind   string
	targetID     string
	shell        string
	cwd          string
	createdAt    time.Time
	keepAlive    bool
	configID     string
}

func newTerminalSession(agentID string, terminal tools.Terminal, registry *terminalSessionRegistry, req tools.TerminalRequest, keepAlive bool) (*terminalSession, error) {
	if terminal == nil {
		return nil, fmt.Errorf("terminal is unavailable")
	}
	output, err := terminal.Output()
	if err != nil {
		return nil, err
	}
	s := &terminalSession{
		id:         terminal.ID(),
		agentID:    agentID,
		terminal:   terminal,
		output:     output,
		registry:   registry,
		outputDone: make(chan struct{}),
		targetKind: strings.TrimSpace(req.Target.Kind),
		targetID:   strings.TrimSpace(req.Target.ID),
		shell:      strings.TrimSpace(req.Shell),
		cwd:        strings.TrimSpace(req.CWD),
		createdAt:  time.Now().UTC(),
		keepAlive:  keepAlive,
		configID:   strings.TrimSpace(req.ConfigID),
	}
	if s.targetKind == "" {
		s.targetKind = "local"
	}
	if err := terminal.Start(); err != nil {
		_ = output.Close()
		_ = terminal.Close()
		return nil, err
	}
	go s.readOutput()
	go s.waitForExit()
	s.touchActivity()
	return s, nil
}

func (s *terminalSession) readOutput() {
	defer close(s.outputDone)
	buffer := make([]byte, 32*1024)
	for {
		n, err := s.output.Read(buffer)
		if n > 0 {
			s.appendOutput(buffer[:n])
		}
		if err != nil {
			return
		}
	}
}

func (s *terminalSession) appendOutput(data []byte) {
	if len(data) == 0 {
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	s.nextSeq++
	frame := terminalOutputFrame{seq: s.nextSeq, data: append([]byte(nil), data...)}
	s.frames = append(s.frames, frame)
	s.frameBytes += len(frame.data)
	for s.frameBytes > terminalReplayBytes && len(s.frames) > 0 {
		s.frameBytes -= len(s.frames[0].data)
		s.frames = s.frames[1:]
		s.dropped = true
	}
	conn := s.conn
	s.mu.Unlock()
	s.touchActivity()
	if conn != nil {
		_ = writeTerminalWSEventWithTimeout(conn, terminalWSEvent{
			Type: "output", SessionID: s.id, TerminalID: s.id, Seq: frame.seq, Data: frame.data,
		})
	}
}

func (s *terminalSession) waitForExit() {
	err := s.terminal.Wait()
	// Terminal.Wait may finish while bytes are still buffered in the PTY
	// master. Close releases the reader; only then is exited published.
	_ = s.terminal.Close()
	<-s.outputDone
	exit := s.terminal.ExitStatus()
	event := &terminalWSEvent{Type: "exited", SessionID: s.id, TerminalID: s.id}
	if exit != nil {
		event.Exit = &terminalWSExit{Code: exit.Code, Error: exit.Error}
	} else if err != nil {
		event.Exit = &terminalWSExit{Code: -1, Error: err.Error()}
	}
	s.writeMu.Lock()
	s.mu.Lock()
	s.exited = event
	conn := s.conn
	shouldExpire := conn == nil
	s.mu.Unlock()
	if conn != nil {
		_ = writeTerminalWSEventWithTimeout(conn, *event)
	}
	s.writeMu.Unlock()
	if shouldExpire {
		s.scheduleExpiry()
	}
	if s.registry != nil {
		s.registry.publishChange(s, "terminal.updated")
	}
}

func (s *terminalSession) attach(ctx context.Context, conn *websocket.Conn) error {
	if s == nil || conn == nil {
		return fmt.Errorf("terminal session is unavailable")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("terminal session is closed")
	}
	if s.conn != nil && s.conn != conn {
		s.mu.Unlock()
		return fmt.Errorf("terminal session is already connected")
	}
	s.conn = conn
	if s.expiry != nil {
		s.expiry.Stop()
		s.expiry = nil
	}
	frames := append([]terminalOutputFrame(nil), s.frames...)
	dropped := s.dropped
	exited := s.exited
	s.mu.Unlock()

	if err := writeTerminalWSEventLocked(ctx, conn, terminalWSEvent{Type: "started", SessionID: s.id, TerminalID: s.id}); err != nil {
		return err
	}
	if dropped {
		if err := writeTerminalWSEventLocked(ctx, conn, terminalWSEvent{Type: "replay_gap", SessionID: s.id, TerminalID: s.id, Error: "terminal output exceeded replay buffer"}); err != nil {
			return err
		}
	}
	for _, frame := range frames {
		if err := writeTerminalWSEventLocked(ctx, conn, terminalWSEvent{Type: "output", SessionID: s.id, TerminalID: s.id, Seq: frame.seq, Replay: true, Data: frame.data}); err != nil {
			return err
		}
	}
	if exited != nil {
		if err := writeTerminalWSEventLocked(ctx, conn, *exited); err != nil {
			return err
		}
	}
	return nil
}

func (s *terminalSession) detach(conn *websocket.Conn) {
	if s == nil || conn == nil {
		return
	}
	s.writeMu.Lock()
	s.mu.Lock()
	if s.conn == conn {
		s.conn = nil
	}
	shouldExpire := s.conn == nil && !s.closed && !s.keepAlive
	s.mu.Unlock()
	s.writeMu.Unlock()
	if shouldExpire {
		s.scheduleExpiry()
	}
}

func (s *terminalSession) send(ctx context.Context, event terminalWSEvent) error {
	if s == nil {
		return fmt.Errorf("terminal session is unavailable")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return nil
	}
	return writeTerminalWSEventLocked(ctx, conn, event)
}

func (s *terminalSession) input(ctx context.Context, data []byte) error {
	err := s.terminal.Input(ctx, data)
	if err == nil && len(data) > 0 {
		s.touchActivity()
	}
	return err
}

func (s *terminalSession) resize(ctx context.Context, rows, cols int) error {
	return s.terminal.Resize(ctx, rows, cols)
}

func (s *terminalSession) terminate(ctx context.Context) (tools.TerminalOutput, error) {
	if s == nil || s.terminal == nil {
		return tools.TerminalOutput{}, fmt.Errorf("terminal session is unavailable")
	}

	// A cancelled turn must not prevent terminal cleanup. The terminate tool
	// is itself the cleanup path, so its control writes use bounded independent
	// contexts instead of inheriting a possibly-cancelled model context.
	if s.hasExited() {
		out := s.snapshotOutput(0, terminalReplayBytes, true)
		out.Graceful = true
		return out, nil
	}

	interruptCtx, interruptCancel := context.WithTimeout(context.Background(), terminalForceTimeout)
	interruptErr := s.input(interruptCtx, []byte{0x03})
	interruptCancel()

	graceful := interruptErr == nil && s.waitForExitWithin(terminalInterruptGrace)
	forced := false
	if !graceful {
		forced = true
		forceCtx, forceCancel := context.WithTimeout(context.Background(), terminalForceTimeout)
		forceErr := s.terminal.Terminate(forceCtx)
		forceCancel()
		if forceErr != nil {
			// Give a concurrently completing terminal a chance to publish its
			// final output before returning the provider error.
			s.waitForExitWithin(terminalForceWait)
			out := s.snapshotOutput(0, terminalReplayBytes, true)
			out.Graceful = false
			out.Forced = true
			return out, forceErr
		}
		s.waitForExitWithin(terminalForceWait)
	}

	out := s.snapshotOutput(0, terminalReplayBytes, true)
	out.Graceful = graceful
	out.Forced = forced
	return out, nil
}

func (s *terminalSession) hasExited() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	exited := s.exited != nil
	s.mu.Unlock()
	return exited
}

func (s *terminalSession) waitForExitWithin(timeout time.Duration) bool {
	if s == nil {
		return true
	}
	if s.hasExited() {
		return true
	}
	timer := time.NewTimer(timeout)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer timer.Stop()
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if s.hasExited() {
				return true
			}
		case <-timer.C:
			return s.hasExited()
		}
	}
}

func (s *terminalSession) scheduleExpiry() {
	if s == nil || s.registry == nil {
		return
	}
	s.mu.Lock()
	if s.closed || s.conn != nil || s.expiry != nil || s.keepAlive {
		s.mu.Unlock()
		return
	}
	s.expiry = time.AfterFunc(terminalResumeGrace, func() {
		s.registry.expire(s.id, s)
	})
	s.mu.Unlock()
}

func (s *terminalSession) touchActivity() {
	if s == nil || s.registry == nil || s.registry.idleTimeout <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.lastActivity = time.Now()
	if s.idleTimer == nil {
		s.idleTimer = time.AfterFunc(s.registry.idleTimeout, func() {
			s.expireIfIdle()
		})
		return
	}
	s.idleTimer.Reset(s.registry.idleTimeout)
}

func (s *terminalSession) expireIfIdle() {
	if s == nil || s.registry == nil || s.registry.idleTimeout <= 0 {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	idleFor := time.Since(s.lastActivity)
	if idleFor < s.registry.idleTimeout {
		s.idleTimer.Reset(s.registry.idleTimeout - idleFor)
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	// remove() performs the provider close and publishes terminal.closed to
	// the Agent/UI subscribers. This applies equally to tool-owned and UI-owned
	// sessions; keepAlive only controls reconnect retention.
	s.registry.remove(s.id, s)
}

func (s *terminalSession) shutdown() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	if s.expiry != nil {
		s.expiry.Stop()
		s.expiry = nil
	}
	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}
	conn := s.conn
	s.conn = nil
	s.mu.Unlock()
	_ = s.terminal.Close()
	_ = s.output.Close()
	if conn != nil {
		_ = conn.CloseNow()
	}
}

type terminalSessionRegistry struct {
	mu                  sync.Mutex
	closed              bool
	sessions            map[string]*terminalSession
	maxSessionsPerAgent int
	idleTimeout         time.Duration
	opener              func(context.Context, string, tools.TerminalRequest) (tools.Terminal, error)
	onChange            func(string, string, map[string]any)
}

func newTerminalSessionRegistry(maxSessions ...int) *terminalSessionRegistry {
	limit := tools.DefaultTerminalSessionLimit
	if len(maxSessions) > 0 && maxSessions[0] > 0 {
		limit = maxSessions[0]
	}
	return newTerminalSessionRegistryWithOptions(limit, terminalIdleTimeout)
}

func newTerminalSessionRegistryWithOptions(maxSessions int, idleTimeout time.Duration) *terminalSessionRegistry {
	if maxSessions <= 0 {
		maxSessions = tools.DefaultTerminalSessionLimit
	}
	return &terminalSessionRegistry{
		sessions:            make(map[string]*terminalSession),
		maxSessionsPerAgent: maxSessions,
		idleTimeout:         idleTimeout,
	}
}

func (r *terminalSessionRegistry) setOpener(opener func(context.Context, string, tools.TerminalRequest) (tools.Terminal, error)) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.opener = opener
	r.mu.Unlock()
}

func (r *terminalSessionRegistry) setChangePublisher(publisher func(string, string, map[string]any)) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.onChange = publisher
	r.mu.Unlock()
}

func (r *terminalSessionRegistry) Open(ctx context.Context, agentID string, req tools.TerminalRequest) (tools.TerminalSessionInfo, error) {
	if r == nil {
		return tools.TerminalSessionInfo{}, fmt.Errorf("terminal registry is unavailable")
	}
	r.mu.Lock()
	opener := r.opener
	r.mu.Unlock()
	if opener == nil {
		return tools.TerminalSessionInfo{}, fmt.Errorf("terminal registry opener is unavailable")
	}
	terminal, err := opener(ctx, agentID, req)
	if err != nil {
		return tools.TerminalSessionInfo{}, err
	}
	session, err := newTerminalSession(agentID, terminal, r, req, true)
	if err != nil {
		return tools.TerminalSessionInfo{}, err
	}
	if err := r.add(session); err != nil {
		session.shutdown()
		return tools.TerminalSessionInfo{}, err
	}
	r.publishChange(session, "terminal.opened")
	return session.info(), nil
}

func (r *terminalSessionRegistry) add(session *terminalSession) error {
	if r == nil || session == nil {
		return fmt.Errorf("terminal registry is unavailable")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return fmt.Errorf("terminal registry is closed")
	}
	count := 0
	for _, item := range r.sessions {
		if item != nil && item.agentID == session.agentID {
			count++
		}
	}
	if r.maxSessionsPerAgent > 0 && count >= r.maxSessionsPerAgent {
		return fmt.Errorf("agent %q has reached terminal session limit (%d); terminate an unused terminal first", session.agentID, r.maxSessionsPerAgent)
	}
	r.sessions[session.id] = session
	return nil
}

func (r *terminalSessionRegistry) get(id, agentID string) (*terminalSession, error) {
	if r == nil {
		return nil, fmt.Errorf("terminal registry is unavailable")
	}
	r.mu.Lock()
	session := r.sessions[id]
	r.mu.Unlock()
	if session == nil {
		return nil, fmt.Errorf("terminal session not found")
	}
	session.mu.Lock()
	closed := session.closed
	sameAgent := session.agentID == agentID
	session.mu.Unlock()
	if closed {
		return nil, fmt.Errorf("terminal session is closed")
	}
	if !sameAgent {
		return nil, fmt.Errorf("terminal session does not belong to agent")
	}
	return session, nil
}

func (r *terminalSessionRegistry) remove(id string, expected *terminalSession) {
	if r == nil {
		return
	}
	r.mu.Lock()
	session := r.sessions[id]
	if session != nil && (expected == nil || session == expected) {
		delete(r.sessions, id)
	} else {
		session = nil
	}
	r.mu.Unlock()
	if session != nil {
		session.shutdown()
		r.publishChange(session, "terminal.closed")
	}
}

func (r *terminalSessionRegistry) expire(id string, expected *terminalSession) {
	r.remove(id, expected)
}

func (r *terminalSessionRegistry) closeAll() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	sessions := make([]*terminalSession, 0, len(r.sessions))
	for _, session := range r.sessions {
		sessions = append(sessions, session)
	}
	r.sessions = make(map[string]*terminalSession)
	r.mu.Unlock()
	for _, session := range sessions {
		session.shutdown()
	}
}

func (s *terminalSession) info() tools.TerminalSessionInfo {
	if s == nil {
		return tools.TerminalSessionInfo{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	status := "running"
	if s.closed {
		status = "closed"
	} else if s.exited != nil {
		status = "exited"
	}
	return tools.TerminalSessionInfo{
		ID:         s.id,
		AgentID:    s.agentID,
		ConfigID:   s.configID,
		TargetKind: s.targetKind,
		TargetID:   s.targetID,
		Shell:      s.shell,
		CWD:        s.cwd,
		Status:     status,
		CreatedAt:  s.createdAt,
	}
}

func (r *terminalSessionRegistry) publishChange(session *terminalSession, eventType string) {
	if r == nil || session == nil {
		return
	}
	r.mu.Lock()
	publisher := r.onChange
	count := 0
	for _, item := range r.sessions {
		if item == nil || item.agentID != session.agentID {
			continue
		}
		count++
	}
	r.mu.Unlock()
	if publisher == nil {
		return
	}
	info := session.info()
	publisher(session.agentID, eventType, map[string]any{
		"terminal":    info,
		"terminal_id": info.ID,
		"count":       count,
	})
}

func (r *terminalSessionRegistry) List(agentID string) []tools.TerminalSessionInfo {
	if r == nil {
		return nil
	}
	id := strings.TrimSpace(agentID)
	r.mu.Lock()
	items := make([]tools.TerminalSessionInfo, 0)
	for _, session := range r.sessions {
		if session == nil || session.agentID != id {
			continue
		}
		items = append(items, session.info())
	}
	r.mu.Unlock()
	sort.SliceStable(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	return items
}

func (r *terminalSessionRegistry) ReadOutput(ctx context.Context, agentID, terminalID string, afterSeq uint64, maxBytes int) (tools.TerminalOutput, error) {
	session, err := r.get(strings.TrimSpace(terminalID), strings.TrimSpace(agentID))
	if err != nil {
		return tools.TerminalOutput{}, err
	}
	if maxBytes <= 0 {
		maxBytes = 12000
	}
	return session.snapshotOutput(afterSeq, maxBytes, true), nil
}

// snapshotOutput snapshots the bounded replay buffer. When advanceCursor is true,
// the cursor is the Agent-side unread boundary; WebSocket replay must call this
// with false so viewing a terminal does not consume output from the model.
func (s *terminalSession) snapshotOutput(afterSeq uint64, maxBytes int, advanceCursor bool) tools.TerminalOutput {
	if s == nil {
		return tools.TerminalOutput{}
	}
	if maxBytes <= 0 {
		maxBytes = terminalReplayBytes
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	effectiveAfter := afterSeq
	if advanceCursor && effectiveAfter < s.toolSeq {
		effectiveAfter = s.toolSeq
	}
	frames := append([]terminalOutputFrame(nil), s.frames...)
	dropped := s.dropped
	exited := s.exited

	out := tools.TerminalOutput{Chunks: make([]tools.TerminalOutputChunk, 0), NextSeq: effectiveAfter}
	if dropped && len(frames) > 0 && effectiveAfter < frames[0].seq-1 {
		out.ReplayGap = true
	}
	used := 0
	for _, frame := range frames {
		if frame.seq <= effectiveAfter {
			continue
		}
		if used > 0 && used+len(frame.data) > maxBytes {
			break
		}
		data := append([]byte(nil), frame.data...)
		out.Chunks = append(out.Chunks, tools.TerminalOutputChunk{Seq: frame.seq, Data: data})
		used += len(data)
		out.NextSeq = frame.seq
		if used >= maxBytes {
			break
		}
	}
	if exited != nil {
		out.Exited = true
		if exited.Exit != nil {
			out.Exit = &tools.ExitStatus{Code: exited.Exit.Code, Error: exited.Exit.Error}
		}
	}
	if advanceCursor {
		if out.NextSeq > s.toolSeq {
			s.toolSeq = out.NextSeq
		}
	}
	return out
}

func (r *terminalSessionRegistry) Input(ctx context.Context, agentID, terminalID string, data []byte) error {
	session, err := r.get(strings.TrimSpace(terminalID), strings.TrimSpace(agentID))
	if err != nil {
		return err
	}
	return session.input(ctx, data)
}

func (r *terminalSessionRegistry) Resize(ctx context.Context, agentID, terminalID string, rows, cols int) error {
	session, err := r.get(strings.TrimSpace(terminalID), strings.TrimSpace(agentID))
	if err != nil {
		return err
	}
	return session.resize(ctx, rows, cols)
}

func (r *terminalSessionRegistry) Terminate(ctx context.Context, agentID, terminalID string) (tools.TerminalOutput, error) {
	session, err := r.get(strings.TrimSpace(terminalID), strings.TrimSpace(agentID))
	if err != nil {
		return tools.TerminalOutput{}, err
	}
	out, err := session.terminate(ctx)
	// Terminate is the combined terminate/close operation. Remove the session
	// even when the provider reports a force-cleanup error, otherwise the UI and
	// the Agent would retain a session that can no longer be used.
	r.remove(strings.TrimSpace(terminalID), session)
	return out, err
}

func writeTerminalWSEventLocked(ctx context.Context, conn *websocket.Conn, event terminalWSEvent) error {
	if conn == nil {
		return fmt.Errorf("terminal websocket is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return wsjson.Write(ctx, conn, event)
}

func writeTerminalWSEventWithTimeout(conn *websocket.Conn, event terminalWSEvent) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return writeTerminalWSEventLocked(ctx, conn, event)
}
