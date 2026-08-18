package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestAgentTerminalToolSessionIsListedAndTerminated(t *testing.T) {
	cfg := testConfig(t)
	registry, err := tools.NewRegistry(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithTools(registry), WithSkipStore())
	t.Cleanup(srv.Close)
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(httpServer.Close)

	shell := "sh"
	if runtime.GOOS == "windows" {
		shell = "cmd"
	}
	opened, err := registry.Execute(context.Background(), "terminal_open", `{"config_id":"local","shell":"`+shell+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	var openedPayload struct {
		Terminal struct {
			ID string `json:"terminal_id"`
		} `json:"terminal"`
	}
	if err := json.Unmarshal([]byte(opened), &openedPayload); err != nil {
		t.Fatal(err)
	}
	if openedPayload.Terminal.ID == "" {
		t.Fatalf("opened=%s", opened)
	}

	resp, err := http.Get(httpServer.URL + "/v1/agents/" + cfg.NodeID + "/terminals")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var listed struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || listed.Count != 1 {
		t.Fatalf("status=%d listed=%+v", resp.StatusCode, listed)
	}

	terminated, err := registry.Execute(context.Background(), "terminal_terminate", `{"terminal_id":"`+openedPayload.Terminal.ID+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(terminated, `"status":"terminated"`) {
		t.Fatalf("terminated=%s", terminated)
	}
	resp2, err := http.Get(httpServer.URL + "/v1/agents/" + cfg.NodeID + "/terminals")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var closed struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&closed); err != nil {
		t.Fatal(err)
	}
	if resp2.StatusCode != http.StatusOK || closed.Count != 0 {
		t.Fatalf("after close status=%d listed=%+v", resp2.StatusCode, closed)
	}
}

func TestAgentTerminalWebSocketRunsPTY(t *testing.T) {
	cfg := testConfig(t)
	registry, err := tools.NewRegistry(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithTools(registry), WithSkipStore())
	t.Cleanup(srv.Close)
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(httpServer.Close)

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/v1/agents/" + cfg.NodeID + "/terminals/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	shell := "sh"
	input := []byte("printf 'websocket-pty-ready\\n'\nexit\n")
	if runtime.GOOS == "windows" {
		shell = "cmd"
		input = []byte("echo websocket-pty-ready\r\nexit\r\n")
	}
	if err := wsjson.Write(ctx, conn, terminalWSCommand{Type: "open", Shell: shell, Rows: 24, Cols: 80}); err != nil {
		t.Fatal(err)
	}
	var started terminalWSEvent
	if err := wsjson.Read(ctx, conn, &started); err != nil {
		t.Fatal(err)
	}
	if started.Type != "started" || started.TerminalID == "" {
		t.Fatalf("started=%+v", started)
	}
	if err := wsjson.Write(ctx, conn, terminalWSCommand{Type: "resize", Rows: 40, Cols: 120}); err != nil {
		t.Fatal(err)
	}
	var resized terminalWSEvent
	if err := wsjson.Read(ctx, conn, &resized); err != nil {
		t.Fatal(err)
	}
	if resized.Type != "resized" || resized.Rows != 40 || resized.Cols != 120 {
		t.Fatalf("resized=%+v", resized)
	}

	if runtime.GOOS == "windows" {
		// cmd may not accept input until its interactive prompt has been
		// emitted; consume output until the prompt while retaining it for the
		// same output assertion below.
		for {
			var event terminalWSEvent
			if err := wsjson.Read(ctx, conn, &event); err != nil {
				t.Fatal(err)
			}
			if event.Type == "output" && strings.Contains(string(event.Data), ">") {
				break
			}
		}
	}
	if err := wsjson.Write(ctx, conn, terminalWSCommand{Type: "input", Data: input}); err != nil {
		t.Fatal(err)
	}

	var output strings.Builder
	var exited terminalWSEvent
	for exited.Type == "" {
		var event terminalWSEvent
		if err := wsjson.Read(ctx, conn, &event); err != nil {
			t.Fatal(err)
		}
		switch event.Type {
		case "output":
			output.Write(event.Data)
		case "exited":
			exited = event
		case "error":
			t.Fatalf("terminal error: %s", event.Error)
		}
	}
	if !strings.Contains(output.String(), "websocket-pty-ready") {
		t.Fatalf("output=%q", output.String())
	}
	if exited.Exit == nil || exited.Exit.Code != 0 {
		t.Fatalf("exited=%+v", exited)
	}
	if err := wsjson.Write(ctx, conn, terminalWSCommand{Type: "close"}); err != nil {
		t.Fatal(err)
	}
}

func TestAgentTerminalWebSocketRejectsUnknownRuntime(t *testing.T) {
	cfg := testConfig(t)
	srv := NewServer(cfg, nil, WithSkipStore())
	t.Cleanup(srv.Close)
	req := httptest.NewRequest("GET", "/v1/agents/unknown-agent/terminals/ws", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAgentTerminalWebSocketDisconnectCanResume(t *testing.T) {
	cfg := testConfig(t)
	registry, err := tools.NewRegistry(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithTools(registry), WithSkipStore())
	t.Cleanup(srv.Close)
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(httpServer.Close)

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/v1/agents/" + cfg.NodeID + "/terminals/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	shell := "sh"
	if runtime.GOOS == "windows" {
		shell = "cmd"
	}
	if err := wsjson.Write(ctx, conn, terminalWSCommand{Type: "open", Shell: shell}); err != nil {
		conn.CloseNow()
		t.Fatal(err)
	}
	var started terminalWSEvent
	if err := wsjson.Read(ctx, conn, &started); err != nil {
		conn.CloseNow()
		t.Fatal(err)
	}
	input := []byte("printf 'resume-ready\\n'\n")
	if runtime.GOOS == "windows" {
		input = []byte("echo resume-ready\r\n")
	}
	if err := wsjson.Write(ctx, conn, terminalWSCommand{Type: "input", Data: input}); err != nil {
		conn.CloseNow()
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	conn.CloseNow()

	deadline := time.Now().Add(2 * time.Second)
	detached := false
	for time.Now().Before(deadline) {
		srv.terminals.mu.Lock()
		session := srv.terminals.sessions[started.SessionID]
		srv.terminals.mu.Unlock()
		if session != nil {
			session.mu.Lock()
			detached = session.conn == nil
			session.mu.Unlock()
			if detached {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !detached {
		t.Fatal("terminal session was not detached after WebSocket disconnect")
	}

	resumeConn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resumeConn.CloseNow()
	if err := wsjson.Write(ctx, resumeConn, terminalWSCommand{Type: "resume", SessionID: started.SessionID}); err != nil {
		t.Fatal(err)
	}
	var resumed terminalWSEvent
	if err := wsjson.Read(ctx, resumeConn, &resumed); err != nil {
		t.Fatal(err)
	}
	if resumed.Type != "started" || resumed.SessionID != started.SessionID {
		t.Fatalf("resumed=%+v", resumed)
	}
	var output strings.Builder
	alreadyExited := false
	for !strings.Contains(output.String(), "resume-ready") && !alreadyExited {
		var event terminalWSEvent
		if err := wsjson.Read(ctx, resumeConn, &event); err != nil {
			t.Fatal(err)
		}
		if event.Type == "output" {
			output.Write(event.Data)
		}
		if event.Type == "exited" {
			alreadyExited = true
		}
		if event.Type == "error" {
			t.Fatalf("resume error: %s", event.Error)
		}
	}
	if !alreadyExited {
		exitInput := []byte("exit\n")
		if runtime.GOOS == "windows" {
			exitInput = []byte("exit\r\n")
		}
		if err := wsjson.Write(ctx, resumeConn, terminalWSCommand{Type: "input", Data: exitInput}); err != nil {
			t.Fatal(err)
		}
		for {
			var event terminalWSEvent
			if err := wsjson.Read(ctx, resumeConn, &event); err != nil {
				t.Fatal(err)
			}
			if event.Type == "exited" {
				break
			}
		}
	}
	_ = wsjson.Write(ctx, resumeConn, terminalWSCommand{Type: "close"})
}
