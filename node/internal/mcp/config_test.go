package mcp

import "testing"

func TestConfigTextRoundTrip(t *testing.T) {
	text := `{
  "mcpServers": {
    "tencent-docs": {
      "type": "streamable-http",
      "url": "https://docs.qq.com/openapi/mcp",
      "headers": {"Authorization": "${TENCENT_DOC_KEY}"}
    },
    "local": {
      "command": "npx",
      "args": ["-y", "example-mcp"],
      "env": {"API_KEY": "${OPENAI_API_KEY}"},
      "enabled_tools": ["echo"]
    }
  }
}`
	configs, err := ParseConfigText(text, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 2 || configs[0].ID != "local" || configs[1].ID != "tencent-docs" {
		t.Fatalf("configs = %+v", configs)
	}
	if configs[1].HeaderRefs["Authorization"] != "TENCENT_DOC_KEY" {
		t.Fatalf("header refs = %+v", configs[1].HeaderRefs)
	}
	rendered, err := FormatConfigText(configs)
	if err != nil {
		t.Fatal(err)
	}
	if want := "${TENCENT_DOC_KEY}"; !contains(rendered, want) {
		t.Fatalf("rendered config does not contain %q: %s", want, rendered)
	}
}

func TestConfigTextRetainsExistingAllowlistWhenOmitted(t *testing.T) {
	configs, err := ParseConfigText(`{"mcpServers":{"local":{"command":"node"}}}`, []ServerConfig{{ID: "local", Command: "old", EnabledTools: []string{"echo"}, Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 1 || len(configs[0].EnabledTools) != 1 || configs[0].EnabledTools[0] != "echo" {
		t.Fatalf("configs = %+v", configs)
	}
}

func TestConfigTextSupportsLiteralCredentials(t *testing.T) {
	configs, err := ParseConfigText(`{"mcpServers":{"remote":{"type":"streamable-http","url":"https://example.com/mcp","headers":{"Authorization":"Bearer plaintext"}}}}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 1 || configs[0].HeaderValues["Authorization"] != "Bearer plaintext" {
		t.Fatalf("configs = %+v", configs)
	}
	rendered, err := FormatConfigText(configs)
	if err != nil || !contains(rendered, "Bearer plaintext") {
		t.Fatalf("rendered=%s err=%v", rendered, err)
	}
}

func contains(text, value string) bool {
	for i := 0; i+len(value) <= len(text); i++ {
		if text[i:i+len(value)] == value {
			return true
		}
	}
	return false
}
