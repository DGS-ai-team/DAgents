package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func autonomyV2TestServer(t *testing.T) (*Server, *store.AgentStore) {
	t.Helper()
	cfg := testConfig(t)
	s := NewServer(cfg, nil, WithLLM(&goalWakeLLM{}), WithSkipStore())
	if s.triggerSched != nil {
		s.triggerSched.Stop()
	}
	if s.maintenanceSched != nil {
		s.maintenanceSched.Stop()
	}
	as, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	s.agents = as
	now := time.Now().UTC()
	for _, typ := range []struct{ id, typ string }{{"auto-v2", "auto"}, {"normal-v2", "normal"}, {"other-v2", "auto"}} {
		if err := as.Save(context.Background(), store.AgentRecord{AgentID: typ.id, ConfigSnapshot: json.RawMessage(`{"agent_type":"` + typ.typ + `","defaults":{}}`), RuntimeRevision: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		if s.triggerSched != nil {
			s.triggerSched.Stop()
		}
		if s.maintenanceSched != nil {
			s.maintenanceSched.Stop()
		}
		if s.sessions != nil {
			s.sessions.Stop()
		}
		_ = as.Close()
	})
	return s, as
}

type autonomyV2PromptLLM struct {
	mu       sync.Mutex
	requests []llm.ChatRequest
	seen     chan llm.ChatRequest
}

func (c *autonomyV2PromptLLM) NormalizeAssistant(existing []llm.Message, m llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, m)
}
func (*autonomyV2PromptLLM) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (c *autonomyV2PromptLLM) StreamChat(_ context.Context, req llm.ChatRequest, h llm.StreamHandler) (llm.ChatResult, error) {
	c.mu.Lock()
	c.requests = append(c.requests, req)
	c.mu.Unlock()
	c.seen <- req
	if h.OnDelta != nil {
		h.OnDelta("ok")
	}
	if h.OnUsage != nil {
		h.OnUsage(llm.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2})
	}
	return llm.ChatResult{Content: "ok", FinishReason: "stop"}, nil
}

func autonomyV2Request(s *Server, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewBufferString(body)))
	return w
}

func TestAutonomyV2ConfigDefaultsPutCASAndStrictInput(t *testing.T) {
	s, _ := autonomyV2TestServer(t)
	w := autonomyV2Request(s, http.MethodGet, "/v1/agents/auto-v2/auto-config", "")
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"max_tool_rounds":32`)) {
		t.Fatalf("default=%d %s", w.Code, w.Body)
	}
	w = autonomyV2Request(s, http.MethodPut, "/v1/agents/auto-v2/auto-config", `{"expected_revision":0,"responsibility":"keep","wake_interval_seconds":60,"max_tool_rounds":4,"dreaming_enabled":false,"dreaming_time":"03:00","timezone":"UTC"}`)
	if w.Code != 200 {
		t.Fatalf("put=%d %s", w.Code, w.Body)
	}
	w = autonomyV2Request(s, http.MethodPut, "/v1/agents/auto-v2/auto-config", `{"expected_revision":0,"responsibility":"stale","wake_interval_seconds":60,"max_tool_rounds":4,"dreaming_enabled":false,"dreaming_time":"03:00","timezone":"UTC"}`)
	if w.Code != 409 {
		t.Fatalf("stale=%d %s", w.Code, w.Body)
	}
	w = autonomyV2Request(s, http.MethodPut, "/v1/agents/auto-v2/auto-config", `{"expected_revision":1,"experience":"forbidden","responsibility":"x","wake_interval_seconds":0,"max_tool_rounds":4,"dreaming_enabled":false,"dreaming_time":"03:00","timezone":"UTC"}`)
	if w.Code != 400 {
		t.Fatalf("strict=%d %s", w.Code, w.Body)
	}
	w = autonomyV2Request(s, http.MethodPut, "/v1/agents/auto-v2/auto-config", `{"expected_revision":1,"responsibility":"x","wake_interval_seconds":0,"max_tool_rounds":4,"dreaming_enabled":false,"dreaming_time":"03:00","timezone":"UTC"} {}`)
	if w.Code != 400 {
		t.Fatalf("trailing JSON=%d %s", w.Code, w.Body)
	}
	w = autonomyV2Request(s, http.MethodGet, "/v1/agents/auto-v2/auto-config", "")
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"responsibility":"keep"`)) {
		t.Fatalf("strict input changed config=%d %s", w.Code, w.Body)
	}
	w = autonomyV2Request(s, http.MethodGet, "/v1/agents/normal-v2/auto-config", "")
	if w.Code != 409 {
		t.Fatalf("normal=%d", w.Code)
	}
}

