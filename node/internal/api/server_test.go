package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/compression"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/version"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := &config.Config{
		AgentID: "ops-linux-01",
		Agent: config.AgentConfig{
			Role: "compliance",
		},
		FSRoot: t.TempDir(),
	}
	cfg.ApplyDefaults()
	return cfg
}

// waitSessionIdle 轮询直到 session turn 结束，避免 t.TempDir() 清理时后台仍写 FSRoot。
func waitSessionIdle(t *testing.T, baseURL, sessionID string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		resp, err := http.Get(baseURL + "/v1/sessions")
		if err != nil {
			t.Fatal(err)
		}
		var got listSessionsResponse
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			resp.Body.Close()
			t.Fatal(err)
		}
		resp.Body.Close()
		for _, item := range got.Sessions {
			if item.SessionID == sessionID && item.RunTurnPhase == "idle" && !item.HasActiveTurn {
				return
			}
		}
		select {
		case <-deadline:
			t.Fatalf("timeout waiting session %s idle", sessionID)
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
	if got.Status != "ok" || got.AgentID != "ops-linux-01" || got.Version != version.Version {
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
	if got.AgentID != "ops-linux-01" || !got.ExposeToPeers {
		t.Fatalf("unexpected agent info: %+v", got)
	}
	if got.ManageRegistered {
		t.Fatal("N0 manage_registered should be false")
	}
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(NewServer(testConfig(t), nil, WithLLM(&llm.MockClient{}), WithTools(reg), WithSkipStore()).Handler())
}

func TestHandleStreamsConnectsImmediately(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	createResp, err := http.Post(ts.URL+"/v1/sessions", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	var created createSessionResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	createResp.Body.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/streams?session_id="+created.SessionID+"&live=1", nil)
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

func TestSessionMessageStreamE2E(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// 创建 session
	createResp, err := http.Post(ts.URL+"/v1/sessions", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer createResp.Body.Close()
	var created createSessionResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.SessionID == "" {
		t.Fatal("empty session_id")
	}

	// 先订阅 SSE
	streamReq, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/streams?session_id="+created.SessionID, nil)
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
	msgBody := `{"session_id":"` + created.SessionID + `","request_type":"message","content":"你好"}`
	msgResp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(msgBody))
	if err != nil {
		t.Fatal(err)
	}
	defer msgResp.Body.Close()
	if msgResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(msgResp.Body)
		t.Fatalf("message status = %d body=%s", msgResp.StatusCode, body)
	}

	// 从 SSE 读取 assistant + done
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
		case "done":
			gotDone = true
		}
	}
	if !strings.Contains(assistantText.String(), "你好") {
		t.Fatalf("unexpected assistant: %q", assistantText.String())
	}
}

func TestPostMessageSessionNotFound(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	body := `{"session_id":"sess-missing","request_type":"message","content":"x"}`
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
	ts := newTestServer(t)
	defer ts.Close()

	createResp, err := http.Post(ts.URL+"/v1/sessions", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var created createSessionResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	createResp.Body.Close()

	body := `{"session_id":"` + created.SessionID + `","request_type":"message","content":"hi","client_id":"ignored","source":"ignored"}`
	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	waitSessionIdle(t, ts.URL, created.SessionID)
}

func TestListSessionsActiveRuntimeFields(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	createResp, err := http.Post(ts.URL+"/v1/sessions", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var created createSessionResponse
	_ = json.NewDecoder(createResp.Body).Decode(&created)
	createResp.Body.Close()

	listResp, err := http.Get(ts.URL + "/v1/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer listResp.Body.Close()
	var got listSessionsResponse
	if err := json.NewDecoder(listResp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Sessions) != 1 {
		t.Fatalf("sessions = %+v", got.Sessions)
	}
	item := got.Sessions[0]
	if !item.Active || item.SessionID != created.SessionID {
		t.Fatalf("unexpected session: %+v", item)
	}
	if item.RunTurnPhase == "" {
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
	ts := httptest.NewServer(NewServer(testConfig(t), nil,
		WithLLM(&llm.MockClient{}), WithTools(reg), WithStore(st)).Handler())
	defer ts.Close()

	createResp, err := http.Post(ts.URL+"/v1/sessions", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	var created createSessionResponse
	_ = json.NewDecoder(createResp.Body).Decode(&created)
	createResp.Body.Close()

	msgBody := `{"session_id":"` + created.SessionID + `","request_type":"message","content":"store-me"}`
	msgResp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(msgBody))
	if err != nil {
		t.Fatal(err)
	}
	msgResp.Body.Close()

	deadline := time.After(3 * time.Second)
	var ctxBody sessionContextResponse
	for {
		ctxResp, err := http.Get(ts.URL + "/v1/sessions/" + created.SessionID + "/context")
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

	compressResp, err := http.Post(ts.URL+"/v1/sessions/"+created.SessionID+"/compress", "application/json", nil)
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

	delReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/v1/sessions/"+created.SessionID, nil)
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatal(err)
	}
	delResp.Body.Close()
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", delResp.StatusCode)
	}
}
