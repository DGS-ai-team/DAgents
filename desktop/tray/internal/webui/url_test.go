package webui

import "testing"

func TestAgentURL(t *testing.T) {
	got := AgentURL("http://127.0.0.1:18765", "agt-abc")
	want := "http://127.0.0.1:18765/ui/agents/agt-abc"
	if got != want {
		t.Fatalf("url = %q want %q", got, want)
	}
}

func TestConsoleURL(t *testing.T) {
	got := ConsoleURL("http://127.0.0.1:18765/")
	if got != "http://127.0.0.1:18765/ui/" {
		t.Fatalf("url = %q", got)
	}
}

func TestSettingsAboutURL(t *testing.T) {
	got := SettingsAboutURL("http://127.0.0.1:18765")
	want := "http://127.0.0.1:18765/ui/settings/about"
	if got != want {
		t.Fatalf("url = %q want %q", got, want)
	}
}

func TestAgentURLPathEscape(t *testing.T) {
	got := AgentURL("http://127.0.0.1:18765", "a/b")
	want := "http://127.0.0.1:18765/ui/agents/a%2Fb"
	if got != want {
		t.Fatalf("url = %q want %q", got, want)
	}
}