func TestAutonomyV2TodoAgentIsolationCASDeleteAndExperienceReadOnly(t *testing.T) {
	s, _ := autonomyV2TestServer(t)
	w := autonomyV2Request(s, http.MethodPost, "/v1/agents/auto-v2/todos", `{"text":"task"}`)
	if w.Code != 201 {
		t.Fatalf("create=%d %s", w.Code, w.Body)
	}
	var todo struct {
		ID       string `json:"id"`
		Revision int64  `json:"revision"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &todo); err != nil {
		t.Fatal(err)
	}
	w = autonomyV2Request(s, http.MethodPatch, "/v1/agents/other-v2/todos/"+todo.ID, `{"expected_revision":1,"status":"completed"}`)
	if w.Code != 404 {
		t.Fatalf("cross patch=%d", w.Code)
	}
	w = autonomyV2Request(s, http.MethodPatch, "/v1/agents/auto-v2/todos/"+todo.ID, `{"expected_revision":99,"status":"completed"}`)
	if w.Code != 409 {
		t.Fatalf("cas=%d", w.Code)
	}
	w = autonomyV2Request(s, http.MethodDelete, "/v1/agents/other-v2/todos/"+todo.ID, `{"expected_revision":1}`)
	if w.Code != 404 {
		t.Fatalf("cross delete=%d", w.Code)
	}
	w = autonomyV2Request(s, http.MethodGet, "/v1/agents/auto-v2/experience", "")
	if w.Code != 200 {
		t.Fatalf("experience=%d", w.Code)
	}
	w = autonomyV2Request(s, http.MethodPut, "/v1/agents/auto-v2/auto-config", `{"expected_revision":0,"responsibility":"x","wake_interval_seconds":0,"max_tool_rounds":4,"dreaming_enabled":false,"dreaming_time":"03:00","timezone":"UTC","experience":"bad"}`)
	if w.Code != 400 {
		t.Fatalf("experience write=%d", w.Code)
	}
}

func TestAutonomyV2TodoToolsWiredOnlyForAutoRuntime(t *testing.T) {
	s, _ := autonomyV2TestServer(t)
	autoReg, err := tools.NewRegistry(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	autoReg.SetAutonomyRuntime(true, nil, nil)
	s.attachNodeRuntimeDeps(autoReg, "auto-v2")
	defs := autoReg.Definitions()
	for _, name := range []string{"todo_list", "todo_create", "todo_update", "todo_delete"} {
		found := false
		for _, d := range defs {
			if d.Function.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("auto runtime omitted %s", name)
		}
	}
	if _, err := autoReg.Execute(context.Background(), "todo_create", `{"call_purpose":"test","text":"wired"}`); err != nil {
		t.Fatal(err)
	}

	normalReg, err := tools.NewRegistry(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	normalReg.SetAutonomyRuntime(true, nil, nil)
	s.attachNodeRuntimeDeps(normalReg, "normal-v2")
	for _, d := range normalReg.Definitions() {
		if strings.HasPrefix(d.Function.Name, "todo_") {
			t.Fatalf("normal runtime exposed %s", d.Function.Name)
		}
	}
	if _, err := normalReg.Execute(context.Background(), "todo_list", `{"call_purpose":"test"}`); err == nil {
		t.Fatal("normal runtime executed todo")
	}
}

func TestAutonomyV2SavedResponsibilityReachesMainSession(t *testing.T) {
	requests := make(chan string, 4)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode provider request: %v", err)
			return
		}
		system := ""
		for _, m := range payload.Messages {
			if m.Role == "system" {
				var text string
				if json.Unmarshal(m.Content, &text) == nil {
					system = text
				} else {
					system = string(m.Content)
				}
				break
			}
		}
		requests <- system
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer provider.Close()
	cfg := testConfig(t)
	cfg.Onboarding.NodeProfileCompleted = true
	cfg.LLM.Profiles["default"] = config.LLMProfileConfig{Provider: "openai", BaseURL: provider.URL, Model: "test-model", APIKeyEnv: "DAGENTS_AUTONOMY_TEST_KEY"}
	cfg.LLM.Active = "default"
	cfg.LLM.Provider, cfg.LLM.BaseURL, cfg.LLM.Model, cfg.LLM.APIKeyEnv = "openai", provider.URL, "test-model", "DAGENTS_AUTONOMY_TEST_KEY"
	t.Setenv("DAGENTS_AUTONOMY_TEST_KEY", "test-key")
	s := NewServer(cfg, nil, WithSkipStore())
	if s.triggerSched != nil {
		s.triggerSched.Stop()
	}
	if s.maintenanceSched != nil {
		s.maintenanceSched.Stop()
	}
	as, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	s.agents = as
	now := time.Now().UTC()
	if err := as.Save(context.Background(), store.AgentRecord{
		AgentID: "auto-v2-main", ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`),
		RuntimeRevision: 1, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if s.triggerSched != nil {
			s.triggerSched.Stop()
		}
		if s.maintenanceSched != nil {
			s.maintenanceSched.Stop()
		}
		s.sessions.Stop()
		_ = as.Close()
	})

	put := func(rev int64, responsibility string) {
		body := fmt.Sprintf(`{"expected_revision":%d,"responsibility":%q,"wake_interval_seconds":0,"max_tool_rounds":4,"dreaming_enabled":false,"dreaming_time":"03:00","timezone":"UTC"}`, rev, responsibility)
		w := autonomyV2Request(s, http.MethodPut, "/v1/agents/auto-v2-main/auto-config", body)
		if w.Code != http.StatusOK {
			t.Fatalf("put responsibility=%q: %d %s", responsibility, w.Code, w.Body)
		}
	}
	enqueue := func(content string) string {
		w := autonomyV2Request(s, http.MethodPost, "/v1/messages", fmt.Sprintf(`{"agent_id":"auto-v2-main","content":%q}`, content))
		if w.Code != http.StatusOK {
			t.Fatalf("enqueue: %d %s", w.Code, w.Body)
		}
		select {
		case system := <-requests:
			return system
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for main session model request")
		}
		return ""
	}

	put(0, "first saved responsibility")
	first := enqueue("first message")
	if !strings.Contains(first, "first saved responsibility") {
		t.Fatalf("first turn system prompt omitted saved responsibility: %q", first)
	}
	put(1, "second saved responsibility")
	second := enqueue("second message")
	if !strings.Contains(second, "second saved responsibility") || strings.Contains(second, "first saved responsibility") {
		t.Fatalf("second turn did not refresh saved responsibility: %q", second)
	}
}

