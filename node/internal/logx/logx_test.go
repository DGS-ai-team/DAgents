package logx

import (
	"log/slog"
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
