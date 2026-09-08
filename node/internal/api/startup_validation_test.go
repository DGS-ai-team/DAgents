package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestStartupValidationFailureBlocksHandlerAndListen(t *testing.T) {
	s := NewServer(testConfig(t), nil, WithSkipStore())
	t.Cleanup(s.Close)
	s.startupErr = errors.New("validation failed")
	r := httptest.NewRecorder()
	s.Handler().ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/v1/triggers/test/fire", nil))
	if r.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", r.Code)
	}
	m := httptest.NewRecorder()
	s.Handler().ServeHTTP(m, httptest.NewRequest(http.MethodPost, "/v1/sessions/test/messages", nil))
	if m.Code != http.StatusServiceUnavailable {
		t.Fatalf("message status=%d", m.Code)
	}
	if err := s.ListenAndServe(context.Background()); err == nil {
		t.Fatal("startup error was not returned")
	}
}

func TestFutureSchemaStartupRejected(t *testing.T) {
	cases := []struct{ name, file, body string }{
		{"triggers", "triggers.json", `{"schema_version":99,"triggers":[]}`},
		{"goals", "goals.json", `{"schema_version":99,"goals":{},"runs":{}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig(t)
			path := filepath.Join(cfg.RuntimeDir(), tc.file)
			if tc.name == "triggers" {
				path = filepath.Join(cfg.RuntimeDir(), "triggers", "triggers.json")
			}
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			s := NewServer(cfg, nil)
			t.Cleanup(s.Close)
			if s.startupErr == nil {
				t.Fatal("future schema did not set startupErr")
			}
			if s.triggerSched != nil {
				t.Fatal("scheduler started after future schema rejection")
			}
			r := httptest.NewRecorder()
			s.Handler().ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/v1/triggers/test/fire", nil))
			if r.Code != http.StatusServiceUnavailable {
				t.Fatalf("status=%d", r.Code)
			}
		})
	}
}
