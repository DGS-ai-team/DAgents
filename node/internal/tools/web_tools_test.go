package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/webapi"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func hasWebTool(defs []ToolDef, name string) bool {
	for _, def := range defs {
		if def.Function.Name == name {
			return true
		}
	}
	return false
}

func TestWebSearchGlobalAndAgentGatesAndProviderPayload(t *testing.T) {
	var sawSearch bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/tools/search" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer local-test-key" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if _, ok := payload["call_purpose"]; ok {
			t.Errorf("call_purpose leaked to provider: %#v", payload)
		}
		if payload["localization"] != "203.0.113.1" {
			t.Errorf("localization = %#v", payload["localization"])
		}
		sawSearch = true
		_, _ = w.Write([]byte(`{"results":[{"title":"T","url":"https://example.com/a","snippet":"S","summary":"M","content":"body","authority":4,"rerank_score":0.7}]}`))
	}))
	defer server.Close()
	client, err := webapi.New(server.URL, "local-test-key")
	if err != nil {
		t.Fatal(err)
	}
	reg, err := NewRegistry(t.TempDir(), 5)
	if err != nil {
		t.Fatal(err)
	}
	// A constructed registry without the process-level feature gate does not expose or execute web tools.
	if hasWebTool(reg.Definitions(), "web_search") {
		t.Fatal("web_search visible while globally disabled")
	}
	if _, err := reg.Execute(context.Background(), "web_search", `{"query":"q"}`); err == nil {
		t.Fatal("web_search executed while globally disabled")
	}
	reg.SetWebAPIClient(client, true, false)
	if !hasWebTool(reg.Definitions(), "web_search") || hasWebTool(reg.Definitions(), "web_fetch") {
		t.Fatal("global per-tool gate did not filter definitions")
	}
	if err := reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"web_fetch"})); err != nil {
		t.Fatal(err)
	}
	if hasWebTool(reg.Definitions(), "web_search") {
		t.Fatal("Agent lacking web_search permission received schema")
	}
	if _, err := reg.Execute(context.Background(), "web_search", `{"query":"q"}`); err == nil {
		t.Fatal("Agent permission gate was bypassed")
	}
	if err := reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"web_search"})); err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(context.Background(), "web_search", `{"query":"  q  ","count":1,"localization":"203.0.113.1","call_purpose":"search docs"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !sawSearch || !strings.Contains(out, "https://example.com/a") || !strings.Contains(out, "外部网站") {
		t.Fatalf("unexpected output: %s", out)
	}
	if got := ClassifyResult("web_search", out, false).Status; got != ResultStatusSucceeded {
		t.Fatalf("search result status = %q, output: %s", got, out)
	}
}

func TestWebSearchRejectsInvalidLocalizationAndMissingResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer server.Close()
	client, err := webapi.New(server.URL, "key")
	if err != nil {
		t.Fatal(err)
	}
	reg, err := NewRegistry(t.TempDir(), 5)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetWebAPIClient(client, true, false)
	if _, err := reg.Execute(context.Background(), "web_search", `{"query":"q","localization":{"ip":"203.0.113.1"}}`); err == nil {
		t.Fatal("expected object localization to be rejected")
	}
	if _, err := reg.Execute(context.Background(), "web_search", `{"query":"q"}`); err == nil || !strings.Contains(err.Error(), "invalid search response") {
		t.Fatalf("expected missing results failure, got %v", err)
	}
}

func TestWebFetchRequiresURLAndContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"title":"T"}`)) }))
	defer server.Close()
	client, err := webapi.New(server.URL, "key")
	if err != nil {
		t.Fatal(err)
	}
	reg, err := NewRegistry(t.TempDir(), 5)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetWebAPIClient(client, false, true)
	for _, args := range []string{`{"url":"file:///etc/passwd"}`, `{"url":"https://user:pass@example.com/"}`} {
		if _, err := reg.Execute(context.Background(), "web_fetch", args); err == nil {
			t.Fatalf("expected URL validation error for %s", args)
		}
	}
	if _, err := reg.Execute(context.Background(), "web_fetch", `{"url":"https://example.com/"}`); err == nil || !strings.Contains(err.Error(), "invalid fetch response") {
		t.Fatalf("expected missing content failure, got %v", err)
	}
}

func TestWebFetchSuccessfulRoundTrip(t *testing.T) {
	const key = "fetch-local-test-key"
	var providerPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/tools/fetch" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer "+key {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&providerPayload); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"title":"Example page","content":"# Markdown\n\nbody mentions timeout and ERROR:失败"}`))
	}))
	defer server.Close()
	client, err := webapi.New(server.URL, key)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := NewRegistry(t.TempDir(), 5)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetWebAPIClient(client, false, true)
	out, err := reg.Execute(context.Background(), "web_fetch", `{"url":"https://example.test/page","call_purpose":"read source"}`)
	if err != nil {
		t.Fatal(err)
	}
	if providerPayload["url"] != "https://example.test/page" || providerPayload["call_purpose"] != nil {
		t.Fatalf("provider payload = %#v", providerPayload)
	}
	for _, fragment := range []string{"https://example.test/page", "# Markdown", "timeout", "ERROR:失败", "外部网站"} {
		if !strings.Contains(out, fragment) {
			t.Fatalf("result missing %q: %s", fragment, out)
		}
	}
	if got := ClassifyResult("web_fetch", out, false).Status; got != ResultStatusSucceeded {
		t.Fatalf("fetch result status = %q, output: %s", got, out)
	}
}
