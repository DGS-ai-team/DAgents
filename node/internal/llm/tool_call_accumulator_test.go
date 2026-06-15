package llm

import "testing"

func TestToolCallAccumulator_snapshotAndFinalize(t *testing.T) {
	t.Parallel()

	acc := newToolCallAccumulator()
	acc.add(streamToolCallDelta{Index: 0, ID: "call-1", Type: "function", Function: struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}{Name: "bash_run"}})
	acc.add(streamToolCallDelta{Index: 0, Function: struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}{Arguments: `{"command":"ls"}`}})

	snap := acc.snapshot()
	if len(snap) != 1 || snap[0].Function.Name != "bash_run" || snap[0].Function.Arguments != `{"command":"ls"}` {
		t.Fatalf("snapshot = %+v", snap)
	}
	final := acc.finalize()
	if len(final) != 1 || final[0].ID != "call-1" {
		t.Fatalf("finalize = %+v", final)
	}
}
