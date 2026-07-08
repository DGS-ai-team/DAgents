package webui

import "testing"

func TestSessionURL(t *testing.T) {
	got := SessionURL("http://127.0.0.1:18765", "sess-abc")
	want := "http://127.0.0.1:18765/ui/?session=sess-abc"
	if got != want {
		t.Fatalf("url = %q", got)
	}
}

func TestConsoleURL(t *testing.T) {
	got := ConsoleURL("http://127.0.0.1:18765/")
	if got != "http://127.0.0.1:18765/ui/" {
		t.Fatalf("url = %q", got)
	}
}
