package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
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

		agentID := createProcessAgent(t, baseURL, []string{"bash"})
		setProcessToolPolicy(t, baseURL, agentID, "bash_run", "allow_auto")
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
}

type processLLMMode string

const (
	processLLMReconcile processLLMMode = "reconcile"
	processLLMResume    processLLMMode = "resume"
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
	if call == 1 {
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
	t.Helper()
	root := t.TempDir()
	port := freeProcessPort(t)
	configPath := filepath.Join(root, "config.yaml")
	config := fmt.Sprintf(`node_id: process-e2e-node
onboarding:
  node_profile_completed: true
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
  enabled: false
manage:
  enabled: false
log:
  level: error
`, port, port, llmURL)
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, fmt.Sprintf("http://127.0.0.1:%d", port)
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
			"llm":   map[string]any{"max_tool_loops": 4},
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

func setProcessToolPolicy(t *testing.T, baseURL, agentID, tool, decision string) {
	t.Helper()
	postProcessJSON(t, http.MethodPut, baseURL+"/v1/agents/"+agentID+"/policy/tools", map[string]any{
		"updates": []map[string]any{{"name": tool, "decision": decision}},
	})
}

func postProcessMessage(t *testing.T, baseURL, agentID, content string) {
	t.Helper()
	postProcessJSON(t, http.MethodPost, baseURL+"/v1/messages", map[string]any{
		"agent_id":     agentID,
		"request_type": "message",
		"content":      content,
	})
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
