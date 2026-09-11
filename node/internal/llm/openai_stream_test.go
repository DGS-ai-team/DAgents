package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func streamToolChunk(t *testing.T, arguments string, first bool, finishReason string) string {
	t.Helper()
	function := map[string]any{"arguments": arguments}
	call := map[string]any{"index": 0, "function": function}
	if first {
		call["id"] = "call-1"
		call["type"] = "function"
		function["name"] = "bash_run"
	}
	choice := map[string]any{"delta": map[string]any{"tool_calls": []any{call}}}
	if finishReason != "" {
		choice["finish_reason"] = finishReason
	}
	raw, err := json.Marshal(map[string]any{"choices": []any{choice}})
	if err != nil {
		t.Fatal(err)
	}
	return "data: " + string(raw) + "\n\n"
}

func newSSETestServer(t *testing.T, payload string) (*httptest.Server, *int32) {
	t.Helper()
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("test server does not support streaming")
			return
		}
		_, _ = fmt.Fprint(w, payload)
		flusher.Flush()
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func TestOpenAIStreamChatRejectsIncompleteProviderToolCall(t *testing.T) {
	server, requests := newSSETestServer(t,
		streamToolChunk(t, `{"command":"echo hi"`, true, "tool_calls")+"data: [DONE]\n\n")
	client := NewOpenAIClient(OpenAIConfig{BaseURL: server.URL, Model: "test-model", APIKey: "test-key"})
	result, err := client.StreamChat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "run"}}}, StreamHandler{})
	if err == nil || !errors.Is(err, ErrInvalidMessageHistory) {
		t.Fatalf("error=%v, result=%+v; want typed invalid history", err, result)
	}
	var validationErr *HistoryValidationError
	if !errors.As(err, &validationErr) || len(validationErr.Violations) != 1 || validationErr.Violations[0].Code != "assistant_tool_call_invalid_arguments_json" {
		t.Fatalf("validation error=%#v", err)
	}
	if atomic.LoadInt32(requests) != 1 {
		t.Fatalf("provider request count=%d, want 1", atomic.LoadInt32(requests))
	}
}

func TestOpenAIStreamChatRejectsInvalidDirectAPIMessagesBeforeNetwork(t *testing.T) {
	server, requests := newSSETestServer(t, "data: [DONE]\n\n")
	client := NewOpenAIClient(OpenAIConfig{BaseURL: server.URL, Model: "test-model", APIKey: "test-key"})
	result, err := client.StreamChat(context.Background(), ChatRequest{APIMessages: []map[string]any{
		{"role": "user", "content": "run"},
		{"role": "assistant", "tool_calls": []map[string]any{{
			"id": "call-1", "type": "function", "function": map[string]any{
				"name": "bash_run", "arguments": `{"command":"echo hi"`,
			},
		}}},
	}}, StreamHandler{})
	if err == nil || !errors.Is(err, ErrInvalidMessageHistory) {
		t.Fatalf("error=%v, result=%+v; want typed invalid history", err, result)
	}
	if atomic.LoadInt32(requests) != 0 {
		t.Fatalf("provider request count=%d, want 0", atomic.LoadInt32(requests))
	}
}

func TestOpenAIStreamChatAcceptsCompleteProviderToolCall(t *testing.T) {
	server, requests := newSSETestServer(t,
		streamToolChunk(t, `{"command":"`, true, "")+
			streamToolChunk(t, `echo hi"}`, false, "tool_calls")+
			"data: [DONE]\n\n")
	client := NewOpenAIClient(OpenAIConfig{BaseURL: server.URL, Model: "test-model", APIKey: "test-key"})
	result, err := client.StreamChat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "run"}}}, StreamHandler{})
	if err != nil {
		t.Fatalf("StreamChat error=%v", err)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Function.Arguments != `{"command":"echo hi"}` {
		t.Fatalf("result=%+v", result)
	}
	if atomic.LoadInt32(requests) != 1 {
		t.Fatalf("provider request count=%d, want 1", atomic.LoadInt32(requests))
	}
}

func TestOpenAIStreamChatRejectsUnexpectedEOFWithoutCompletionMarker(t *testing.T) {
	server, requests := newSSETestServer(t, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
	client := NewOpenAIClient(OpenAIConfig{BaseURL: server.URL, Model: "test-model", APIKey: "test-key"})
	result, err := client.StreamChat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "answer"}}}, StreamHandler{})
	if err == nil || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("error=%v, result=%+v; want unexpected EOF", err, result)
	}
	if atomic.LoadInt32(requests) != 1 {
		t.Fatalf("provider request count=%d, want 1", atomic.LoadInt32(requests))
	}
}

func TestOpenAIStreamChatCancellationReturnsDraftWithoutValidation(t *testing.T) {
	started := make(chan struct{})
	draftPayload := streamToolChunk(t, `{"command":"echo hi"`, true, "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = fmt.Fprint(w, draftPayload)
		flusher.Flush()
		close(started)
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)
	client := NewOpenAIClient(OpenAIConfig{BaseURL: server.URL, Model: "test-model", APIKey: "test-key"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	draftSeen := make(chan struct{})
	type response struct {
		result ChatResult
		err    error
	}
	done := make(chan response, 1)
	go func() {
		result, err := client.StreamChat(ctx, ChatRequest{Messages: []Message{{Role: "user", Content: "run"}}}, StreamHandler{
			OnToolCallDelta: func(calls []ToolCall) {
				if len(calls) > 0 {
					close(draftSeen)
				}
			},
		})
		done <- response{result: result, err: err}
	}()
	<-started
	<-draftSeen
	cancel()
	got := <-done
	if !errors.Is(got.err, context.Canceled) {
		t.Fatalf("error=%v, result=%+v; want context.Canceled", got.err, got.result)
	}
	if len(got.result.ToolCalls) != 1 || got.result.ToolCalls[0].Function.Arguments != `{"command":"echo hi"` {
		t.Fatalf("draft result=%+v", got.result)
	}
}
