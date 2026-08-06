package workgroup

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestCancelInterruptsRunningExecutor(t *testing.T) {
	h := &CommandHandler{
		Journal:  NewMemoryJournal(),
		Bindings: NewMemoryBindingStore(),
	}
	started := make(chan struct{})
	var once sync.Once
	h.Executor = func(ctx context.Context, cmd ToolCommand) (string, error) {
		once.Do(func() { close(started) })
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(5 * time.Second):
			return `{"ok":true}`, nil
		}
	}
	binding := WorkerBinding{
		MemberID:       "mb_01h00000000000000000000002",
		WorkgroupID:    "wg_01h00000000000000000000001",
		ToolAllowNames: []string{"read_file"},
	}
	cmd := ToolCommand{
		CommandID:       "cmd_01h00000000000000000000099",
		WorkgroupID:     binding.WorkgroupID,
		MemberID:        binding.MemberID,
		ToolName:        "read_file",
		ArgumentsJSON:   `{"path":"README"}`,
		PayloadHash:     "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SideEffectClass: "fs_read",
	}

	var acceptRes *AcceptResult
	var acceptErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		acceptRes, acceptErr = h.Accept(cmd, binding)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("executor did not start")
	}

	cancelRes, err := h.Cancel(cmd.CommandID, binding.WorkgroupID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelRes.Entry.Status != "canceled" {
		t.Fatalf("cancel status=%s", cancelRes.Entry.Status)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Accept did not return after cancel")
	}
	if acceptErr != nil {
		t.Fatalf("Accept err=%v", acceptErr)
	}
	if acceptRes == nil || acceptRes.Entry.Status != "canceled" {
		t.Fatalf("Accept result=%+v", acceptRes)
	}
	entry, err := h.Journal.Get(cmd.CommandID)
	if err != nil || entry == nil || entry.Status != "canceled" {
		t.Fatalf("journal=%+v err=%v", entry, err)
	}
}

func TestWorkspaceExecutorRespectsCanceledContext(t *testing.T) {
	bindings := NewMemoryBindingStore()
	b := WorkerBinding{
		MemberID:       "mb_01h00000000000000000000002",
		WorkgroupID:    "wg_01h00000000000000000000001",
		WorkspacePath:  t.TempDir(),
		ToolAllowNames: []string{"read_file"},
	}
	_ = bindings.Put(b)
	exec := NewWorkspaceToolExecutor(bindings)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := exec(ctx, ToolCommand{
		MemberID:      b.MemberID,
		ToolName:      "read_file",
		ArgumentsJSON: `{"path":"x"}`,
	})
	if err == nil {
		t.Fatal("expected canceled")
	}
	if err != context.Canceled {
		t.Fatalf("err=%v", err)
	}
}
