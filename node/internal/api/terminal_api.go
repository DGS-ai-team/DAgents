package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// terminalWSCommand is deliberately byte-oriented. JSON encodes []byte as
// base64, so PowerShell control sequences and non-UTF-8 PTY output are not
// silently rewritten by the transport.
type terminalWSCommand struct {
	Type       string `json:"type"`
	SessionID  string `json:"session_id,omitempty"`
	TargetKind string `json:"target_kind,omitempty"`
	TargetID   string `json:"target_id,omitempty"`
	Shell      string `json:"shell,omitempty"`
	CWD        string `json:"cwd,omitempty"`
	Rows       int    `json:"rows,omitempty"`
	Cols       int    `json:"cols,omitempty"`
	Data       []byte `json:"data,omitempty"`
}

type terminalWSExit struct {
	Code  int    `json:"code"`
	Error string `json:"error,omitempty"`
}

type terminalWSEvent struct {
	Type       string          `json:"type"`
	SessionID  string          `json:"session_id,omitempty"`
	TerminalID string          `json:"terminal_id,omitempty"`
	Seq        uint64          `json:"seq,omitempty"`
	Replay     bool            `json:"replay,omitempty"`
	Data       []byte          `json:"data,omitempty"`
	Rows       int             `json:"rows,omitempty"`
	Cols       int             `json:"cols,omitempty"`
	Exit       *terminalWSExit `json:"exit,omitempty"`
	Error      string          `json:"error,omitempty"`
}

func (s *Server) registerTerminalRoutes() {
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/terminals/ws", s.handleAgentTerminalWS)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/terminals", s.handleAgentTerminals)
}

func (s *Server) ensureTerminalAgentRuntime(ctx context.Context, agentID string) error {
	if s == nil || s.agents == nil || s.sessions == nil {
		return nil
	}
	if s.sessions.SessionTools(agentID) != nil {
		return nil
	}
	rec, err := s.agents.Get(ctx, agentID)
	if err != nil {
		return err
	}
	if rec == nil || rec.Archived {
		return fmt.Errorf("agent_not_found")
	}
	return s.ensureAgentRuntime(ctx, agentID)
}

func (s *Server) handleAgentTerminals(w http.ResponseWriter, r *http.Request) {
	agentID := strings.TrimSpace(r.PathValue("agent_id"))
	if err := s.ensureTerminalAgentRuntime(r.Context(), agentID); err != nil {
		status := http.StatusInternalServerError
		code := "agent_ensure_failed"
		if err.Error() == "agent_not_found" {
			status = http.StatusNotFound
			code = "agent_not_found"
		}
		writeAPIError(w, status, code, err.Error(), map[string]any{"agent_id": agentID})
		return
	}
	if s.terminals == nil {
		writeJSON(w, http.StatusOK, map[string]any{"terminals": []any{}, "count": 0})
		return
	}
	items := s.terminals.List(agentID)
	writeJSON(w, http.StatusOK, map[string]any{"terminals": items, "count": len(items)})
}

func (s *Server) terminalToolsRegistry(agentID string) (*tools.Registry, error) {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return nil, fmt.Errorf("agent_id is required")
	}
	if s.sessions != nil {
		if registry := s.sessions.SessionTools(id); registry != nil {
			return registry, nil
		}
	}
	// The node-level registry is valid only for the node's own identity. Do
	// not accidentally expose one agent's workspace to another agent URL.
	if s.cfg != nil && id == strings.TrimSpace(s.cfg.NodeID) && s.tools != nil {
		return s.tools, nil
	}
	return nil, fmt.Errorf("agent runtime is unavailable")
}

