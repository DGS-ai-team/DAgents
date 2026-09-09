package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompleteTextWithUsageFromHTTPAndAdapter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"摘要"}}],"usage":{"prompt_tokens":12,"completion_tokens":5,"total_tokens":17}}`)
	}))
	defer server.Close()
	client := newAdapterClient(NewOpenAIClient(OpenAIConfig{BaseURL: server.URL, Model: "model", APIKey: "key"}), openAIAdapter{}, nil)
	text, usage, err := client.CompleteTextWithUsage(context.Background(), CompleteRequest{UserPrompt: "内容"})
	if err != nil || text != "摘要" || usage == nil || usage.PromptTokens != 12 || usage.CompletionTokens != 5 || usage.TotalTokens != 17 {
		t.Fatalf("text=%q usage=%+v err=%v", text, usage, err)
	}
}

func TestCompleteRequestMaxOutputTokensIsSentAndZeroIsOmitted(t *testing.T) {
	seen := make(chan map[string]any, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		seen <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`)
	}))
	defer server.Close()
	client := NewOpenAIClient(OpenAIConfig{BaseURL: server.URL, Model: "model", APIKey: "key"})
	_, usage, err := client.CompleteTextWithUsage(context.Background(), CompleteRequest{UserPrompt: "x", MaxOutputTokens: 7})
	if err != nil || usage == nil || usage.TotalTokens != 5 {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}
	body := <-seen
	if got, ok := body["max_tokens"].(float64); !ok || got != 7 {
		t.Fatalf("max_tokens=%#v", body["max_tokens"])
	}
	if _, _, err := client.CompleteTextWithUsage(context.Background(), CompleteRequest{MaxOutputTokens: -1}); err == nil {
		t.Fatal("negative max output accepted")
	}
	_, _, err = client.CompleteTextWithUsage(context.Background(), CompleteRequest{UserPrompt: "x", MaxOutputTokens: 0})
	if err != nil {
		t.Fatal(err)
	}
	zeroBody := <-seen
	if _, exists := zeroBody["max_tokens"]; exists {
		t.Fatalf("zero max_tokens was sent: %#v", zeroBody["max_tokens"])
	}
	clamped := NewOpenAIClient(OpenAIConfig{BaseURL: server.URL, Model: "model", APIKey: "key", RequestExtra: map[string]any{"max_tokens": 99}})
	if _, _, err := clamped.CompleteTextWithUsage(context.Background(), CompleteRequest{MaxOutputTokens: 7}); err != nil {
		t.Fatal(err)
	}
	if got := <-seen; got["max_tokens"] != float64(7) {
		t.Fatalf("request extra bypassed limit: %#v", got)
	}
	modern := NewOpenAIClient(OpenAIConfig{BaseURL: server.URL, Model: "model", APIKey: "key", RequestExtra: map[string]any{"max_completion_tokens": 4, "max_tokens": 99}})
	if _, _, err := modern.CompleteTextWithUsage(context.Background(), CompleteRequest{MaxOutputTokens: 7}); err != nil {
		t.Fatal(err)
	}
	modernBody := <-seen
	if modernBody["max_completion_tokens"] != float64(4) {
		t.Fatalf("modern limit changed: %#v", modernBody)
	}
	if _, exists := modernBody["max_tokens"]; exists {
		t.Fatalf("mutually exclusive limits sent: %#v", modernBody)
	}
}

func TestCompleteTextWithUsagePreservesUnknownUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"摘要"}}]}`)
	}))
	defer server.Close()
	client := NewOpenAIClient(OpenAIConfig{BaseURL: server.URL, Model: "model", APIKey: "key"})
	_, usage, err := client.CompleteTextWithUsage(context.Background(), CompleteRequest{})
	if err != nil || usage != nil {
		t.Fatalf("usage=%+v err=%v; omitted usage must remain unknown", usage, err)
	}
}

func TestCompleteTextWithUsageKeepsUsageWhenChoicesAreEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":0,"total_tokens":12}}`)
	}))
	defer server.Close()
	client := NewOpenAIClient(OpenAIConfig{BaseURL: server.URL, Model: "model", APIKey: "key"})
	_, usage, err := client.CompleteTextWithUsage(context.Background(), CompleteRequest{})
	if err == nil || usage == nil || usage.TotalTokens != 12 {
		t.Fatalf("usage=%+v err=%v; usage must survive empty choices error", usage, err)
	}
}

func TestCompleteTextWithUsageTreatsEmptyUsageObjectAsUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"摘要"}}],"usage":{}}`)
	}))
	defer server.Close()
	client := NewOpenAIClient(OpenAIConfig{BaseURL: server.URL, Model: "model", APIKey: "key"})
	_, usage, err := client.CompleteTextWithUsage(context.Background(), CompleteRequest{})
	if err != nil || usage != nil {
		t.Fatalf("usage=%+v err=%v; empty usage object must remain unknown", usage, err)
	}
}

func TestCompleteTextWithUsageRecognizesTokenCountFields(t *testing.T) {
	tests := []struct {
		name  string
		usage string
		known bool
	}{
		{"whitespace object", `{ }`, false},
		{"unrelated field", `{"unrelated":1}`, false},
		{"null total", `{"total_tokens":null}`, false},
		{"negative total", `{"total_tokens":-1}`, false},
		{"explicit zero total", `{"total_tokens":0}`, true},
		{"prompt and completion", `{"prompt_tokens":2,"completion_tokens":0}`, true},
		{"only prompt", `{"prompt_tokens":2}`, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":"摘要"}}],"usage":%s}`, test.usage)
			}))
			defer server.Close()
			client := NewOpenAIClient(OpenAIConfig{BaseURL: server.URL, Model: "model", APIKey: "key"})
			_, usage, err := client.CompleteTextWithUsage(context.Background(), CompleteRequest{})
			if err != nil || (usage != nil) != test.known {
				t.Fatalf("usage=%+v err=%v, known=%v", usage, err, test.known)
			}
		})
	}
}

func TestCompleteTextWithUsageReturnsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "provider failed", http.StatusBadGateway)
	}))
	defer server.Close()
	client := NewOpenAIClient(OpenAIConfig{BaseURL: server.URL, Model: "model", APIKey: "key"})
	_, usage, err := client.CompleteTextWithUsage(context.Background(), CompleteRequest{})
	if err == nil || usage != nil {
		t.Fatalf("usage=%+v err=%v; failed request must not report zero usage", usage, err)
	}
}

func TestEnvAdapterCompleteTextWithUsagePassesThroughProviderUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"摘要"}}],"usage":{"total_tokens":9}}`)
	}))
	defer server.Close()
	t.Setenv("TEST_USAGE_KEY", "key")
	client := newEnvAdapterClient(server.URL, "TEST_USAGE_KEY", openAIAdapter{}, &RuntimeSettings{Provider: "openai", BaseURL: server.URL, APIKeyEnv: "TEST_USAGE_KEY", Model: "model"}, nil)
	_, usage, err := client.CompleteTextWithUsage(context.Background(), CompleteRequest{})
	if err != nil || usage == nil || usage.TotalTokens != 9 {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}
}
