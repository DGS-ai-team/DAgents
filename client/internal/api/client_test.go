package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateLocalNodeEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{name: "localhost custom port", url: "http://localhost:22117"},
		{name: "ipv4 loopback", url: "http://127.0.0.1:18765"},
		{name: "ipv4 loopback range", url: "http://127.42.0.2:9000"},
		{name: "ipv6 loopback", url: "http://[::1]:18765"},
		{name: "remote ipv4", url: "http://192.168.1.8:18765", wantErr: true},
		{name: "remote hostname", url: "http://node.example:18765", wantErr: true},
		{name: "https", url: "https://localhost:18765", wantErr: true},
		{name: "path", url: "http://localhost:18765/api", wantErr: true},
		{name: "userinfo", url: "http://user@localhost:18765", wantErr: true},
		{name: "invalid port", url: "http://localhost:70000", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateLocalNodeEndpoint(tt.url)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateLocalNodeEndpoint(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestGetAgentUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/node/update" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"current_version":"1.0.0","latest_version":"1.1.0","upgrade_available":true}`))
	}))
	defer server.Close()

	status, err := New(server.URL, server.Client()).GetAgentUpdate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.CurrentVersion != "1.0.0" || status.LatestVersion != "1.1.0" || !status.UpgradeAvailable {
		t.Fatalf("update status = %+v", status)
	}
}

func TestGetNodeUpdatePackageDisablesRedirectsAndExtendsTimeout(t *testing.T) {
	redirected := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected = true
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/node/update/package" || r.Header.Get("Accept") != "application/octet-stream" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	httpClient := &http.Client{}
	resp, err := New(server.URL, httpClient).GetNodeUpdatePackage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTemporaryRedirect || redirected {
		t.Fatalf("status = %d, target reached = %v", resp.StatusCode, redirected)
	}
	if httpClient.Timeout != 0 {
		t.Fatalf("caller client timeout was mutated: %s", httpClient.Timeout)
	}
}
