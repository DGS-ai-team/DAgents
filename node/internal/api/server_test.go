package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/compression"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
	"github.com/DGS-ai-team/DAgents/node/internal/version"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := &config.Config{
		NodeID:      "ops-linux-01",
		Agent:       config.AgentConfig{Name: "ops-linux"},
		Manage:      config.ManageConfig{},
		RuntimeRoot: t.TempDir(),
		Compression: config.CompressionConfig{
			SilentTriggerTokens:   80000,
			BlockingTriggerTokens: 100000,
		},
	}
	cfg.ApplyDefaults()
	return cfg
}

// createTestRuntime 在无 agents store 的单测中直接创建内存 runtime（/v1/sessions 路由已移除）。
func createTestRuntime(t *testing.T, srv *Server) string {
	t.Helper()
	if srv == nil || srv.sessions == nil {
		t.Fatal("sessions manager required")
	}
	sess, _, err := srv.sessions.Create("")
	if err != nil {
		t.Fatal(err)
	}
	if sess.ID == "" {
		t.Fatal("empty session_id")
	}
	return sess.ID
}

// waitSessionIdle 轮询直到 session turn 结束，避免 t.TempDir() 清理时后台仍写 runtime root。
func waitSessionIdle(t *testing.T, srv *Server, sessionID string) {
	t.Helper()
	waitSessionIdleDeadline(t, srv, sessionID, 3*time.Second)
}

func waitSessionIdleDeadline(t *testing.T, srv *Server, sessionID string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		_, hasActive, state, err := srv.sessions.RuntimeInfo(sessionID)
		if err == nil && !hasActive && state == turn.StateIdle {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timeout waiting session %s idle (err=%v active=%v state=%q)", sessionID, err, hasActive, state)
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func TestHandleHealth(t *testing.T) {
	srv := NewServer(testConfig(t), nil, WithSkipStore())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var got healthResponse
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "ok" || got.NodeID != "ops-linux-01" || got.Version != version.Version {
		t.Fatalf("unexpected body: %+v", got)
	}
}

func TestHandleAgentInfo(t *testing.T) {
	srv := NewServer(testConfig(t), nil, WithSkipStore())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/agent/info")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var got agentInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.NodeID != "ops-linux-01" {
		t.Fatalf("unexpected agent info: %+v", got)
	}
	if got.ManageRegistered {
		t.Fatal("N0 manage_registered should be false")
	}
	if got.Compression.SilentTriggerTokens != 80000 || got.Compression.BlockingTriggerTokens != 100000 {
		t.Fatalf("compression = %+v", got.Compression)
	}
}

func TestHandleDesktopRuntimeConfig(t *testing.T) {
	cfg := testConfig(t)
	cfg.Manage.Enabled = true
	cfg.Manage.URL = "http://manage.local/"
	cfg.Manage.NodeToken = "node-secret"
	cfg.Manage.Update.CheckIntervalSeconds = 17
	cfg.Manage.Update.Channel = "beta"
	srv := NewServer(cfg, nil, WithSkipStore())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/desktop/runtime-config")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var got desktopRuntimeConfig
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.ManageEnabled || got.ManageURL != "http://manage.local/" || got.ManageNodeToken != "node-secret" {
		t.Fatalf("unexpected manage config: %+v", got)
	}
	if got.ManageUpdateCheckIntervalSeconds != 17 || got.ManageUpdateChannel != "beta" || !got.ManageUpdateEnabled {
		t.Fatalf("unexpected update config: %+v", got)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/desktop/runtime-config", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	recorder := httptest.NewRecorder()
	srv.handleDesktopRuntimeConfig(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("non-loopback status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(testConfig(t), nil, WithLLM(&llm.MockClient{}), WithTools(reg), WithSkipStore())
	ts := httptest.NewServer(srv.Handler())
	// 须在 testConfig 的 t.TempDir 清理之前关闭 Server，否则 runtime root 仍被后台写入。
	t.Cleanup(func() {
		ts.Close()
		time.Sleep(50 * time.Millisecond)
	})
	return srv, ts
}

func TestSessionsRoutesRemoved(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	paths := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/v1/sessions"},
		{http.MethodGet, "/v1/sessions"},
		{http.MethodDelete, "/v1/sessions/sess-x"},
		{http.MethodGet, "/v1/sessions/sess-x/hydrate"},
		{http.MethodGet, "/v1/sessions/sess-x/context"},
		{http.MethodPost, "/v1/sessions/sess-x/ack"},
		{http.MethodGet, "/v1/sessions/sess-x/child-agents"},
		{http.MethodGet, "/v1/sessions/sess-x/media/med-1"},
	}
	for _, tc := range paths {
		req, err := http.NewRequest(tc.method, ts.URL+tc.path, bytes.NewReader([]byte(`{}`)))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		// 410 桩已删除：路由不再注册，应为 404（非 Gone / sessions_moved）。
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s %s status=%d body=%s", tc.method, tc.path, resp.StatusCode, body)
		}
	}
}

func TestHandleStreamsConnectsImmediately(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	sessionID := createTestRuntime(t, srv)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/streams?agent_id="+sessionID+"&live=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", resp.StatusCode)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("stream headers took too long: %v (expected immediate flush)", elapsed)
	}
}

func TestHandleStreamsAfterSeqReplaysHistory(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	sessionID := createTestRuntime(t, srv)
	first := srv.stream.Publish(sessionID, "assistant", map[string]any{"content": "hi"})
	_ = srv.stream.Publish(sessionID, "turn_finished", map[string]any{"finish_reason": "stop", "turn_complete": true})

	req, err := http.NewRequest(
		http.MethodGet,
		ts.URL+"/v1/streams?agent_id="+sessionID+"&after_agent_seq="+strconv.Itoa(first.AgentSeq),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", resp.StatusCode)
	}

	deadline := time.After(2 * time.Second)
	reader := bufio.NewReader(resp.Body)
	sawFinished := false
	for !sawFinished {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for replayed turn_finished")
		default:
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read sse: %v", err)
		}
		if strings.HasPrefix(line, "event: turn_finished") {
			sawFinished = true
		}
	}
}

