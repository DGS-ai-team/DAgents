package llm

import "testing"

func TestUserMessage_setsName(t *testing.T) {
	m := UserMessage("hi", UserNameTrigger)
	if m.Role != "user" || m.Content != "hi" || m.Name != UserNameTrigger {
		t.Fatalf("message = %+v", m)
	}
}

func TestUserMessage_emptyNameOmitted(t *testing.T) {
	m := UserMessage("hi", "")
	if m.Name != "" {
		t.Fatalf("name = %q", m.Name)
	}
}

func TestNormalizeUserMessageName(t *testing.T) {
	if got := NormalizeUserMessageName(""); got != UserNameHuman {
		t.Fatalf("default = %q", got)
	}
	if got := NormalizeUserMessageName("custom"); got != "custom" {
		t.Fatalf("custom = %q", got)
	}
}
