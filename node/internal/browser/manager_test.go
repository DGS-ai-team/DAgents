package browser

import (
	"context"
	"testing"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestManagerStartStopWithMockDriver(t *testing.T) {
	mock := &MockDriver{
		Handler: func(_ context.Context, req Request) (Response, error) {
			switch req.Op {
			case "start":
				return Response{OK: true, URL: "about:blank", Title: ""}, nil
			case "stop":
				return Response{OK: true}, nil
			default:
				return Response{OK: false, Error: "unexpected " + req.Op}, nil
			}
		},
	}
	enabled := true
	m := &Manager{
		cfg:      testBrowserConfig(enabled),
		driver:   mock,
		sessions: make(map[string]struct{}),
	}
	if !m.Enabled() {
		t.Fatal("expected enabled manager")
	}
	out, err := m.Start(context.Background(), "sess-1", nil, 1280, 720)
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK {
		t.Fatalf("start = %+v", out)
	}
	out, err = m.Stop(context.Background(), "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK {
		t.Fatalf("stop = %+v", out)
	}
	if !mock.Closed {
		_ = m.Close()
	}
	if !mock.Closed {
		t.Fatal("expected mock driver closed")
	}
}

func TestValidateNavigateURL(t *testing.T) {
	m := &Manager{cfg: testBrowserConfig(true)}
	if err := m.ValidateNavigateURL("https://example.com/path"); err != nil {
		t.Fatal(err)
	}
	if err := m.ValidateNavigateURL("file:///etc/passwd"); err == nil {
		t.Fatal("expected scheme error")
	}
}

func TestSessionScreenshotPath(t *testing.T) {
	abs, rel, err := SessionScreenshotPath(t.TempDir(), "browser", "sess/a", "shot 1")
	if err != nil {
		t.Fatal(err)
	}
	if rel != "browser/sess-a/shot-1.png" {
		t.Fatalf("rel = %q", rel)
	}
	if abs == "" {
		t.Fatal("empty abs")
	}
}

func testBrowserConfig(enabled bool) config.BrowserConfig {
	return config.BrowserConfig{
		Enabled:           &enabled,
		DefaultTimeoutMS:  5000,
		OutputDir:         "browser",
		MaxSessions:       2,
		AllowedURLSchemes: []string{"https", "http"},
	}
}
