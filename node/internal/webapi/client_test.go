package webapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchEndpointAuthAndPrefixAndRedactedError(t *testing.T) {
	const key = "secret-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxy/v1/tools/search" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("unexpected query: %q", r.URL.RawQuery)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+key {
			t.Errorf("authorization = %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["query"] != "hello" || payload["call_purpose"] != nil {
			t.Errorf("payload = %#v", payload)
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid credential ` + key + `"}`))
	}))
	defer server.Close()
	client, err := New(server.URL+"/proxy/v1", key)
	if err != nil {
		t.Fatal(err)
	}
	err = client.Search(context.Background(), map[string]any{"query": "hello"}, &struct{}{})
	if err == nil || strings.Contains(err.Error(), key) {
		t.Fatalf("expected redacted provider error, got %v", err)
	}
}

func TestEndpointAvoidsDuplicateV1AndPreservesProxyPrefix(t *testing.T) {
	for _, tc := range []struct{ base, want string }{
		{"https://api.deepseek.com", "https://api.deepseek.com/v1/tools/fetch"},
		{"https://proxy.example/custom/v1/", "https://proxy.example/custom/v1/tools/fetch"},
		{"https://proxy.example/custom", "https://proxy.example/custom/v1/tools/fetch"},
	} {
		c, err := New(tc.base, "key")
		if err != nil {
			t.Fatal(err)
		}
		got, err := c.endpoint("tools/fetch")
		if err != nil || got != tc.want {
			t.Fatalf("endpoint(%q) = %q, %v; want %q", tc.base, got, err, tc.want)
		}
	}
}

func TestCrossOriginRedirectDoesNotForwardAuthorization(t *testing.T) {
	var leaked bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("Authorization") != ""
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/v1/tools/search", http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	c, err := New(source.URL, "key")
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Search(context.Background(), map[string]string{"query": "q"}, &struct{}{}); err == nil {
		t.Fatal("expected cross-origin redirect error")
	}
	if leaked {
		t.Fatal("authorization header leaked to redirected host")
	}
}

func TestRetryBoundIsLimitedToTransientStatuses(t *testing.T) {
	for _, tc := range []struct{ status, wantCalls int }{{http.StatusBadRequest, 1}, {http.StatusRequestTimeout, 3}, {http.StatusTooManyRequests, 3}, {http.StatusServiceUnavailable, 3}} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"message":"upstream error"}`))
			}))
			defer server.Close()
			client, err := New(server.URL, "test-key")
			if err != nil {
				t.Fatal(err)
			}
			_ = client.Search(context.Background(), map[string]string{"query": "q"}, &struct{}{})
			if calls != tc.wantCalls {
				t.Fatalf("requests=%d, want %d", calls, tc.wantCalls)
			}
		})
	}
}

func TestSearchHonorsContextCancellation(t *testing.T) {
	release := make(chan struct{})
	arrived := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(arrived)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	c, err := New(server.URL, "key")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- c.Search(ctx, map[string]string{"query": "q"}, &struct{}{}) }()
	<-arrived
	cancel()
	err = <-result
	close(release)
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("expected cancellation, got %v", err)
	}
}
