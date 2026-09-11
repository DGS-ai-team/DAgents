package tools

import (
	"context"
	"strings"
	"testing"
)

func TestAutoIdleIsOnlyVisibleAndExecutableForTrustedSystemActivation(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	for _, def := range reg.Definitions() {
		if def.Function.Name == "auto_idle" {
			t.Fatal("auto_idle leaked into ordinary definitions")
		}
	}
	trusted := WithTrustedAutoIdleActivation(context.Background(), "agent-1", "auto-default", "delivery-1")
	seen := false
	for _, def := range reg.DefinitionsForContext(trusted) {
		if def.Function.Name == "auto_idle" {
			seen = true
		}
	}
	if !seen {
		t.Fatal("trusted activation did not expose auto_idle")
	}
	if _, err := reg.Execute(context.Background(), "auto_idle", `{}`); err == nil {
		t.Fatal("untrusted auto_idle executed")
	}
	got, err := reg.Execute(trusted, "auto_idle", `{}`)
	if err != nil || !strings.Contains(got, `"no_work":true`) {
		t.Fatalf("trusted auto_idle: result=%q err=%v", got, err)
	}
	if _, err := reg.Execute(trusted, "auto_idle", `{"unexpected":true}`); err == nil {
		t.Fatal("auto_idle accepted arguments")
	}
}
