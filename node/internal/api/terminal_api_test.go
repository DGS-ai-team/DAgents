package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func executeTerminalTool(t *testing.T, registry *tools.Registry, name, arguments string) string {
	t.Helper()
	result, err := registry.Execute(context.Background(), name, arguments)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return result
}

func terminalInputArguments(t *testing.T, terminalID, data string) string {
	t.Helper()
	arguments, err := json.Marshal(struct {
		TerminalID string `json:"terminal_id"`
		Data       string `json:"data"`
	}{TerminalID: terminalID, Data: data})
	if err != nil {
		t.Fatal(err)
	}
	return string(arguments)
}

func waitForTerminalSessionOutput(t *testing.T, session *terminalSession, want string) string {
	t.Helper()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for {
		session.mu.Lock()
		wake := session.outputWake
		session.mu.Unlock()

		out := session.snapshotOutput(0, terminalReplayBytes, false)
		var output strings.Builder
		for _, chunk := range out.Chunks {
			output.Write(chunk.Data)
		}
		if strings.Contains(output.String(), want) {
			return output.String()
		}
		select {
		case <-wake:
		case <-timeout.C:
			t.Fatalf("terminal output did not contain %q; got %q", want, output.String())
		}
	}
}

func waitForTerminalSessionDetached(t *testing.T, session *terminalSession) {
	t.Helper()
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		session.mu.Lock()
		detached := session.conn == nil
		session.mu.Unlock()
		if detached {
			return
		}
		select {
		case <-ticker.C:
		case <-timeout.C:
			t.Fatal("terminal WebSocket did not detach")
		}
	}
}

