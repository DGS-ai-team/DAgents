package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/manage"
)

func TestHandleAgentUpdateWindowsDelegate(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only delegate behavior")
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
	var got manage.UpdateStatus
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Delegate != "shell" {
		t.Fatalf("delegate = %q", got.Delegate)
	}
	if !got.Deprecated {
		t.Fatal("expected deprecated")
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
