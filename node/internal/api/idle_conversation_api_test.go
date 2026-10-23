package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestIdleConversationBoundaryThroughHTTP(t *testing.T) {
	t.Setenv(config.EnvRawMessageHistoryEnabled, "false")
	t.Setenv("IDLE_BOUNDARY_TEST_API_KEY", "test")
	t.Setenv("OPENAI_API_KEY", "test")
	cfg := &config.Config{NodeID: "idle-boundary-test", RuntimeRoot: t.TempDir()}
	cfg.ApplyDefaults()
	cfg.Onboarding.NodeProfileCompleted = true
	cfg.Compression.IdleNewConversationSeconds = 1
	st, err := store.Open(cfg.SessionDBPath())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := tools.NewRegistry(cfg.RuntimeDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.CloseBrowser()

	var mu sync.Mutex
	var requests []map[string]any
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode provider request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, body)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"assistant response\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n")
	}))
	defer provider.Close()
	cfg.LLM.Provider = "openai"
	cfg.LLM.BaseURL = provider.URL
	cfg.LLM.Model = "idle-test-model"
	cfg.LLM.APIKeyEnv = "IDLE_BOUNDARY_TEST_API_KEY"
	cfg.SyncActiveProfileFromFlat()
	client := &idleConversationHTTPClient{inner: llm.NewOpenAIClient(llm.OpenAIConfig{BaseURL: provider.URL, Model: "idle-test-model", APIKey: "test"})}
	srv := NewServer(cfg, nil, WithLLM(client), WithTools(registry), WithStore(st), WithSkipStore())
	ts := httptest.NewServer(srv.Handler())
	agents, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	srv.agents = agents
	t.Cleanup(func() { ts.Close(); srv.Close(); _ = agents.Close() })

	createBody := map[string]any{
		"display_name": "Idle boundary test", "agent_type": "normal",
		"defaults": map[string]any{
			"llm":            map[string]any{"active": "default", "max_steps": 4},
			"tools":          map[string]any{"enabled_groups": []string{"fs"}},
			"prompt_context": map[string]any{"memory_enabled": false},
		},
	}
	raw, _ := json.Marshal(createBody)
	createResp, err := http.Post(ts.URL+"/v1/agents", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	var created agentView
	if createResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(createResp.Body)
		createResp.Body.Close()
		t.Fatalf("create Agent status=%d body=%s", createResp.StatusCode, body)
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		createResp.Body.Close()
		t.Fatal(err)
	}
	createResp.Body.Close()
	if created.AgentID == "" {
		t.Fatal("created Agent has no ID")
	}

	waitProviderCalls := func(want int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			got := len(requests)
			mu.Unlock()
			if got >= want {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		mu.Lock()
		got := len(requests)
		mu.Unlock()
		t.Fatalf("provider request count=%d want at least %d", got, want)
	}
	postMessage := func(content string, expectedCalls int) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"agent_id": created.AgentID, "request_type": "message", "content": content})
		resp, err := http.Post(ts.URL+"/v1/messages", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			content, _ := io.ReadAll(resp.Body)
			t.Fatalf("post message status=%d body=%s", resp.StatusCode, content)
		}
		waitProviderCalls(expectedCalls)
		waitSessionIdle(t, srv, created.AgentID)
	}

	oldText := strings.Repeat("OLD_BLOCK ", 450) + "OLD_END_MARKER"
	postMessage(oldText, 1)
	// The initial human message establishes the runtime's idle baseline. The
	// second message arrives after the configured threshold.
	baselineDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(baselineDeadline) {
		record, loadErr := st.Load(context.Background(), created.AgentID)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if record != nil && !record.RuntimeState.LastHumanInteraction.IsZero() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	record, err := st.Load(context.Background(), created.AgentID)
	if err != nil || record == nil || record.RuntimeState.LastHumanInteraction.IsZero() {
		t.Fatalf("human idle baseline was not persisted: record=%+v err=%v", record, err)
	}
	remaining := time.Until(record.RuntimeState.LastHumanInteraction.Add(1200 * time.Millisecond))
	if remaining > 0 {
		time.Sleep(remaining)
	}
	boundaryEvents := srv.stream.SubscribeAgent(srv.stream.CurrentSeq(), created.AgentID)
	defer srv.stream.Unsubscribe(boundaryEvents)
	postMessage("CURRENT_MESSAGE_MARKER", 2)
	var boundaryData map[string]any
	deadline := time.After(2 * time.Second)
	for boundaryData == nil {
		select {
		case event, ok := <-boundaryEvents:
			if !ok {
				t.Fatal("event stream closed before boundary notice")
			}
			if event.Type == "runtime/notice" && event.Data["message_kind"] == "context_boundary" {
				boundaryData = event.Data
			}
		case <-deadline:
			mu.Lock()
			latest := append([]map[string]any(nil), requests...)
			mu.Unlock()
			raw, _ := json.Marshal(latest)
			t.Fatalf("timed out waiting for context boundary event; provider requests=%s", raw)
		}
	}
	if boundaryData["message_kind"] != "context_boundary" {
		t.Fatalf("boundary SSE data = %#v", boundaryData)
	}

	mu.Lock()
	captured := append([]map[string]any(nil), requests...)
	mu.Unlock()
	if len(captured) != 2 {
		t.Fatalf("provider request count=%d want=2", len(captured))
	}
	secondRaw, _ := json.Marshal(captured[1])
	second := string(secondRaw)
	if !strings.Contains(second, "CURRENT_MESSAGE_MARKER") {
		t.Fatalf("second request missing current input: %s", second)
	}
	if strings.Contains(second, "OLD_END_MARKER") {
		t.Fatal("second request contains the previous full request")
	}
	if !strings.Contains(second, "上一段对话的请求") || !strings.Contains(second, "history/") {
		t.Fatalf("second request missing boundary bridge/archive reference: %s", second)
	}
	archivePath := ""
	providerMessages, _ := captured[1]["messages"].([]any)
	for _, rawMessage := range providerMessages {
		message, _ := rawMessage.(map[string]any)
		content, _ := message["content"].(string)
		const marker = "历史记录已归档到："
		if idx := strings.Index(content, marker); idx >= 0 {
			archivePath = strings.TrimSpace(strings.SplitN(content[idx+len(marker):], "\n", 2)[0])
		}
	}
	if !filepath.IsAbs(archivePath) {
		t.Fatalf("archive reference is not absolute: %q", archivePath)
	}

	workspaceRoot, ok := srv.sessions.SessionWorkspaceRoot(created.AgentID)
	if !ok {
		t.Fatal("Agent workspace root unavailable")
	}
	stateRoot, err := agentruntime.WorkspaceStateRoot(workspaceRoot, created.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Clean(archivePath), filepath.Clean(stateRoot)+string(filepath.Separator)) {
		t.Fatalf("archive path %q is outside Agent state root %q", archivePath, stateRoot)
	}
	relativeArchive, err := filepath.Rel(workspaceRoot, archivePath)
	if err != nil {
		t.Fatal(err)
	}
	readRegistry := srv.sessions.SessionTools(created.AgentID)
	if readRegistry == nil {
		t.Fatal("Agent read_file registry unavailable")
	}
	readArgs, _ := json.Marshal(map[string]any{"path": relativeArchive})
	archiveContents, err := readRegistry.Execute(context.Background(), "read_file", string(readArgs))
	if err != nil {
		t.Fatalf("read_file archive: %v", err)
	}
	if !strings.Contains(archiveContents, "OLD_END_MARKER") || !strings.Contains(archiveContents, "assistant response") {
		t.Fatalf("read_file did not expose complete archive: %s", archiveContents)
	}
	if logs, _ := filepath.Glob(filepath.Join(stateRoot, "history", "*", "*.jsonl")); len(logs) != 0 {
		t.Fatalf("raw journal should be disabled, got %v", logs)
	}
	// The persisted bridge is authoritative for its absolute reference; inspect
	// hydrate as the UI does and verify it exposes only a divider marker.
	resp, err := http.Get(ts.URL + "/v1/agents/" + created.AgentID + "/hydrate")
	if err != nil {
		t.Fatal(err)
	}
	var hydrated struct {
		Transcript []map[string]any `json:"transcript"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&hydrated); err != nil {
		resp.Body.Close()
		t.Fatal(err)
	}
	resp.Body.Close()
	var gotOldHuman, gotBoundary bool
	var hydratedBoundaryID string
	boundaryCount := 0
	for _, entry := range hydrated.Transcript {
		if entry["kind"] == "user" && strings.Contains(asString(entry["text"]), "OLD_END_MARKER") {
			gotOldHuman = true
		}
		if entry["kind"] == "context_boundary" {
			gotBoundary = true
			boundaryCount++
			hydratedBoundaryID, _ = entry["boundary_id"].(string)
			if _, ok := entry["text"]; ok {
				t.Fatalf("boundary exposed bridge body: %#v", entry)
			}
		}
		if strings.Contains(asString(entry["text"]), "上一段对话的请求") {
			t.Fatalf("hydrate exposed bridge body: %#v", entry)
		}
	}
	if !gotOldHuman || !gotBoundary {
		t.Fatalf("hydrate missing full history or divider: %#v", hydrated.Transcript)
	}
	if boundaryCount != 1 {
		t.Fatalf("hydrate boundary count=%d want=1", boundaryCount)
	}
	if hydratedBoundaryID == "" || boundaryData["boundary_id"] != hydratedBoundaryID {
		t.Fatalf("SSE/hydrate boundary IDs differ: sse=%#v hydrate=%q", boundaryData["boundary_id"], hydratedBoundaryID)
	}
	boundaryJSON, _ := json.Marshal(boundaryData)
	if strings.Contains(string(boundaryJSON), "上一段对话的请求") || strings.Contains(string(boundaryJSON), archivePath) {
		t.Fatalf("SSE leaked bridge body or archive reference: %s", boundaryJSON)
	}
}

type idleConversationHTTPClient struct{ inner *llm.OpenAIClient }

func (c *idleConversationHTTPClient) StreamChat(ctx context.Context, req llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	return c.inner.StreamChat(ctx, req, handler)
}
func (c *idleConversationHTTPClient) CompleteText(ctx context.Context, req llm.CompleteRequest) (string, error) {
	return c.inner.CompleteText(ctx, req)
}
func (c *idleConversationHTTPClient) NormalizeAssistant(existing []llm.Message, message llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, message)
}

func asString(value any) string { text, _ := value.(string); return text }