func TestAgentTerminalToolSessionResumesAcrossWebSocketDisconnect(t *testing.T) {
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

	terminalID := openedPayload.Terminal.ID
	input := "echo tool-session-resume-ready\n"
	if runtime.GOOS == "windows" {
		input = "echo tool-session-resume-ready\r\n"
	}
	executeTerminalTool(t, registry, "terminal_input", terminalInputArguments(t, terminalID, input))
	srv.terminals.mu.Lock()
	toolSession := srv.terminals.sessions[terminalID]
	srv.terminals.mu.Unlock()
	if toolSession == nil {
		t.Fatalf("tool-opened terminal session %q was not registered", terminalID)
	}
	waitForTerminalSessionOutput(t, toolSession, "tool-session-resume-ready")

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/v1/agents/" + cfg.NodeID + "/terminals/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resumeConn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(ctx, resumeConn, terminalWSCommand{Type: "resume", SessionID: terminalID}); err != nil {
		resumeConn.CloseNow()
		t.Fatal(err)
	}
	var resumed terminalWSEvent
	if err := wsjson.Read(ctx, resumeConn, &resumed); err != nil {
		resumeConn.CloseNow()
		t.Fatal(err)
	}
	if resumed.Type != "started" || resumed.SessionID != terminalID {
		resumeConn.CloseNow()
		t.Fatalf("tool-opened session did not resume: %+v", resumed)
	}
	var replayed strings.Builder
	for !strings.Contains(replayed.String(), "tool-session-resume-ready") {
		var event terminalWSEvent
		if err := wsjson.Read(ctx, resumeConn, &event); err != nil {
			resumeConn.CloseNow()
			t.Fatal(err)
		}
		if event.Type == "error" {
			resumeConn.CloseNow()
			t.Fatalf("resume error: %s", event.Error)
		}
		if event.Type == "output" {
			replayed.Write(event.Data)
		}
	}
	resumeConn.CloseNow()
	waitForTerminalSessionDetached(t, toolSession)
	if got := len(srv.terminals.List(cfg.NodeID)); got != 1 {
		t.Fatalf("tool-opened session after UI disconnect=%d, want 1", got)
	}
	toolRead := executeTerminalTool(t, registry, "terminal_read", `{"terminal_id":"`+terminalID+`"}`)
	if !strings.Contains(toolRead, "tool-session-resume-ready") {
		t.Fatalf("UI replay consumed tool-side unread output: %s", toolRead)
	}

	input = "echo tool-write-after-ui-disconnect\n"
	if runtime.GOOS == "windows" {
		input = "echo tool-write-after-ui-disconnect\r\n"
	}
	executeTerminalTool(t, registry, "terminal_input", terminalInputArguments(t, terminalID, input))
	waitForTerminalSessionOutput(t, toolSession, "tool-write-after-ui-disconnect")
	toolRead = executeTerminalTool(t, registry, "terminal_read", `{"terminal_id":"`+terminalID+`"}`)
	if !strings.Contains(toolRead, "tool-write-after-ui-disconnect") {
		t.Fatalf("tool-side read after disconnect=%s", toolRead)
	}

	wrongAgentURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/v1/agents/other-agent/terminals/ws"
	wrongAgentConn, wrongAgentResponse, wrongAgentErr := websocket.Dial(ctx, wrongAgentURL, nil)
	if wrongAgentConn != nil {
		wrongAgentConn.CloseNow()
	}
	if wrongAgentErr == nil || wrongAgentResponse == nil || wrongAgentResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-Agent WebSocket resume status=%v err=%v, want 404", wrongAgentResponse, wrongAgentErr)
	}
	if _, err := srv.terminals.get(terminalID, "other-agent"); err == nil {
		t.Fatal("terminal registry allowed another Agent to resume the session")
	}

	resumeConn, _, err = websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(ctx, resumeConn, terminalWSCommand{Type: "resume", SessionID: terminalID}); err != nil {
		resumeConn.CloseNow()
		t.Fatal(err)
	}
	if err := wsjson.Read(ctx, resumeConn, &resumed); err != nil {
		resumeConn.CloseNow()
		t.Fatal(err)
	}
	if resumed.Type != "started" || resumed.SessionID != terminalID {
		resumeConn.CloseNow()
		t.Fatalf("tool-opened session did not resume after tool writes: %+v", resumed)
	}
	var secondReplay strings.Builder
	for !strings.Contains(secondReplay.String(), "tool-write-after-ui-disconnect") {
		var event terminalWSEvent
		if err := wsjson.Read(ctx, resumeConn, &event); err != nil {
			resumeConn.CloseNow()
			t.Fatal(err)
		}
		if event.Type == "error" {
			resumeConn.CloseNow()
			t.Fatalf("second resume error: %s", event.Error)
		}
		if event.Type == "output" {
			secondReplay.Write(event.Data)
		}
	}
	if err := wsjson.Write(ctx, resumeConn, terminalWSCommand{Type: "close"}); err != nil {
		resumeConn.CloseNow()
		t.Fatal(err)
	}
	var closedEvent terminalWSEvent
	for closedEvent.Type == "" {
		if err := wsjson.Read(ctx, resumeConn, &closedEvent); err != nil {
			resumeConn.CloseNow()
			t.Fatal(err)
		}
		if closedEvent.Type == "error" {
			resumeConn.CloseNow()
			t.Fatalf("explicit close error: %s", closedEvent.Error)
		}
		if closedEvent.Type != "closed" {
			closedEvent = terminalWSEvent{}
		}
	}
	if closedEvent.SessionID != terminalID {
		resumeConn.CloseNow()
		t.Fatalf("explicit close event=%+v", closedEvent)
	}
	resumeConn.CloseNow()
	resp2, err := http.Get(httpServer.URL + "/v1/agents/" + cfg.NodeID + "/terminals")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var closedList struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&closedList); err != nil {
		t.Fatal(err)
	}
	if resp2.StatusCode != http.StatusOK || closedList.Count != 0 {
		t.Fatalf("after explicit close status=%d listed=%+v", resp2.StatusCode, closedList)
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
	srv.terminals.mu.Lock()
	uiSession := srv.terminals.sessions[started.SessionID]
	srv.terminals.mu.Unlock()
	if uiSession == nil {
		t.Fatalf("UI terminal session %q was not registered", started.SessionID)
	}
	uiSession.mu.Lock()
	keepAlive := uiSession.keepAlive
	uiSession.mu.Unlock()
	if !keepAlive {
		t.Fatal("UI-created terminal session is not keep-alive")
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
	srv.terminals.mu.Lock()
	resumedSession := srv.terminals.sessions[started.SessionID]
	srv.terminals.mu.Unlock()
	if resumedSession == nil {
		t.Fatal("detached UI terminal session was removed instead of retained")
	}
	resumedSession.mu.Lock()
	keepAlive := resumedSession.keepAlive
	expiry := resumedSession.expiry
	resumedSession.mu.Unlock()
	if !keepAlive || expiry != nil {
		t.Fatalf("detached UI session retention: keepAlive=%v expiry=%v", keepAlive, expiry)
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

func TestAgentTerminalDeleteClosesDetachedSessionAndChecksAgentOwnership(t *testing.T) {
	cfg := testConfig(t)
	// Keep the spawned shell's working directory outside testing.T.TempDir:
	// Windows may release a closed ConPTY process asynchronously, and the
	// test runner cannot remove a directory while that process still uses it.
	registry, err := tools.NewRegistry(os.TempDir(), 10)
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
		t.Fatal("terminal session did not detach after WebSocket close")
	}
	if got := len(srv.terminals.List(cfg.NodeID)); got != 1 {
		t.Fatalf("detached sessions=%d, want 1 before API close", got)
	}

	wrongOwnerReq, err := http.NewRequest(http.MethodDelete, httpServer.URL+"/v1/agents/other-agent/terminals/"+started.SessionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	wrongOwnerResp, err := http.DefaultClient.Do(wrongOwnerReq)
	if err != nil {
		t.Fatal(err)
	}
	wrongOwnerResp.Body.Close()
	if wrongOwnerResp.StatusCode != http.StatusNotFound {
		t.Fatalf("wrong-owner delete status=%d, want 404", wrongOwnerResp.StatusCode)
	}
	if got := len(srv.terminals.List(cfg.NodeID)); got != 1 {
		t.Fatalf("wrong-owner delete changed original Agent sessions=%d, want 1", got)
	}

	deleteReq, err := http.NewRequest(http.MethodDelete, httpServer.URL+"/v1/agents/"+cfg.NodeID+"/terminals/"+started.SessionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatal(err)
	}
	deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusOK {
		t.Fatalf("detached close status=%d, want 200", deleteResp.StatusCode)
	}
	if got := len(srv.terminals.List(cfg.NodeID)); got != 0 {
		t.Fatalf("sessions after detached close=%d, want 0", got)
	}
}
