package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestNotifyCursorBumpAndAck(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	sessionID := "sess-notify"
	if err := st.Save(ctx, Record{
		AgentID:  sessionID,
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatal(err)
	}

	if err := st.BumpNotifySeq(ctx, sessionID, 10); err != nil {
		t.Fatal(err)
	}
	rec, err := st.Load(ctx, sessionID)
	if err != nil || rec == nil || rec.RuntimeState.NotifySeq != 10 {
		t.Fatalf("notify_seq = %d err = %v", rec.RuntimeState.NotifySeq, err)
	}
	if rec.RuntimeState.HasUnread() != true {
		t.Fatal("expected unread")
	}

	state, err := st.AckSession(ctx, sessionID, 8)
	if err != nil || state == nil || state.AckSeq != 8 || !state.HasUnread() {
		t.Fatalf("partial ack state = %+v err=%v", state, err)
	}

	state, err = st.AckSession(ctx, sessionID, 10)
	if err != nil || state == nil || state.AckSeq != 10 || state.HasUnread() {
		t.Fatalf("ack state = %+v err=%v", state, err)
	}
}
