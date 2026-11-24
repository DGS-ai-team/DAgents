package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// This test intentionally starts the real dagents-node binary twice. It is
// kept outside node/internal/api so it verifies process startup, SQLite
// reopening, process locking, HTTP routing, and runtime hydration together.
func TestProcessRestartRecovery(t *testing.T) {
	binary := buildProcessTestBinary(t)

	t.Run("reconcile_unknown_tool_execution", func(t *testing.T) {
		llmServer := newProcessLLMServer(t, processLLMReconcile)
		defer llmServer.Close()

		root, baseURL := startProcessNode(t, binary, llmServer.URL)
		first := newNodeProcess(t, binary, root)
		waitNodeHealthy(t, first, baseURL)
		completeProcessOnboarding(t, baseURL, llmServer.URL)

		agentID := createProcessAgent(t, baseURL, []string{"bash"})
		setProcessToolPolicy(t, baseURL, agentID, "bash_run", "never")
		postProcessMessage(t, baseURL, agentID, "run a long process")

		started := waitProcessEvent(t, baseURL, agentID, "tool.execution.started", 12*time.Second)
		if started.ToolExecutionID == "" || started.TurnID == "" || started.StepID == "" {
			t.Fatalf("tool execution event missing identifiers: %+v", started)
		}
		first.Kill(t)

		second := newNodeProcess(t, binary, root)
		waitNodeHealthy(t, second, baseURL)
		postProcessJSON(t, http.MethodPost, baseURL+"/v1/agents/"+agentID+"/ensure", map[string]any{})
		unknown := waitProcessEvent(t, baseURL, agentID, "tool.execution.failed", 12*time.Second)
		if unknown.ToolExecutionID != started.ToolExecutionID {
			t.Fatalf("unknown execution id=%q, want %q", unknown.ToolExecutionID, started.ToolExecutionID)
		}
		if !timelinePayloadContains(unknown, "node_restart_unknown") {
			t.Fatalf("restart failure does not identify unknown side effect: %+v", unknown)
		}

		postProcessJSON(t, http.MethodPost, baseURL+"/v1/agents/"+agentID+"/turns/"+started.TurnID+
			"/steps/"+started.StepID+"/tool-executions/"+started.ToolExecutionID+"/reconcile",
			map[string]any{"status": "succeeded", "content": "recovered after process restart"})
		waitProcessEvent(t, baseURL, agentID, "turn.completed", 12*time.Second)
		second.Kill(t)

		if got := llmServer.Calls(); got != 2 {
			t.Fatalf("LLM calls=%d, want initial tool call plus post-reconcile continuation", got)
		}
	})

	t.Run("resume_pending_hitl", func(t *testing.T) {
		llmServer := newProcessLLMServer(t, processLLMResume)
		defer llmServer.Close()

		root, baseURL := startProcessNode(t, binary, llmServer.URL)
		first := newNodeProcess(t, binary, root)
		waitNodeHealthy(t, first, baseURL)
		completeProcessOnboarding(t, baseURL, llmServer.URL)

		agentID := createProcessAgent(t, baseURL, []string{"hitl"})
		postProcessMessage(t, baseURL, agentID, "ask me for the environment")
		requested := waitProcessEvent(t, baseURL, agentID, "interaction.requested", 12*time.Second)
		if requested.TurnID == "" || requested.StepID == "" {
			t.Fatalf("interaction event missing turn/step identifiers: %+v", requested)
		}
		first.Kill(t)

		second := newNodeProcess(t, binary, root)
		waitNodeHealthy(t, second, baseURL)
		postProcessJSON(t, http.MethodPost, baseURL+"/v1/messages", map[string]any{
			"agent_id":     agentID,
			"request_type": "resume",
			"resume_value": map[string]any{
				"type":         "user_information",
				"tool_call_id": "call-process-hitl",
				"answer":       "staging",
			},
		})
		waitProcessEvent(t, baseURL, agentID, "interaction.resolved", 12*time.Second)
		waitProcessEvent(t, baseURL, agentID, "turn.completed", 12*time.Second)
		second.Kill(t)

		if got := llmServer.Calls(); got != 2 {
			t.Fatalf("LLM calls=%d, want initial HITL request plus resumed continuation", got)
		}
	})

	t.Run("hydrate_terminal_message_after_all_stream_events_are_missed", func(t *testing.T) {
		llmServer := newProcessLLMServer(t, processLLMAuto)
		defer llmServer.Close()

		root, baseURL := startProcessNode(t, binary, llmServer.URL)
		first := newNodeProcess(t, binary, root)
		waitNodeHealthy(t, first, baseURL)
		completeProcessOnboarding(t, baseURL, llmServer.URL)

		agentID := createProcessAgent(t, baseURL, nil)
		postProcessMessage(t, baseURL, agentID, "reply with a short completion")

		// Poll only the hydrate API. No SSE subscriber is installed, so every
		// streaming and terminal notification is intentionally missed by the
		// client. The durable transcript must still reach a terminal view.
		beforeRestart := waitProcessHydrateReply(t, baseURL, agentID, "auto completed 1", 12*time.Second)
		if beforeRestart.ConversationRevision == 0 {
			t.Fatalf("completed hydrate did not expose a conversation revision: %+v", beforeRestart)
		}
		if !beforeRestart.TranscriptSynchronized || beforeRestart.TurnState.ResultCommitStatus == "failed" {
			t.Fatalf("terminal reply was visible before restart without a durable transcript commit: %+v", beforeRestart)
		}
		firstRevision := beforeRestart.ConversationRevision
		postProcessMessage(t, baseURL, agentID, "reply to a second independent turn")
		beforeRestart = waitProcessHydrateReply(t, baseURL, agentID, "auto completed 2", 12*time.Second)
		if beforeRestart.ConversationRevision <= firstRevision {
			t.Fatalf("second terminal message did not advance conversation revision: before=%d after=%d", firstRevision, beforeRestart.ConversationRevision)
		}
		if got := llmServer.Calls(); got != 2 {
			t.Fatalf("LLM calls before restart=%d, want 2", got)
		}

		first.Kill(t)
		assertProcessCheckpointDeltas(t, root, agentID, 2)
		second := newNodeProcess(t, binary, root)
		waitNodeHealthy(t, second, baseURL)

		afterRestart := getProcessHydrate(t, baseURL, agentID)
		if afterRestart.Active {
			t.Fatalf("reopened hydrate still reports active turn: %+v", afterRestart)
		}
		if !afterRestart.TranscriptSynchronized || afterRestart.TurnState.ResultCommitStatus == "failed" {
			t.Fatalf("reopened hydrate did not report a durable transcript commit: %+v", afterRestart)
		}
		if !processHydrateContainsAssistant(afterRestart, "auto completed 2") {
			t.Fatalf("reopened hydrate lost terminal assistant message (pre-restart hydrate: %+v): %+v", beforeRestart, afterRestart)
		}
		if afterRestart.ConversationRevision != beforeRestart.ConversationRevision {
			t.Fatalf("conversation revision changed across restart: before=%d after=%d", beforeRestart.ConversationRevision, afterRestart.ConversationRevision)
		}
		if !reflect.DeepEqual(afterRestart.Transcript, beforeRestart.Transcript) {
			t.Fatalf("reopened transcript differs from pre-restart hydrate:\nbefore=%+v\nafter=%+v", beforeRestart.Transcript, afterRestart.Transcript)
		}
		if got := llmServer.Calls(); got != 2 {
			t.Fatalf("LLM calls after restart=%d, want no replay", got)
		}
		second.Kill(t)
	})

	t.Run("manual_compression_preserves_hydrate_history_after_restart", func(t *testing.T) {
		llmServer := newProcessLLMServer(t, processLLMAuto)
		defer llmServer.Close()

		root, baseURL := startProcessNode(t, binary, llmServer.URL)
		first := newNodeProcess(t, binary, root)
		waitNodeHealthy(t, first, baseURL)
		completeProcessOnboarding(t, baseURL, llmServer.URL)

		agentID := createProcessAgent(t, baseURL, nil)
		postProcessMessage(t, baseURL, agentID, "first human turn with history to preserve")
		waitProcessHydrateReply(t, baseURL, agentID, "auto completed 1", 12*time.Second)
		postProcessMessage(t, baseURL, agentID, "second human turn after the first")
		beforeCompression := waitProcessHydrateReply(t, baseURL, agentID, "auto completed 2", 12*time.Second)
		if beforeCompression.ConversationRevision == 0 || len(beforeCompression.Transcript) < 4 {
			t.Fatalf("pre-compression hydrate is missing durable turns: %+v", beforeCompression)
		}

		compressResp, err := http.Post(baseURL+"/v1/agents/"+agentID+"/compress", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		var compressResult struct {
			Status string `json:"status"`
		}
		decodeErr := json.NewDecoder(compressResp.Body).Decode(&compressResult)
		compressResp.Body.Close()
		if compressResp.StatusCode != http.StatusOK || decodeErr != nil {
			t.Fatalf("compress status=%d decode=%v", compressResp.StatusCode, decodeErr)
		}
		if compressResult.Status != "applied" {
			t.Fatalf("manual compression status=%q, want applied", compressResult.Status)
		}
		afterCompression := getProcessHydrate(t, baseURL, agentID)
		if !processHydrateContainsAssistant(afterCompression, "auto completed 2") {
			t.Fatalf("post-compression hydrate lost the completed human turn: %+v", afterCompression)
		}
		callsAfterCompression := llmServer.Calls()
		if callsAfterCompression != 3 {
			t.Fatalf("LLM calls after two turns and compression=%d, want 3", callsAfterCompression)
		}

		first.Kill(t)
		second := newNodeProcess(t, binary, root)
		waitNodeHealthy(t, second, baseURL)
		afterRestart := getProcessHydrate(t, baseURL, agentID)
		if afterRestart.ConversationRevision != afterCompression.ConversationRevision {
			t.Fatalf("conversation revision changed after compressed restart: before=%d after=%d", afterCompression.ConversationRevision, afterRestart.ConversationRevision)
		}
		if !reflect.DeepEqual(afterRestart.Transcript, afterCompression.Transcript) {
			t.Fatalf("reopened hydrate transcript differs after compression:\nbefore=%+v\nafter=%+v", afterCompression.Transcript, afterRestart.Transcript)
		}
		if got := llmServer.Calls(); got != callsAfterCompression {
			t.Fatalf("LLM calls after reopen=%d, want unchanged %d", got, callsAfterCompression)
		}
		second.Kill(t)
	})

	t.Run("auto_default_trigger_fires_without_sse_and_reopens", func(t *testing.T) {
		llmServer := newProcessLLMServer(t, processLLMAuto)
		defer llmServer.Close()
		root, baseURL := startProcessNodeWithTriggers(t, binary, llmServer.URL, true)
		first := newNodeProcess(t, binary, root)
		waitNodeHealthy(t, first, baseURL)
		completeProcessOnboarding(t, baseURL, llmServer.URL)
		agentID := createProcessAutoAgent(t, baseURL)
		postProcessJSON(t, http.MethodPut, baseURL+"/v1/agents/"+agentID+"/auto-config", map[string]any{
			"expected_revision": 0, "responsibility": "process test", "wake_interval_seconds": 1,
			"dreaming_enabled": false, "dreaming_time": "03:00", "timezone": "UTC",
		})
		triggerID := "auto-default:" + agentID
		waitProcessTriggerFireCount(t, baseURL, triggerID, 1, 15*time.Second)
		hydrate := waitProcessHydrateReply(t, baseURL, agentID, "auto completed 1", 15*time.Second)
		if got := llmServer.Calls(); got != 1 {
			t.Fatalf("auto trigger model calls=%d want 1", got)
		}
		hydrate = getProcessHydrate(t, baseURL, agentID)
		if hydrate.Active || hydrate.QueuePending != 0 || hydrate.PendingHITL != nil {
			t.Fatalf("auto hydrate after completion=%+v", hydrate)
		}
		beforeRestartEpoch, beforeRestartWatermark := getProcessAgentWatermark(t, baseURL, agentID)
		if beforeRestartEpoch == "" || beforeRestartWatermark.ConversationRevision != hydrate.ConversationRevision || beforeRestartWatermark.LatestAgentSeq == 0 {
			t.Fatalf("completed Auto watermark=%+v epoch=%q hydrate=%+v", beforeRestartWatermark, beforeRestartEpoch, hydrate)
		}
		waitProcessTriggerSettled(t, baseURL, triggerID, 15*time.Second)
		// Move the enabled schedule into the future through the public Auto
		// config API before restarting, so the restart checks persistence rather
		// than the disabled path.
		postProcessJSON(t, http.MethodPut, baseURL+"/v1/agents/"+agentID+"/auto-config", map[string]any{
			"expected_revision": 1, "responsibility": "process test", "wake_interval_seconds": 60,
			"dreaming_enabled": false, "dreaming_time": "03:00", "timezone": "UTC",
		})
		beforeRestartTranscript := append([]map[string]any(nil), hydrate.Transcript...)

		first.Kill(t)
		second := newNodeProcess(t, binary, root)
		waitNodeHealthy(t, second, baseURL)
		afterRestartEpoch, afterRestartWatermark := getProcessAgentWatermark(t, baseURL, agentID)
		if afterRestartEpoch == "" || afterRestartEpoch == beforeRestartEpoch {
			t.Fatalf("stream epoch did not change across restart: before=%q after=%q", beforeRestartEpoch, afterRestartEpoch)
		}
		if afterRestartWatermark.ConversationRevision != beforeRestartWatermark.ConversationRevision {
			t.Fatalf("watermark revision changed across restart: before=%+v after=%+v", beforeRestartWatermark, afterRestartWatermark)
		}
		waitProcessTriggerFireCount(t, baseURL, triggerID, 1, 5*time.Second)
		trigger := getProcessTrigger(t, baseURL, triggerID)
		if trigger.Controller != "auto" || trigger.OwnerAgentID != agentID || !trigger.Enabled || trigger.FireCount != 1 || trigger.NextFireAt <= float64(time.Now().Unix()) || trigger.PendingDeliveryID != "" || trigger.RecoveryRequired {
			t.Fatalf("reopened default trigger=%+v", trigger)
		}
		if got := llmServer.Calls(); got != 1 {
			t.Fatalf("reopened auto trigger model calls=%d want 1", got)
		}
		hydrate = getProcessHydrate(t, baseURL, agentID)
		if hydrate.Active || hydrate.QueuePending != 0 || hydrate.PendingHITL != nil {
			t.Fatalf("auto hydrate after reopen=%+v", hydrate)
		}
		if !reflect.DeepEqual(hydrate.Transcript, beforeRestartTranscript) {
			t.Fatalf("Auto transcript changed or replayed after restart: before=%+v after=%+v", beforeRestartTranscript, hydrate.Transcript)
		}
		second.Kill(t)
	})
}

func assertProcessCheckpointDeltas(t *testing.T, root, agentID string, minimum int) {
	t.Helper()
	dbPath := filepath.Join(root, ".runtime", "memory", "sessions.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var deltas int
	if err := db.QueryRow(`SELECT COUNT(*) FROM context_checkpoints WHERE conversation_id=? AND checkpoint_kind='delta'`, agentID).Scan(&deltas); err != nil {
		t.Fatalf("inspect checkpoint deltas after process stop: %v", err)
	}
	if deltas < minimum {
		rows, queryErr := db.Query(`SELECT revision,checkpoint_kind,base_revision,lineage FROM context_checkpoints WHERE conversation_id=? ORDER BY revision`, agentID)
		if queryErr != nil {
			t.Fatalf("list checkpoint rows: %v", queryErr)
		}
		var details []string
		for rows.Next() {
			var revision, base int
			var kind, lineage string
			if scanErr := rows.Scan(&revision, &kind, &base, &lineage); scanErr != nil {
				t.Fatalf("scan checkpoint row: %v", scanErr)
			}
			details = append(details, fmt.Sprintf("r%d:%s(base=%d,lineage=%s)", revision, kind, base, lineage))
		}
		_ = rows.Close()
		t.Fatalf("process restart fixture persisted %d incremental checkpoints for agent %s, want at least %d; checkpoints=%v", deltas, agentID, minimum, details)
	}
}

type processLLMMode string

const (
	processLLMReconcile processLLMMode = "reconcile"
	processLLMResume    processLLMMode = "resume"
	processLLMAuto      processLLMMode = "auto"
)

type processLLMServer struct {
	*httptest.Server
	mu    sync.Mutex
	mode  processLLMMode
	calls int
}

func newProcessLLMServer(t *testing.T, mode processLLMMode) *processLLMServer {
	t.Helper()
	mock := &processLLMServer{mode: mode}
	mock.Server = httptest.NewServer(http.HandlerFunc(mock.handle))
	return mock
}

func (s *processLLMServer) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *processLLMServer) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	s.calls++
	call := s.calls
	mode := s.mode
	s.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	if mode == processLLMAuto {
		writeProcessSSE(w, map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": fmt.Sprintf("auto completed %d", call)}, "finish_reason": "stop"}}})
	} else if call == 1 {
		toolName := "ask_user_information"
		arguments := `{"question":"Which environment should be used?"}`
		if mode == processLLMReconcile {
			toolName = "bash_run"
			if runtime.GOOS == "windows" {
				arguments = `{"command":"Start-Sleep -Seconds 60","timeout_seconds":120,"shell_type":"powershell"}`
			} else {
				arguments = `{"command":"sleep 60","timeout_seconds":120,"shell_type":"bash"}`
			}
		}
		writeProcessSSE(w, map[string]any{
			"choices": []any{map[string]any{
				"delta": map[string]any{
					"tool_calls": []any{map[string]any{
						"index": 0,
						"id": func() string {
							if toolName == "bash_run" {
								return "call-process-bash"
							}
							return "call-process-hitl"
						}(),
						"type": "function",
						"function": map[string]any{
							"name":      toolName,
							"arguments": arguments,
						},
					}},
				},
				"finish_reason": nil,
			}},
		})
		writeProcessSSE(w, map[string]any{
			"choices": []any{map[string]any{
				"delta":         map[string]any{},
				"finish_reason": "tool_calls",
			}},
		})
	} else {
		writeProcessSSE(w, map[string]any{
			"choices": []any{map[string]any{
				"delta":         map[string]any{"content": "recovered continuation"},
				"finish_reason": "stop",
			}},
		})
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func writeProcessSSE(w http.ResponseWriter, payload map[string]any) {
	raw, _ := json.Marshal(payload)
	fmt.Fprintf(w, "data: %s\n\n", raw)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

type processTimelineEvent struct {
	EventType       string         `json:"event_type"`
	TurnID          string         `json:"turn_id"`
	StepID          string         `json:"step_id"`
	ToolCallID      string         `json:"tool_call_id"`
	ToolExecutionID string         `json:"tool_execution_id"`
	Payload         map[string]any `json:"payload"`
}

type processTimelineResponse struct {
	Events []processTimelineEvent `json:"events"`
}

func buildProcessTestBinary(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "dagents-node-test.exe")
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build dagents-node: %v\n%s", err, output)
	}
	return out
}

func startProcessNode(t *testing.T, binary, llmURL string) (string, string) {
	return startProcessNodeWithTriggers(t, binary, llmURL, false)
}

func startProcessNodeWithTriggers(t *testing.T, binary, llmURL string, enabled bool) (string, string) {
	t.Helper()
	root := t.TempDir()
	port := freeProcessPort(t)
	configPath := filepath.Join(root, "config.yaml")
	config := fmt.Sprintf(`node_id: process-e2e-node
listen:
  host: 127.0.0.1
  port: %d
local:
  endpoint: http://127.0.0.1:%d
llm:
  provider: openai
  base_url: %s
  model: process-e2e
  api_key_env: E2E_API_KEY
  mock: false
skills:
  enabled: false
triggers:
  enabled: %t
manage:
  enabled: false
log:
  level: error
`, port, port, llmURL, enabled)
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, fmt.Sprintf("http://127.0.0.1:%d", port)
}

func completeProcessOnboarding(t *testing.T, baseURL, llmURL string) {
	t.Helper()
	postProcessJSON(t, http.MethodPatch, baseURL+"/v1/settings", map[string]any{
		"agent": map[string]any{"name": "process-e2e-node", "description": "process restart test"},
		"user":  map[string]any{"preferred_name": "process-e2e-user"},
		"llm": map[string]any{
			"active": "default",
			"profiles": []map[string]any{{
				"id": "default", "provider": "openai", "base_url": llmURL,
				"model": "process-e2e", "api_key_env": "E2E_API_KEY", "mock": false,
			}},
		},
		"onboarding": map[string]any{"node_profile_completed": true},
	})
}

type nodeProcess struct {
	cmd    *exec.Cmd
	stdout bytes.Buffer
	stderr bytes.Buffer
}

func newNodeProcess(t *testing.T, binary, root string) *nodeProcess {
	t.Helper()
	configPath := filepath.Join(root, "config.yaml")
	proc := &nodeProcess{cmd: exec.Command(binary, "-config", configPath, "--log-level", "error")}
	proc.cmd.Dir = root
	proc.cmd.Env = append(os.Environ(), "E2E_API_KEY=process-e2e-key", "OPENAI_API_KEY=process-e2e-key")
	proc.cmd.Stdout = &proc.stdout
	proc.cmd.Stderr = &proc.stderr
	if err := proc.cmd.Start(); err != nil {
		t.Fatalf("start dagents-node: %v", err)
	}
	t.Cleanup(func() {
		if proc.cmd.Process != nil && proc.cmd.ProcessState == nil {
			proc.Kill(t)
		}
	})
	return proc
}

func (p *nodeProcess) Kill(t *testing.T) {
	t.Helper()
	if p == nil || p.cmd == nil || p.cmd.Process == nil || p.cmd.ProcessState != nil {
		return
	}
	_ = p.cmd.Process.Kill()
	if err := p.cmd.Wait(); err != nil {
		// Kill is the deliberate crash simulation; a non-zero exit is expected.
	}
}

func (p *nodeProcess) diagnostic() string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace("stdout:\n" + p.stdout.String() + "\nstderr:\n" + p.stderr.String())
}

func waitNodeHealthy(t *testing.T, proc *nodeProcess, baseURL string) {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/health")
		if err == nil {
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr == nil && resp.StatusCode == http.StatusOK {
				return
			}
			lastErr = fmt.Errorf("status=%d body=%s read=%v", resp.StatusCode, body, readErr)
		} else {
			lastErr = err
		}
		if proc != nil && proc.cmd.ProcessState != nil {
			t.Fatalf("dagents-node exited before health: %v\n%s\nlast health error: %v", proc.cmd.ProcessState, proc.diagnostic(), lastErr)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for node health: %v\n%s", lastErr, proc.diagnostic())
}

func freeProcessPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func createProcessAgent(t *testing.T, baseURL string, groups []string) string {
	t.Helper()
	resp := postProcessJSON(t, http.MethodPost, baseURL+"/v1/agents", map[string]any{
		"display_name": "process-e2e-agent",
		"defaults": map[string]any{
			"llm":   map[string]any{"max_tool_calls": 4},
			"tools": map[string]any{"enabled_groups": groups},
		},
	})
	var body struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(resp, &body); err != nil {
		t.Fatalf("decode create agent response: %v body=%s", err, resp)
	}
	if body.AgentID == "" {
		t.Fatalf("create agent returned empty id: %s", resp)
	}
	return body.AgentID
}

func createProcessAutoAgent(t *testing.T, baseURL string) string {
	t.Helper()
	resp := postProcessJSON(t, http.MethodPost, baseURL+"/v1/agents", map[string]any{
		"display_name": "process-e2e-auto",
		"agent_type":   "auto",
		"defaults":     map[string]any{"llm": map[string]any{"max_tool_calls": 4}, "tools": map[string]any{"enabled_groups": []any{}}},
	})
	var body struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(resp, &body); err != nil || body.AgentID == "" {
		t.Fatalf("decode auto agent response: %v body=%s", err, resp)
	}
	return body.AgentID
}

func setProcessToolPolicy(t *testing.T, baseURL, agentID, tool, mode string) {
	t.Helper()
	postProcessJSON(t, http.MethodPut, baseURL+"/v1/agents/"+agentID+"/policy/tools", map[string]any{
		"updates": []map[string]any{{"name": tool, "mode": mode}},
	})
}

func postProcessMessage(t *testing.T, baseURL, agentID, content string) {
	t.Helper()
	postProcessJSON(t, http.MethodPost, baseURL+"/v1/messages", map[string]any{
		"agent_id":           agentID,
		"request_type":       "message",
		"content":            content,
		"client_request_id":  fmt.Sprintf("process-test-%d", time.Now().UnixNano()),
		"conversation_epoch": uint64(1),
	})
}

type processHydrate struct {
	Active                 bool            `json:"active"`
	QueuePending           int             `json:"queue_pending"`
	PendingHITL            json.RawMessage `json:"pending_hitl"`
	ConversationRevision   uint64          `json:"conversation_revision"`
	TranscriptSynchronized bool            `json:"transcript_synchronized"`
	TurnState              struct {
		ResultCommitStatus string `json:"result_commit_status"`
	} `json:"turn_state"`
	Transcript []map[string]any `json:"transcript"`
}

type processConversationWatermark struct {
	AgentID              string `json:"agent_id"`
	ConversationID       string `json:"conversation_id"`
	ConversationRevision uint64 `json:"conversation_revision"`
	LatestAgentSeq       int    `json:"latest_agent_seq"`
	ResultCommitStatus   string `json:"result_commit_status"`
}

func getProcessAgentWatermark(t *testing.T, baseURL, agentID string) (string, processConversationWatermark) {
	t.Helper()
	resp, err := http.Get(baseURL + "/v1/agents/watermarks")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("watermarks status=%d body=%s", resp.StatusCode, body)
	}
	var body struct {
		StreamEpoch string                         `json:"stream_epoch"`
		Agents      []processConversationWatermark `json:"agents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	for _, watermark := range body.Agents {
		if watermark.AgentID == agentID && watermark.ConversationID == agentID {
			return body.StreamEpoch, watermark
		}
	}
	t.Fatalf("watermarks response missing conversation %s: %+v", agentID, body.Agents)
	return "", processConversationWatermark{}
}

func getProcessHydrate(t *testing.T, baseURL, agentID string) processHydrate {
	t.Helper()
	resp, err := http.Get(baseURL + "/v1/agents/" + agentID + "/hydrate")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("hydrate status=%d", resp.StatusCode)
	}
	var out processHydrate
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if string(out.PendingHITL) == "null" {
		out.PendingHITL = nil
	}
	return out
}

func processHydrateContainsAssistant(hydrate processHydrate, want string) bool {
	for _, entry := range hydrate.Transcript {
		if entry["kind"] == "assistant" && strings.Contains(fmt.Sprint(entry["text"]), want) {
			return true
		}
	}
	return false
}

func waitProcessHydrateReply(t *testing.T, baseURL, agentID, want string, timeout time.Duration) processHydrate {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var latest processHydrate
	for time.Now().Before(deadline) {
		latest = getProcessHydrate(t, baseURL, agentID)
		if !latest.Active && latest.TranscriptSynchronized && processHydrateContainsAssistant(latest, want) {
			return latest
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for terminal hydrate reply %q: %+v", want, latest)
	return processHydrate{}
}

func waitProcessTriggerFireCount(t *testing.T, baseURL, triggerID string, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/v1/triggers")
		if err == nil {
			var body struct {
				Triggers []struct {
					TriggerID string `json:"trigger_id"`
					FireCount int    `json:"fire_count"`
				} `json:"triggers"`
			}
			if json.NewDecoder(resp.Body).Decode(&body) == nil {
				resp.Body.Close()
				for _, trigger := range body.Triggers {
					if trigger.TriggerID == triggerID && trigger.FireCount >= want {
						return
					}
				}
			} else {
				resp.Body.Close()
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("trigger %s did not reach fire_count=%d", triggerID, want)
}

type processTrigger struct {
	TriggerID         string  `json:"trigger_id"`
	Controller        string  `json:"controller"`
	OwnerAgentID      string  `json:"owner_agent_id"`
	Enabled           bool    `json:"enabled"`
	FireCount         int     `json:"fire_count"`
	NextFireAt        float64 `json:"next_fire_at"`
	PendingDeliveryID string  `json:"pending_delivery_id"`
	RecoveryRequired  bool    `json:"recovery_required"`
}

func waitProcessTriggerSettled(t *testing.T, baseURL, triggerID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		trigger := getProcessTrigger(t, baseURL, triggerID)
		if trigger.FireCount == 1 && trigger.Enabled && trigger.PendingDeliveryID == "" && !trigger.RecoveryRequired {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("trigger %s did not settle after first delivery", triggerID)
}

func getProcessTrigger(t *testing.T, baseURL, triggerID string) processTrigger {
	t.Helper()
	resp, err := http.Get(baseURL + "/v1/triggers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Triggers []processTrigger `json:"triggers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	for _, trigger := range body.Triggers {
		if trigger.TriggerID == triggerID {
			return trigger
		}
	}
	t.Fatalf("trigger %s missing after reopen", triggerID)
	return processTrigger{}
}

func postProcessJSON(t *testing.T, method, url string, body map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("%s %s status=%d body=%s", method, url, resp.StatusCode, responseBody)
	}
	return responseBody
}

func waitProcessEvent(t *testing.T, baseURL, agentID, eventType string, timeout time.Duration) processTimelineEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	var last []processTimelineEvent
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/v1/agents/" + agentID + "/timeline?limit=1000")
		if err == nil {
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr == nil && resp.StatusCode == http.StatusOK {
				var timeline processTimelineResponse
				if decodeErr := json.Unmarshal(body, &timeline); decodeErr == nil {
					last = timeline.Events
					for _, event := range timeline.Events {
						if event.EventType == eventType {
							return event
						}
					}
				} else {
					lastErr = decodeErr
				}
			} else {
				lastErr = fmt.Errorf("status=%d body=%s read=%v", resp.StatusCode, body, readErr)
			}
		} else {
			lastErr = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s: err=%v events=%+v", eventType, lastErr, last)
	return processTimelineEvent{}
}

func timelinePayloadContains(event processTimelineEvent, needle string) bool {
	raw, _ := json.Marshal(event.Payload)
	return strings.Contains(string(raw), needle)
}
