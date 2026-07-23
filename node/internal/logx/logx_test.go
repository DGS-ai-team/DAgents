package logx

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in   string
		want slog.Level
		ok   bool
	}{
		{"debug", slog.LevelDebug, true},
		{"INFO", slog.LevelInfo, true},
		{"warn", slog.LevelWarn, true},
		{"error", slog.LevelError, true},
		{"", slog.LevelInfo, false},
		{"verbose", slog.LevelInfo, false},
	}
	for _, tc := range cases {
		got, ok := ParseLevel(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("ParseLevel(%q) = (%v, %v), want (%v, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestNewSplitLoggerRoutesLevels(t *testing.T) {
	var full, errBuf bytes.Buffer
	logger := NewSplitLogger(&full, &errBuf, slog.LevelInfo)
	logger.Info("hello info")
	logger.Warn("hello warn")
	logger.Error("hello error")

	fullText := full.String()
	errText := errBuf.String()
	if !strings.Contains(fullText, "hello info") || !strings.Contains(fullText, "hello warn") || !strings.Contains(fullText, "hello error") {
		t.Fatalf("full log missing entries: %q", fullText)
	}
	if strings.Contains(errText, "hello info") || strings.Contains(errText, "hello warn") {
		t.Fatalf("err log should not contain info/warn: %q", errText)
	}
	if !strings.Contains(errText, "hello error") {
		t.Fatalf("err log missing error: %q", errText)
	}
}