func TestHandleStreamsReportsResyncWhenAgentHistoryWasTruncated(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	sessionID := createTestRuntime(t, srv)
	for i := 0; i < 300; i++ {
		srv.stream.Publish(sessionID, "assistant", map[string]any{"content": "x"})
	}

	resp, err := http.Get(ts.URL + "/v1/streams?agent_id=" + sessionID + "&after_agent_seq=0")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", resp.StatusCode)
	}
	reader := bufio.NewReader(resp.Body)
	seenResync := false
	for !seenResync {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.HasPrefix(strings.TrimSpace(line), "event: resync_required") {
			seenResync = true
		}
	}
}

func TestSessionMessageStreamE2E(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	sessionID := createTestRuntime(t, srv)

	// 先订阅 SSE
	streamReq, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/streams?agent_id="+sessionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	streamResp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatal(err)
	}
	defer streamResp.Body.Close()
	if streamResp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", streamResp.StatusCode)
	}

	// 发送消息
	msgBody := `{"agent_id":"` + sessionID + `","request_type":"message","content":"你好"}`
	msgResp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(msgBody))
	if err != nil {
		t.Fatal(err)
	}
	defer msgResp.Body.Close()
	if msgResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(msgResp.Body)
		t.Fatalf("message status = %d body=%s", msgResp.StatusCode, body)
	}

	// 从 SSE 读取 assistant + turn_finished
	deadline := time.After(3 * time.Second)
	reader := bufio.NewReader(streamResp.Body)
	var assistantText strings.Builder
	gotDone := false
	for !gotDone {
		select {
		case <-deadline:
			t.Fatalf("timeout; assistant=%q", assistantText.String())
		default:
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			if gotDone {
				break
			}
			t.Fatal(err)
		}
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var envelope struct {
			Type string         `json:"type"`
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
			continue
		}
		switch envelope.Type {
		case "assistant":
			if c, ok := envelope.Data["content"].(string); ok {
				assistantText.WriteString(c)
			}
		case "turn_finished":
			gotDone = true
		}
	}
	if !strings.Contains(assistantText.String(), "你好") {
		t.Fatalf("unexpected assistant: %q", assistantText.String())
	}
}

func TestPostMessageRejectsSessionIDOnly(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	body := `{"session_id":"sess-old","request_type":"message","content":"x"}`
	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestPostMessageSessionNotFound(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	body := `{"agent_id":"sess-missing","request_type":"message","content":"x"}`
	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestPostMessageAcceptsPythonClientFields(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	sessionID := createTestRuntime(t, srv)

	body := `{"agent_id":"` + sessionID + `","request_type":"message","content":"hi","client_id":"ignored","source":"ignored"}`
	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	waitSessionIdle(t, srv, sessionID)
}

func TestCreateRuntimeActiveFields(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	sessionID := createTestRuntime(t, srv)
	_, hasActive, state, err := srv.sessions.RuntimeInfo(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if hasActive {
		t.Fatal("new runtime should be idle")
	}
	if state != turn.StateIdle {
		t.Fatalf("state = %q", state)
	}
	if turn.RunTurnPhase(state) == "" {
		t.Fatal("run_turn_phase should be set for active session")
	}
}

func TestSessionPersistenceAPI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(testConfig(t), nil, WithLLM(&llm.MockClient{}), WithTools(reg), WithStore(st), WithSkipStore())
	t.Cleanup(srv.Close)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	sessionID := createTestRuntime(t, srv)

	msgBody := `{"agent_id":"` + sessionID + `","request_type":"message","content":"store-me"}`
	msgResp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(msgBody))
	if err != nil {
		t.Fatal(err)
	}
	msgResp.Body.Close()

	deadline := time.After(3 * time.Second)
	var ctxBody sessionContextResponse
	for {
		ctxResp, err := http.Get(ts.URL + "/v1/agents/" + sessionID + "/context")
		if err != nil {
			t.Fatal(err)
		}
		_ = json.NewDecoder(ctxResp.Body).Decode(&ctxBody)
		ctxResp.Body.Close()
		if ctxBody.MessagesCount >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timeout; count=%d", ctxBody.MessagesCount)
		default:
			time.Sleep(30 * time.Millisecond)
		}
	}
	if ctxBody.SystemPrompt == "" {
		t.Fatal("expected non-empty system_prompt in context view")
	}

	compressResp, err := http.Post(ts.URL+"/v1/agents/"+sessionID+"/compress", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var compressBody compression.ForceResult
	_ = json.NewDecoder(compressResp.Body).Decode(&compressBody)
	compressResp.Body.Close()
	if compressResp.StatusCode != http.StatusOK {
		t.Fatalf("compress status = %d body=%+v", compressResp.StatusCode, compressBody)
	}
	if compressBody.Status != "applied" && compressBody.Status != "noop" {
		t.Fatalf("unexpected compress status = %q", compressBody.Status)
	}

	ok, err := srv.sessions.Delete(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected delete ok")
	}
}
