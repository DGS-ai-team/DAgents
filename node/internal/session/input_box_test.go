package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

func TestInputBoxPersistenceSnapshotExcludesReceiptHistory(t *testing.T) {
	box := NewInputBox()
	receipt := InputReceipt{ClientRequestID: "request-1", PayloadDigest: "digest", ConversationEpoch: 1}
	accepted, duplicate, err := box.AppendDeferredWithReceipt(InputKindUser, queue.Envelope{Content: "hello"}, receipt)
	if err != nil || duplicate {
		t.Fatalf("append receipt=%+v duplicate=%v err=%v", accepted, duplicate, err)
	}
	raw := box.SnapshotForPersistence()
	var state map[string]json.RawMessage
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	if _, exists := state["receipts"]; exists {
		t.Fatalf("persistence snapshot contains receipts: %s", raw)
	}
	if _, exists := state["items"]; exists {
		t.Fatalf("persistence snapshot contains queue payloads: %s", raw)
	}
	if version := string(state["queue_storage_version"]); version != "1" {
		t.Fatalf("queue storage version = %s, want 1", version)
	}
	_, rows, _, _ := box.PersistenceState()
	if len(rows) != 1 || rows[0].InputSeq != accepted.InputSeq || rows[0].Status != "queued" {
		t.Fatalf("persisted queue rows = %+v", rows)
	}
	receipts, err := InputReceiptsFromSnapshot(box.Snapshot())
	if err != nil || len(receipts) != 1 || receipts[0].ClientRequestID != receipt.ClientRequestID {
		t.Fatalf("legacy receipt extraction=%+v err=%v", receipts, err)
	}
	if got, found, err := box.LookupReceipt(receipt.ClientRequestID, 1); err != nil || !found || got.InputSeq != accepted.InputSeq {
		t.Fatalf("in-memory receipt=%+v found=%v err=%v", got, found, err)
	}
}