func (s *Server) handleAgentTerminalWS(w http.ResponseWriter, r *http.Request) {
	agentID := strings.TrimSpace(r.PathValue("agent_id"))
	// Agent settings can open a terminal before the chat page has hydrated the
	// session. Load the per-agent runtime here so terminal policy, workspace,
	// and tool bindings match the selected Agent instead of returning a false
	// agent_runtime_missing response.
	if s.agents != nil && s.sessions != nil && s.sessions.SessionTools(agentID) == nil {
		rec, getErr := s.agents.Get(r.Context(), agentID)
		if getErr != nil {
			writeAPIError(w, http.StatusInternalServerError, "agent_runtime_load_failed", getErr.Error(), map[string]any{"agent_id": agentID})
			return
		}
		if rec != nil && !rec.Archived {
			if ensureErr := s.ensureAgentRuntime(r.Context(), agentID); ensureErr != nil {
				if ensureErr.Error() == "agent_not_found" {
					writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": agentID})
					return
				}
				writeAPIError(w, http.StatusInternalServerError, "agent_ensure_failed", ensureErr.Error(), map[string]any{"agent_id": agentID})
				return
			}
		}
	}
	_, err := s.terminalToolsRegistry(agentID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "agent_runtime_missing", err.Error(), map[string]any{"agent_id": agentID})
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "terminal closed")

	ctx := r.Context()
	var open terminalWSCommand
	if err := wsjson.Read(ctx, conn, &open); err != nil {
		return
	}

	var session *terminalSession
	created := false
	switch strings.ToLower(strings.TrimSpace(open.Type)) {
	case "open":
		created = true
		targetKind := strings.TrimSpace(open.TargetKind)
		if targetKind == "" {
			targetKind = "local"
		}
		target := tools.ExecutionTarget{Kind: targetKind, ID: strings.TrimSpace(open.TargetID)}
		request := tools.TerminalRequest{
			Target: target,
			Context: tools.ExecutionContext{
				AgentID:   agentID,
				SessionID: agentID,
				Target:    target,
			},
			CWD:   open.CWD,
			Shell: open.Shell,
			Rows:  open.Rows,
			Cols:  open.Cols,
		}
		if target.Kind == "local" {
			registry, registryErr := s.terminalToolsRegistry(agentID)
			if registryErr != nil {
				_ = writeTerminalWSEvent(ctx, conn, &sync.Mutex{}, terminalWSEvent{Type: "error", Error: registryErr.Error()})
				return
			}
			cwd, cwdErr := registry.ResolveLocalTerminalCWD(open.CWD)
			if cwdErr != nil {
				_ = writeTerminalWSEvent(ctx, conn, &sync.Mutex{}, terminalWSEvent{Type: "error", Error: cwdErr.Error()})
				return
			}
			request.CWD = cwd
		}
		// UI-created sessions must use the same broker path as terminal_open.
		// Besides keeping the session registry authoritative for both callers,
		// this gives a detached UI session the same keep-alive semantics as an
		// Agent-owned session. Leaving the terminal view is therefore a detach,
		// not an implicit close; explicit terminate/close still removes it.
		info, openErr := s.terminals.Open(ctx, agentID, request)
		if openErr != nil {
			_ = writeTerminalWSEvent(ctx, conn, &sync.Mutex{}, terminalWSEvent{Type: "error", Error: openErr.Error()})
			return
		}
		session, err = s.terminals.get(info.ID, agentID)
		if err != nil {
			_ = writeTerminalWSEvent(ctx, conn, &sync.Mutex{}, terminalWSEvent{Type: "error", Error: err.Error()})
			return
		}
	case "resume":
		session, err = s.terminals.get(strings.TrimSpace(open.SessionID), agentID)
		if err != nil {
			_ = writeTerminalWSEvent(ctx, conn, &sync.Mutex{}, terminalWSEvent{Type: "error", Error: err.Error()})
			return
		}
	default:
		_ = writeTerminalWSEvent(ctx, conn, &sync.Mutex{}, terminalWSEvent{Type: "error", Error: "first message must be open or resume"})
		return
	}

	if err := session.attach(ctx, conn); err != nil {
		session.detach(conn)
		if created {
			s.terminals.remove(session.id, session)
		}
		_ = writeTerminalWSEvent(ctx, conn, &sync.Mutex{}, terminalWSEvent{Type: "error", SessionID: session.id, Error: err.Error()})
		return
	}
	defer session.detach(conn)

	for {
		var command terminalWSCommand
		if err := wsjson.Read(ctx, conn, &command); err != nil {
			return
		}
		switch strings.ToLower(strings.TrimSpace(command.Type)) {
		case "input":
			if err := session.input(ctx, command.Data); err != nil {
				if session.send(ctx, terminalWSEvent{Type: "error", SessionID: session.id, TerminalID: session.id, Error: err.Error()}) != nil {
					return
				}
			}
		case "resize":
			if err := session.resize(ctx, command.Rows, command.Cols); err != nil {
				if session.send(ctx, terminalWSEvent{Type: "error", SessionID: session.id, TerminalID: session.id, Error: err.Error()}) != nil {
					return
				}
				continue
			}
			if err := session.send(ctx, terminalWSEvent{Type: "resized", SessionID: session.id, TerminalID: session.id, Rows: command.Rows, Cols: command.Cols}); err != nil {
				return
			}
		case "terminate":
			if _, err := session.terminate(ctx); err != nil {
				_ = session.send(ctx, terminalWSEvent{Type: "error", SessionID: session.id, TerminalID: session.id, Error: err.Error()})
				s.terminals.remove(session.id, session)
				return
			}
			sendErr := session.send(ctx, terminalWSEvent{Type: "terminated", SessionID: session.id, TerminalID: session.id})
			s.terminals.remove(session.id, session)
			if sendErr != nil {
				return
			}
			return
		case "close":
			if err := session.send(ctx, terminalWSEvent{Type: "closed", SessionID: session.id, TerminalID: session.id}); err != nil {
				return
			}
			s.terminals.remove(session.id, session)
			return
		case "ping":
			if err := session.send(ctx, terminalWSEvent{Type: "pong", SessionID: session.id, TerminalID: session.id}); err != nil {
				return
			}
		default:
			if err := session.send(ctx, terminalWSEvent{Type: "error", SessionID: session.id, TerminalID: session.id, Error: "unsupported terminal command"}); err != nil {
				return
			}
		}
	}
}

func writeTerminalWSEvent(ctx context.Context, conn *websocket.Conn, writeMu *sync.Mutex, event terminalWSEvent) error {
	if conn == nil {
		return fmt.Errorf("terminal websocket is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	writeMu.Lock()
	defer writeMu.Unlock()
	return writeTerminalWSEventLocked(ctx, conn, event)
}