func TestAutonomyV2TodoToolRunsThroughMainSession(t *testing.T) {
	var mu sync.Mutex
	var calls int
	callSeen := make(chan int, 4)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		callSeen <- n
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"todo-1\",\"type\":\"function\",\"function\":{\"name\":\"todo_create\",\"arguments\":\"{\\\"call_purpose\\\":\\\"maintain todo\\\",\\\"text\\\":\\\"from session\\\"}\"}}]}}]}\n\n")
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
		} else {
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"}}]}\n\n")
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		}
		_, _ = fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer provider.Close()
	cfg := testConfig(t)
	cfg.LLM.Profiles["default"] = config.LLMProfileConfig{Provider: "openai", BaseURL: provider.URL, Model: "test-model", APIKeyEnv: "DAGENTS_AUTONOMY_TEST_KEY"}
	cfg.LLM.Active, cfg.LLM.Provider, cfg.LLM.BaseURL, cfg.LLM.Model, cfg.LLM.APIKeyEnv = "default", "openai", provider.URL, "test-model", "DAGENTS_AUTONOMY_TEST_KEY"
	t.Setenv("DAGENTS_AUTONOMY_TEST_KEY", "test-key")
	s := NewServer(cfg, nil, WithSkipStore())
	if s.triggerSched != nil {
		s.triggerSched.Stop()
	}
	if s.maintenanceSched != nil {
		s.maintenanceSched.Stop()
	}
	as, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	s.agents = as
	now := time.Now().UTC()
	if err := s.agents.Save(context.Background(), store.AgentRecord{AgentID: "auto-v2-tools", ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), RuntimeRevision: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.agents.SaveAgentPolicy(context.Background(), store.AgentPolicyRecord{AgentID: "auto-v2-tools", Tools: map[string]string{"todo_create": "never"}, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if s.triggerSched != nil {
			s.triggerSched.Stop()
		}
		if s.maintenanceSched != nil {
			s.maintenanceSched.Stop()
		}
		s.sessions.Stop()
		_ = as.Close()
	})
	w := autonomyV2Request(s, http.MethodPut, "/v1/agents/auto-v2-tools/auto-config", `{"expected_revision":0,"responsibility":"session todo owner","wake_interval_seconds":0,"max_tool_rounds":4,"dreaming_enabled":false,"dreaming_time":"03:00","timezone":"UTC"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("put=%d %s", w.Code, w.Body)
	}
	w = autonomyV2Request(s, http.MethodPost, "/v1/messages", `{"agent_id":"auto-v2-tools","content":"create a todo"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("enqueue=%d %s", w.Code, w.Body)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(s.autonomyStore.ListTodos("auto-v2-tools")) == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	todos := s.autonomyStore.ListTodos("auto-v2-tools")
	if len(todos) != 1 || todos[0].Text != "from session" {
		t.Fatalf("session tool did not persist todo: %+v", todos)
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case n := <-callSeen:
			if n >= 2 {
				return
			}
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	mu.Lock()
	gotCalls := calls
	mu.Unlock()
	t.Fatalf("expected tool continuation model call, got %d", gotCalls)
}
