package triggers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyMigrationBacksUpAndPersists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "triggers.json")
	legacy := `{"triggers":[{"trigger_id":"t","target_agent_id":"a","condition":{"interval_seconds":60},"enabled":false}],"history":[]}`
	if err := os.WriteFile(p, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(p, 20); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p + ".v1.bak")
	if string(b) != legacy {
		t.Fatal("legacy backup mismatch")
	}
	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), `"schema_version": 2`) {
		t.Fatal("migration was not persisted")
	}
	if err := os.WriteFile(p+".v1.bak", []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(p, 20); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p + ".v1.bak")
	if string(b) != "keep" {
		t.Fatal("backup was overwritten")
	}
}

func TestFutureSchemaRejected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "triggers.json")
	if err := os.WriteFile(p, []byte(`{"schema_version":99,"triggers":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(p, 20); err == nil {
		t.Fatal("future schema accepted")
	}
}

func TestValidateOwnersDisablesInvalidAndDoesNotGuessV2(t *testing.T) {
	st, _ := OpenStore(filepath.Join(t.TempDir(), "triggers.json"), 20)
	d := Definition{TriggerID: "bad", OwnerAgentID: "", TargetAgentID: "a", Controller: "goal", ControllerID: "wrong", ManagedGoalID: "goal"}
	if _, err := st.CreateTrigger(d); err != nil {
		t.Fatal(err)
	}
	if err := st.ValidateOwners(map[string]bool{"a": true}); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetTrigger("bad")
	if got.Enabled || !got.RecoveryRequired || got.OwnerAgentID != "" {
		t.Fatalf("got=%+v", got)
	}
}