func TestInputBoxPersistenceProjectsRowsOnlyWhenQueueRevisionChanges(t *testing.T) {
	box := NewInputBox()
	raw, rows, _, _ := box.PersistenceState()
	if rows != nil {
		t.Fatalf("new empty queue projected rows: %+v", rows)
	}
	if !box.MarkPersistenceCommitted(raw) {
		t.Fatal("mark initial queue snapshot committed")
	}

	seq, err := box.Append(InputKindUser, queue.Envelope{Content: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	raw, rows, _, _ = box.PersistenceState()
	if len(rows) != 1 || rows[0].InputSeq != seq {
		t.Fatalf("changed queue projection=%+v", rows)
	}
	if !box.MarkPersistenceCommitted(raw) {
		t.Fatal("mark queue row projection committed")
	}
	if _, rows, _, _ = box.PersistenceState(); rows != nil {
		t.Fatalf("unchanged queue projected rows again: %+v", rows)
	}

	if _, changed, err := box.SetQueuePaused(true, 1); err != nil || !changed {
		t.Fatalf("pause changed=%v err=%v", changed, err)
	}
	raw, rows, _, _ = box.PersistenceState()
	if rows != nil {
		t.Fatalf("control-only change projected queue rows: %+v", rows)
	}
	paused, _, err := QueueControlFromSnapshot(raw)
	if err != nil || !paused {
		t.Fatalf("control snapshot paused=%v err=%v", paused, err)
	}
	if _, changed, err := box.SetQueuePaused(false, 2); err != nil || !changed {
		t.Fatalf("resume changed=%v err=%v", changed, err)
	}

	if claimed, ok := box.Pop(); !ok || claimed.Seq != seq {
		t.Fatalf("claim queued input=%+v ok=%v", claimed, ok)
	}
	if !box.Ack(seq) {
		t.Fatal("ack queued input")
	}
	raw, rows, _, _ = box.PersistenceState()
	if rows == nil || len(rows) != 0 {
		t.Fatalf("queue deletion must project a nonnil empty row set, got %#v", rows)
	}
	if !box.MarkPersistenceCommitted(raw) {
		t.Fatal("mark empty queue projection committed")
	}
	if _, rows, _, _ = box.PersistenceState(); rows != nil {
		t.Fatalf("unchanged empty queue projected rows again: %#v", rows)
	}
}

func TestInputBoxPausePersistsAndRevisionFencesClaimAndResume(t *testing.T) {
	box := NewInputBox()
	seq, err := box.Append(InputKindUser, queue.Envelope{Content: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	var beforePause inputBoxState
	if err := json.Unmarshal(box.Snapshot(), &beforePause); err != nil {
		t.Fatal(err)
	}
	revision, changed, err := box.SetQueuePaused(true, 1)
	if err != nil || !changed || revision != 2 {
		t.Fatalf("pause revision=%d changed=%v err=%v", revision, changed, err)
	}
	var afterPause inputBoxState
	if err := json.Unmarshal(box.Snapshot(), &afterPause); err != nil {
		t.Fatal(err)
	}
	if afterPause.QueueRevision != beforePause.QueueRevision {
		t.Fatalf("pause changed queue content revision from %d to %d", beforePause.QueueRevision, afterPause.QueueRevision)
	}
	if _, ok := box.PopForIdle(); ok {
		t.Fatal("paused queue allowed an idle claim")
	}
	if _, ok := box.Pop(); ok {
		t.Fatal("paused queue allowed a FIFO claim")
	}

	restored := NewInputBox()
	if err := restored.Restore(box.Snapshot()); err != nil {
		t.Fatal(err)
	}
	paused, restoredRevision := restored.QueueControlState()
	if !paused || restoredRevision != 2 {
		t.Fatalf("restored control state paused=%v revision=%d", paused, restoredRevision)
	}
	if _, changed, err := restored.SetQueuePaused(false, 1); !errors.Is(err, ErrQueueControlRevisionConflict) || changed {
		t.Fatalf("stale resume changed=%v err=%v", changed, err)
	}
	revision, changed, err = restored.SetQueuePaused(false, restoredRevision)
	if err != nil || !changed || revision != 3 {
		t.Fatalf("resume revision=%d changed=%v err=%v", revision, changed, err)
	}
	claimed, ok := restored.PopForIdle()
	if !ok || claimed.Seq != seq {
		t.Fatalf("resumed input=%+v ok=%v", claimed, ok)
	}
}

func restoreMailboxForTest(t *testing.T, box *InputBox, st interface {
	LoadInputQueue(context.Context, string) ([]store.InputQueueRow, error)
}, conversationID string, raw []byte) error {
	t.Helper()
	if !headerIsRowBacked(raw) {
		return box.Restore(raw)
	}
	rows, err := st.LoadInputQueue(context.Background(), conversationID)
	if err != nil {
		return err
	}
	return box.RestoreQueueRows(raw, rows)
}

func TestInputBoxRejectsInvalidAndOversizedInputs(t *testing.T) {
	box := NewInputBox()
	if _, err := box.Append(InputKind("control"), queue.Envelope{Content: "not external"}); !errors.Is(err, ErrInvalidInputKind) {
		t.Fatalf("invalid kind error = %v", err)
	}
	if _, err := box.Append(InputKindUser, queue.Envelope{Content: strings.Repeat("x", InputBoxMaxRecordBytes)}); !errors.Is(err, ErrInputRecordTooBig) {
		t.Fatalf("oversized input error = %v", err)
	}
	for i := 0; i < InputBoxMaxItems; i++ {
		if _, err := box.Append(InputKindUser, queue.Envelope{Content: "bounded"}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	if _, err := box.Append(InputKindUser, queue.Envelope{Content: "overflow"}); !errors.Is(err, ErrInputBoxFull) {
		t.Fatalf("overflow error = %v", err)
	}
}

func TestInputBoxRestoreRejectsDuplicateSequences(t *testing.T) {
	box := NewInputBox()
	raw := []byte(`{"seq":1,"items":[{"seq":1,"kind":"user","env":{}},{"seq":1,"kind":"trigger","env":{}}]}`)
	if err := box.Restore(raw); err == nil {
		t.Fatal("expected duplicate sequence restore to fail")
	}
}

func TestInputBoxFIFOSequenceAndRestore(t *testing.T) {
	box := NewInputBox()
	first, err := box.Append(InputKindUser, queue.Envelope{Content: "first", SessionEpoch: 3})
	if err != nil {
		t.Fatal(err)
	}
	second, err := box.Append(InputKindTrigger, queue.Envelope{Content: "second", SessionEpoch: 3})
	if err != nil {
		t.Fatal(err)
	}
	if first != 1 || second != 2 {
		t.Fatalf("sequences = %d, %d; want 1, 2", first, second)
	}

	raw := box.Snapshot()
	restored := NewInputBox()
	if err := restored.Restore(raw); err != nil {
		t.Fatal(err)
	}
	got, ok := restored.Pop()
	if !ok || got.Seq != first || got.Kind != InputKindUser || got.Env.Content != "first" {
		t.Fatalf("first restored input = %+v, ok=%v", got, ok)
	}
	if !restored.Ack(got.Seq) {
		t.Fatal("ack first restored input")
	}
	got, ok = restored.Pop()
	if !ok || got.Seq != second || got.Kind != InputKindTrigger || got.Env.Content != "second" {
		t.Fatalf("second restored input = %+v, ok=%v", got, ok)
	}
	third, err := restored.Append(InputKindUser, queue.Envelope{Content: "third", SessionEpoch: 3})
	if err != nil {
		t.Fatal(err)
	}
	if third != 3 {
		t.Fatalf("next sequence = %d, want 3", third)
	}
}

func TestInputBoxDeferredAdmissionIsHiddenUntilCommitAndRollbackPreservesSequence(t *testing.T) {
	box := NewInputBox()
	first, err := box.AppendDeferred(InputKindUser, queue.Envelope{Content: "persist first"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := box.Peek(); ok {
		t.Fatal("deferred input became visible before persistence")
	}
	if _, ok := box.Pop(); ok {
		t.Fatal("deferred input was claimed before persistence")
	}
	if !box.RollbackDeferred(first) {
		t.Fatal("rollback did not remove deferred input")
	}
	if box.Len() != 0 {
		t.Fatalf("rollback left %d queued items", box.Len())
	}
	second, err := box.AppendDeferred(InputKindUser, queue.Envelope{Content: "persist second"})
	if err != nil {
		t.Fatal(err)
	}
	if second != first+1 {
		t.Fatalf("rollback reused input sequence: got %d after %d", second, first)
	}
	if !box.CommitDeferred(second) {
		t.Fatal("commit did not release deferred input")
	}
	got, ok := box.Pop()
	if !ok || got.Seq != second || got.Env.Content != "persist second" {
		t.Fatalf("committed input=%+v, ok=%v", got, ok)
	}
}

func TestInputBoxReceiptDeduplicatesAndSurvivesAcknowledgementAndRestore(t *testing.T) {
	box := NewInputBox()
	receipt := InputReceipt{ClientRequestID: "request-1", PayloadDigest: "digest-a", ConversationEpoch: 1}
	accepted, duplicate, err := box.AppendDeferredWithReceipt(InputKindUser, queue.Envelope{RequestType: queue.RequestTypeMessage, Content: "hello"}, receipt)
	if err != nil || duplicate || accepted.InputSeq == 0 || accepted.Status != "queued" {
		t.Fatalf("first append receipt=%+v duplicate=%v err=%v", accepted, duplicate, err)
	}
	var state inputBoxState
	if err := json.Unmarshal(box.Snapshot(), &state); err != nil {
		t.Fatal(err)
	}
	var acceptedRecord InputRecord
	for _, record := range state.Items {
		if record.Seq == accepted.InputSeq {
			acceptedRecord = record
			break
		}
	}
	if acceptedRecord.ClientRequestID != receipt.ClientRequestID {
		t.Fatalf("queued record lost client request identity: %+v", acceptedRecord)
	}
	views := pendingInputViews("agent-a", state.Items)
	if len(views) != 1 || views[0].ClientRequestID != receipt.ClientRequestID {
		t.Fatalf("pending input view lost client request identity: %+v", views)
	}
	replayed, duplicate, err := box.AppendDeferredWithReceipt(InputKindUser, queue.Envelope{RequestType: queue.RequestTypeMessage, Content: "hello"}, receipt)
	if err != nil || !duplicate || replayed.InputSeq != accepted.InputSeq {
		t.Fatalf("duplicate receipt=%+v duplicate=%v err=%v", replayed, duplicate, err)
	}
	changed := receipt
	changed.PayloadDigest = "digest-b"
	if _, _, err := box.AppendDeferredWithReceipt(InputKindUser, queue.Envelope{Content: "changed"}, changed); !errors.Is(err, ErrInputRequestConflict) {
		t.Fatalf("same request ID with changed content error=%v", err)
	}
	if !box.CommitDeferred(accepted.InputSeq) {
		t.Fatal("commit did not release accepted input")
	}
	claimed, ok := box.Pop()
	if !ok || claimed.Seq != accepted.InputSeq {
		t.Fatalf("claimed input=%+v ok=%v", claimed, ok)
	}
	if !box.MarkCompleted(claimed.Seq) || !box.Ack(claimed.Seq) {
		t.Fatal("failed to complete and acknowledge accepted input")
	}

	restored := NewInputBox()
	if err := restored.Restore(box.Snapshot()); err != nil {
		t.Fatal(err)
	}
	got, found, err := restored.LookupReceipt("request-1", 1)
	if err != nil || !found || got.InputSeq != accepted.InputSeq || got.Status != "consumed" {
		t.Fatalf("restored receipt=%+v found=%v err=%v", got, found, err)
	}
	replayed, duplicate, err = restored.AppendDeferredWithReceipt(InputKindUser, queue.Envelope{Content: "hello"}, receipt)
	if err != nil || !duplicate || replayed.InputSeq != accepted.InputSeq {
		t.Fatalf("post-ack replay receipt=%+v duplicate=%v err=%v", replayed, duplicate, err)
	}
}

func TestRecoveredUserInputDeduplicationUsesRecordIDNotMessageText(t *testing.T) {
	rt := &runtime{messages: []llm.Message{{Role: "user", Content: "same text"}}}
	target := llm.Message{Role: "user", Content: "same text", RecordID: "agent-a/input/7"}
	if rt.historyHasUserRecordLocked(target) {
		t.Fatal("same text without the recovered input RecordID was treated as a duplicate")
	}
	rt.messages = append(rt.messages, target)
	if !rt.historyHasUserRecordLocked(target) {
		t.Fatal("matching stable RecordID was not recognized during recovery")
	}
}

func TestInputBoxReceiptRollbackAndClearEpochFence(t *testing.T) {
	box := NewInputBox()
	receipt := InputReceipt{ClientRequestID: "request-before-clear", PayloadDigest: "digest", ConversationEpoch: 1}
	accepted, duplicate, err := box.AppendDeferredWithReceipt(InputKindUser, queue.Envelope{Content: "rolled back"}, receipt)
	if err != nil || duplicate {
		t.Fatalf("deferred append duplicate=%v err=%v", duplicate, err)
	}
	if !box.RollbackDeferred(accepted.InputSeq) {
		t.Fatal("rollback did not remove deferred input")
	}
	if _, found, err := box.LookupReceipt(receipt.ClientRequestID, 1); err != nil || found {
		t.Fatalf("rolled back receipt found=%v err=%v", found, err)
	}
	queued, duplicate, err := box.AppendDeferredWithReceipt(InputKindUser, queue.Envelope{Content: "old conversation", SessionEpoch: 1}, receipt)
	if err != nil || duplicate {
		t.Fatalf("pre-clear append duplicate=%v err=%v", duplicate, err)
	}
	if !box.CommitDeferred(queued.InputSeq) {
		t.Fatal("pre-clear input did not commit")
	}
	if dropped := box.DropStale(2); dropped != 1 {
		t.Fatalf("clear dropped=%d, want 1", dropped)
	}
	if _, found, err := box.LookupReceipt(receipt.ClientRequestID, 2); err != nil || found {
		t.Fatalf("clear retained old receipt: found=%v err=%v", found, err)
	}
	if _, _, err := box.LookupReceipt(receipt.ClientRequestID, 1); !errors.Is(err, ErrInputConversationChanged) {
		t.Fatalf("old epoch receipt lookup error=%v", err)
	}
	newReceipt := InputReceipt{ClientRequestID: receipt.ClientRequestID, PayloadDigest: "digest", ConversationEpoch: 2}
	newInput, duplicate, err := box.AppendDeferredWithReceipt(InputKindUser, queue.Envelope{Content: "new conversation"}, newReceipt)
	if err != nil || duplicate || newInput.InputSeq <= queued.InputSeq {
		t.Fatalf("new epoch append receipt=%+v duplicate=%v err=%v", newInput, duplicate, err)
	}
}

func TestPendingUserInputsExposeQueuedAndClaimedWithoutTriggerPayloads(t *testing.T) {
	box := NewInputBox()
	queued, _ := box.Append(InputKindUser, queue.Envelope{Content: "queued"})
	_, _ = box.Append(InputKindTrigger, queue.Envelope{Content: "private trigger"})
	claimed, ok := box.Pop()
	if !ok || claimed.Seq != queued {
		t.Fatalf("claimed input=%+v, ok=%v", claimed, ok)
	}
	views := box.PendingUserInputs()
	if len(views) != 1 || views[0].Seq != queued || !views[0].Claimed || views[0].Env.Content != "queued" {
		t.Fatalf("pending user input view=%+v", views)
	}
	fromSnapshot := pendingUserInputsFromSnapshot(box.Snapshot())
	if len(fromSnapshot) != 1 || fromSnapshot[0].Seq != queued || !fromSnapshot[0].Claimed {
		t.Fatalf("restored user input view=%+v", fromSnapshot)
	}
}

func TestInputBoxQueuedEditUsesCASAndBlocksClaimsUntilCommit(t *testing.T) {
	box := NewInputBox()
	first, _ := box.Append(InputKindUser, queue.Envelope{RequestType: queue.RequestTypeMessage, Content: "first"})
	second, _ := box.Append(InputKindUser, queue.Envelope{RequestType: queue.RequestTypeMessage, Content: "second"})

	if _, _, err := box.BeginQueuedEdit(second, 2, queue.Envelope{RequestType: queue.RequestTypeMessage, Content: "stale"}); !errors.Is(err, ErrInputRevisionConflict) {
		t.Fatalf("stale edit error=%v, want revision conflict", err)
	}
	revised, mutation, err := box.BeginQueuedEdit(second, 1, queue.Envelope{RequestType: queue.RequestTypeMessage, Content: "edited"})
	if err != nil {
		t.Fatal(err)
	}
	if revised.Seq != second || revised.Revision != 2 || revised.Env.Content != "edited" {
		t.Fatalf("revised queued input=%+v", revised)
	}
	if _, ok := box.Pop(); ok {
		t.Fatal("claim passed a pending durable mutation")
	}
	if _, _, err := box.BeginQueuedDelete(second, 2); !errors.Is(err, ErrInputMutationInProgress) {
		t.Fatalf("overlapping mutation error=%v", err)
	}
	if !box.RollbackMutation(mutation) {
		t.Fatal("rollback did not restore edit")
	}
	got, ok := box.Pop()
	if !ok || got.Seq != first || got.Env.Content != "first" {
		t.Fatalf("first input after rollback=%+v, ok=%v", got, ok)
	}
	box.Ack(first)
	got, ok = box.Pop()
	if !ok || got.Seq != second || got.Revision != 1 || got.Env.Content != "second" {
		t.Fatalf("input after rollback=%+v, ok=%v", got, ok)
	}
}

func TestInputBoxQueuedDeleteIsCASAndOnlyRemovesUnclaimedInput(t *testing.T) {
	box := NewInputBox()
	first, _ := box.Append(InputKindUser, queue.Envelope{RequestType: queue.RequestTypeMessage, Content: "first"})
	second, _ := box.Append(InputKindUser, queue.Envelope{RequestType: queue.RequestTypeMessage, Content: "second"})
	if _, _, err := box.BeginQueuedDelete(second, 2); !errors.Is(err, ErrInputRevisionConflict) {
		t.Fatalf("stale delete error=%v, want revision conflict", err)
	}
	deleted, mutation, err := box.BeginQueuedDelete(second, 1)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Seq != second || box.Len() != 1 {
		t.Fatalf("deleted=%+v queued=%d", deleted, box.Len())
	}
	if !box.RollbackMutation(mutation) {
		t.Fatal("rollback did not restore deleted input")
	}
	if box.Len() != 2 {
		t.Fatalf("rollback queue length=%d, want 2", box.Len())
	}
	_, mutation, err = box.BeginQueuedDelete(second, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !box.CommitMutation(mutation) {
		t.Fatal("commit did not release queue")
	}
	claimed, ok := box.Pop()
	if !ok || claimed.Seq != first {
		t.Fatalf("first input after committed delete=%+v, ok=%v", claimed, ok)
	}
	if _, _, err := box.BeginQueuedDelete(first, 1); !errors.Is(err, ErrInputNotQueued) {
		t.Fatalf("claimed input delete error=%v, want not queued", err)
	}
	box.Ack(first)
	if _, ok := box.Pop(); ok {
		t.Fatal("deleted input was still claimable")
	}
}

func TestInputBoxPopForIdlePrioritizesUserOverSystemAuto(t *testing.T) {
	box := NewInputBox()
	if _, err := box.Append(InputKindSystemAuto, queue.Envelope{Content: "auto", TriggerID: "auto-default:a", DeliveryID: "d1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := box.Append(InputKindUser, queue.Envelope{Content: "user-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := box.Append(InputKindUser, queue.Envelope{Content: "user-2"}); err != nil {
		t.Fatal(err)
	}
	got, ok := box.PopForIdle()
	if !ok || got.Kind != InputKindUser || got.Env.Content != "user-1" {
		t.Fatalf("first idle input=%+v, want user-1", got)
	}
	box.Ack(got.Seq)
	got, ok = box.PopForIdle()
	if !ok || got.Env.Content != "user-2" {
		t.Fatalf("second idle input=%+v, want user-2", got)
	}
	box.Ack(got.Seq)
	got, ok = box.PopForIdle()
	if !ok || got.Kind != InputKindSystemAuto {
		t.Fatalf("third idle input=%+v, want system auto", got)
	}
}

func TestInputBoxIdlePriorityKeepsOrdinaryTriggerFIFOBarrier(t *testing.T) {
	box := NewInputBox()
	_, _ = box.Append(InputKindSystemAuto, queue.Envelope{Content: "auto", TriggerID: "auto-default:a", DeliveryID: "d1"})
	_, _ = box.Append(InputKindTrigger, queue.Envelope{Content: "ordinary", TriggerID: "user-trigger"})
	_, _ = box.Append(InputKindUser, queue.Envelope{Content: "user"})
	got, ok := box.PopForIdle()
	if !ok || got.Kind != InputKindSystemAuto {
		t.Fatalf("first input=%+v, want auto before ordinary trigger barrier", got)
	}
	box.Ack(got.Seq)
	got, ok = box.PopForIdle()
	if !ok || got.Kind != InputKindTrigger || got.Env.Content != "ordinary" {
		t.Fatalf("second input=%+v, want ordinary trigger FIFO", got)
	}
}

func TestInputBoxSystemAutoInFlightSurvivesRestore(t *testing.T) {
	box := NewInputBox()
	seq, err := box.Append(InputKindSystemAuto, queue.Envelope{Content: "auto", TriggerID: "auto-default:a", DeliveryID: "d1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := box.PopForIdle(); !ok {
		t.Fatal("system auto was not claimed")
	}
	restored := NewInputBox()
	if err := restored.Restore(box.Snapshot()); err != nil {
		t.Fatal(err)
	}
	got, ok := restored.InFlight()
	if !ok || got.Seq != seq || got.Kind != InputKindSystemAuto || got.Env.DeliveryID != "d1" {
		t.Fatalf("restored in-flight=%+v, want system auto delivery", got)
	}
}

func TestInputBoxDropStalePreservesSequence(t *testing.T) {
	box := NewInputBox()
	_, _ = box.Append(InputKindUser, queue.Envelope{Content: "old", SessionEpoch: 1})
	_, _ = box.Append(InputKindUser, queue.Envelope{Content: "new", SessionEpoch: 2})
	if dropped := box.DropStale(2); dropped != 1 {
		t.Fatalf("dropped = %d, want 1", dropped)
	}
	got, ok := box.Pop()
	if !ok || got.Env.Content != "new" {
		t.Fatalf("remaining input = %+v, ok=%v", got, ok)
	}
	seq, err := box.Append(InputKindTrigger, queue.Envelope{Content: "later", SessionEpoch: 2})
	if err != nil {
		t.Fatal(err)
	}
	if seq != 3 {
		t.Fatalf("sequence after drop = %d, want 3", seq)
	}
}

func TestInputBoxInFlightSurvivesRestartAndAcknowledgement(t *testing.T) {
	box := NewInputBox()
	seq, err := box.Append(InputKindUser, queue.Envelope{Content: "recover me", SessionEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := box.Pop(); !ok || got.Seq != seq {
		t.Fatalf("popped input = %+v, ok=%v", got, ok)
	}

	restored := NewInputBox()
	if err := restored.Restore(box.Snapshot()); err != nil {
		t.Fatal(err)
	}
	if got, ok := restored.InFlight(); !ok || got.Seq != seq || got.Completed {
		t.Fatalf("restored in-flight input = %+v, ok=%v", got, ok)
	}
	if !restored.MarkCompleted(seq) {
		t.Fatal("MarkCompleted returned false")
	}
	completed := NewInputBox()
	if err := completed.Restore(restored.Snapshot()); err != nil {
		t.Fatal(err)
	}
	if got, ok := completed.InFlight(); !ok || !got.Completed {
		t.Fatalf("completed in-flight input = %+v, ok=%v", got, ok)
	}
	if !completed.Ack(seq) || completed.Len() != 0 {
		t.Fatal("ack did not clear in-flight input")
	}
}

func TestInputBoxRequeueInFlightPreservesFIFO(t *testing.T) {
	box := NewInputBox()
	first, _ := box.Append(InputKindUser, queue.Envelope{Content: "first"})
	second, _ := box.Append(InputKindUser, queue.Envelope{Content: "second"})
	got, ok := box.Pop()
	if !ok || got.Seq != first {
		t.Fatalf("popped input = %+v, ok=%v", got, ok)
	}
	if !box.RequeueInFlight() {
		t.Fatal("RequeueInFlight returned false")
	}
	got, ok = box.Pop()
	if !ok || got.Seq != first {
		t.Fatalf("requeued input = %+v, ok=%v", got, ok)
	}
	if !box.Ack(first) {
		t.Fatal("ack requeued input returned false")
	}
	got, ok = box.Pop()
	if !ok || got.Seq != second {
		t.Fatalf("second input = %+v, ok=%v", got, ok)
	}
}

func TestActiveTurnDefersPoppedInputWithoutDroppingIt(t *testing.T) {
	hub := stream.NewHub(8, logx.Discard())
	rt := newRuntimeWithPublisher(
		"session-input-race", "agent-1", hub, hub, &llm.MockClient{}, nil, nil, nil,
		logx.Discard(), nil, nil, nil, 0, 0, TurnOptions{}, nil,
	)
	if err := rt.lifecycleBeginHumanTurn(); err != nil {
		t.Fatal(err)
	}
	seq, err := rt.inputBox.Append(InputKindUser, queue.Envelope{Content: "keep me"})
	if err != nil {
		t.Fatal(err)
	}
	record, ok := rt.inputBox.Pop()
	if !ok || record.Seq != seq {
		t.Fatalf("popped input = %+v, ok=%v", record, ok)
	}

	if consumed := rt.dispatchInput(context.Background(), record); consumed {
		t.Fatal("active Turn consumed an input instead of deferring it")
	}
	if _, inFlight := rt.inputBox.InFlight(); inFlight {
		t.Fatal("deferred input remained in-flight")
	}
	if got, ok := rt.inputBox.Peek(); !ok || got.Seq != seq || got.Env.Content != "keep me" {
		t.Fatalf("deferred input = %+v, ok=%v", got, ok)
	}
}
