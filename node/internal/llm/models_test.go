package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSuggestProviderFromBaseURL(t *testing.T) {
	cases := map[string]string{
		"https://api.deepseek.com":                          "deepseek",
		"https://dashscope.aliyuncs.com/compatible-mode/v1": "qwen",
		"https://api.openai.com/v1":                         "openai",
		"https://open.bigmodel.cn/api/paas/v4":              "glm",
		"https://api.minimaxi.com/v1":                       "minimax",
		"https://api.xiaomimimo.com/v1":                     "mimo",
		"http://127.0.0.1:8000/v1":                          "vllm",
		"https://example.com/v1":                            "",
	}
	for url, want := range cases {
		if got := SuggestProviderFromBaseURL(url); got != want {
			t.Fatalf("url=%q got=%q want=%q", url, got, want)
		}
	}
}

func TestProbeModels(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("auth=%q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "deepseek-chat"},
				{"id": "deepseek-reasoner"},
				{"id": "deepseek-chat"},
			},
		})
	}))
	defer ts.Close()

	got, err := ProbeModels(context.Background(), ProviderDeepSeek, ts.URL+"/v1", "sk-test")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Models) != 2 {
		t.Fatalf("models=%+v", got.Models)
	}
	if got.Models[0].ID != "deepseek-chat" || got.Models[1].ID != "deepseek-reasoner" {
		t.Fatalf("models=%+v", got.Models)
	}
}

func TestProbeModelsHTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"nope"}`, http.StatusUnauthorized)
	}))
	defer ts.Close()
	_, err := ProbeModels(context.Background(), ProviderOpenAI, ts.URL+"/v1", "bad")
	if err == nil {
		t.Fatal("expected error")
	}
}
