package shared

import "testing"

func TestStreamingToolCallParts_bashPartial(t *testing.T) {
	t.Parallel()
	summary, code := streamingToolCallParts("bash_run", `{"command": "ls -la`)
	if code != "ls -la" {
		t.Fatalf("code = %q", code)
	}
	if summary == "" {
		t.Fatal("expected summary")
	}
}

func TestStreamingToolCallParts_invalidJSON(t *testing.T) {
	t.Parallel()
	raw := `{"path": "/tmp`
	_, code := streamingToolCallParts("read_file", raw)
	if code != raw {
		t.Fatalf("code = %q", code)
	}
}
