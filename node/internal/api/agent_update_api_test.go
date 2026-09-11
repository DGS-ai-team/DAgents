package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

func TestHandleAgentUpdateWindowsReturnsCurrentStatus(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only Node update response")
	}
	srv := NewServer(testConfig(t), nil, WithSkipStore())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/agent/update")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["delegate"]; ok {
		t.Fatalf("deprecated delegate field must be absent: %#v", got)
	}
	if _, ok := got["deprecated"]; ok {
		t.Fatalf("deprecated field must be absent: %#v", got)
	}
}

func TestHandleAgentUpdateLinuxUsesChecker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux path only")
	}
	cfg := testConfig(t)
	cfg.Manage.Enabled = true
	cfg.Manage.URL = "http://manage.invalid"
	srv := NewServer(cfg, nil, WithSkipStore())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/agent/update")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}
