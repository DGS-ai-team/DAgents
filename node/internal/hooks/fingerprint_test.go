package hooks

import "testing"

func TestToolArgsFingerprintStable(t *testing.T) {
	a := ToolArgsFingerprint("read_file", `{"path":"a.txt","call_purpose":"x"}`)
	b := ToolArgsFingerprint("read_file", `{"call_purpose":"y","path":"a.txt"}`)
	if a != b {
		t.Fatalf("fingerprints differ: %q vs %q", a, b)
	}
}

func TestToolExecutionLogRecordAndLast(t *testing.T) {
	log := &ToolExecutionLog{}
	if _, ok := log.LastRecord(); ok {
		t.Fatal("expected empty")
	}
	log.RecordSuccess("bash_run", "fp1", "call-1", "long result preview")
	rec, ok := log.LastRecord()
	if !ok || rec.ToolCallID != "call-1" || rec.ArgsFingerprint != "fp1" {
		t.Fatalf("record = %+v ok=%v", rec, ok)
	}
}
