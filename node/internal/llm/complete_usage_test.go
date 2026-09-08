package llm

import (
	"context"
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
