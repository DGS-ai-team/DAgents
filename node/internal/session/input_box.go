package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

// InputKind identifies an external input which may start a Turn.  Control
// operations such as resume and cancel deliberately do not use InputBox:
// they operate on the currently active Turn rather than becoming transcript
// messages.
type InputKind string

const (
	InputKindUser    InputKind = "user"
	InputKindTrigger InputKind = "trigger"
	// InputKindSystemAuto is set by the trusted trigger submitter.
	InputKindSystemAuto InputKind = "system_auto"
	InputKindChildAgent InputKind = "child_agent"
)

const (
	// InputBoxMaxItems bounds accepted external inputs while a session is
	// busy. Control-plane resume/cancel commands do not enter this mailbox.
	InputBoxMaxItems = 256
	// InputBoxMaxRecordBytes prevents a single trigger/child-agent payload from
	// exhausting the session runtime before the model can consume it.
	InputBoxMaxRecordBytes = 1 << 20
)

var (
	ErrInputBoxFull                 = errors.New("input box is full")
	ErrInputRecordTooBig            = errors.New("input record is too large")
	ErrInvalidInputKind             = errors.New("invalid input kind")
	ErrInputSequenceFull            = errors.New("input sequence exhausted")
	ErrInputRevisionFull            = errors.New("input revision exhausted")
	ErrInputNotFound                = errors.New("input not found")
	ErrInputNotQueued               = errors.New("input is no longer queued")
	ErrInputRevisionConflict        = errors.New("input revision conflict")
	ErrInputMutationInProgress      = errors.New("input mutation in progress")
	ErrInputRequestConflict         = errors.New("input request id conflict")
	ErrInputConversationChanged     = errors.New("conversation changed")
	ErrQueueControlRevisionConflict = errors.New("input queue control revision conflict")
)

func validateInputRecord(record InputRecord) error {
	if record.Kind != InputKindUser && record.Kind != InputKindTrigger && record.Kind != InputKindSystemAuto && record.Kind != InputKindChildAgent {
		return fmt.Errorf("%w: %q", ErrInvalidInputKind, record.Kind)
	}
	if record.Kind == InputKindSystemAuto && (record.Env.TriggerID == "" || record.Env.DeliveryID == "") {
		return fmt.Errorf("%w: system auto requires trigger and delivery identity", ErrInvalidInputKind)
	}
	if request := record.Env.ConversationResubmit; request != nil {
		if record.Kind != InputKindUser || strings.TrimSpace(record.Env.RequestType) != queue.RequestTypeMessage ||
			strings.TrimSpace(request.RoundID) == "" || strings.TrimSpace(request.ExpectedSelectedRevisionID) == "" || request.ExpectedConversationRevision == 0 {
			return fmt.Errorf("%w: conversation resubmit identity is incomplete", ErrInvalidInputKind)
		}
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode input record: %w", err)
	}
	if len(raw) > InputBoxMaxRecordBytes {
		return ErrInputRecordTooBig
	}
	return nil
}

// InputRecord is the FIFO record accepted by a session.  Seq is assigned at
// append time and is never reused. queue.Envelope carries the common request
// payload shape; control requests still use MessageQueue directly.
type InputRecord struct {
	Seq             uint64         `json:"seq"`
	Revision        uint64         `json:"revision"`
	Kind            InputKind      `json:"kind"`
	Env             queue.Envelope `json:"env"`
	ClientRequestID string         `json:"client_request_id,omitempty"`
	Completed       bool           `json:"completed,omitempty"`
	Claimed         bool           `json:"-"`
	// RecoveredLegacy fences pre-delivery-identity trigger records restored
	// after restart. It is process-local and deliberately never persisted.
	RecoveredLegacy bool `json:"-"`
}

// InputReceipt is an indexed durable idempotency receipt for one
// HTTP-submitted user message. The in-memory map is only a cache; SQLite's
// conversation_inputs table is the durable owner. A clear advances the epoch
// and deletes prior-epoch rows, while the epoch fence rejects late requests.
type InputReceipt = store.InputReceipt

type inputBoxState struct {
	Seq                  uint64                  `json:"seq"`
	ConversationEpoch    uint64                  `json:"conversation_epoch,omitempty"`
	QueueRevision        uint64                  `json:"queue_revision,omitempty"`
	QueuePaused          bool                    `json:"queue_paused,omitempty"`
	QueueControlRevision uint64                  `json:"queue_control_revision,omitempty"`
	QueueStorageVersion  int                     `json:"queue_storage_version,omitempty"`
	Items                []InputRecord           `json:"items,omitempty"`
	InFlight             *InputRecord            `json:"in_flight,omitempty"`
	Receipts             map[string]InputReceipt `json:"receipts,omitempty"`
}

func inputBoxPendingCount(raw []byte) int {
	if len(raw) == 0 {
		return 0
	}
	var state inputBoxState
	if err := json.Unmarshal(raw, &state); err != nil {
		return 0
	}
	return len(state.Items)
}

func headerIsRowBacked(raw []byte) bool {
	var state inputBoxState
	return len(raw) > 0 && json.Unmarshal(raw, &state) == nil && state.QueueStorageVersion == 1
}

func QueueControlFromSnapshot(raw []byte) (bool, uint64, error) {
	var state inputBoxState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &state); err != nil {
			return false, 0, fmt.Errorf("decode input queue control state: %w", err)
		}
	}
	if state.QueueControlRevision == 0 {
		state.QueueControlRevision = 1
	}
	return state.QueuePaused, state.QueueControlRevision, nil
}

func countQueuedInputRows(rows []store.InputQueueRow) int {
	count := 0
	for _, row := range rows {
		if row.Status == "queued" {
			count++
		}
	}
	return count
}

// InputBox is a per-session FIFO mailbox for external inputs.  It does not
// execute turns, apply history, or reorder records.  Wake is intentionally a
// coalesced notification; consumers must always drain with Peek/Pop so a
// notification cannot represent the data itself.
type InputBox struct {
	mu                     sync.Mutex
	seq                    uint64
	conversationEpoch      uint64
	queueRevision          uint64
	persistedQueueEpoch    uint64
	persistedQueueRevision uint64
	queuePaused            bool
	queueControlRevision   uint64
	items                  []InputRecord
	inFlight               *InputRecord
	receipts               map[string]InputReceipt
	deferred               map[uint64]struct{}
	mutationSeq            uint64
	pendingMutation        uint64
	mutationBefore         []InputRecord
	wake                   chan struct{}
	closed                 bool
}

type inputBoxMutation struct {
	seq                   uint64
	previousItems         []InputRecord
	previousQueueRevision uint64
}

func NewInputBox() *InputBox {
	return &InputBox{conversationEpoch: 1, queueRevision: 1, persistedQueueEpoch: 1, persistedQueueRevision: 1, queueControlRevision: 1, wake: make(chan struct{}, 1), deferred: make(map[uint64]struct{}), receipts: make(map[string]InputReceipt)}
}

// QueueControlState is the durable pause state for this conversation's root
// input queue. The revision fences stale pause/resume commands.
func (b *InputBox) QueueControlState() (paused bool, revision uint64) {
	if b == nil {
		return false, 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.queuePaused, b.queueControlRevision
}

// SetQueuePaused applies a revision-checked pause/resume transition. Callers
// that persist the state must hold the execution gate across this change and
// its durable write so no consumer can claim an input in between.
func (b *InputBox) SetQueuePaused(paused bool, expectedRevision uint64) (uint64, bool, error) {
	if b == nil {
		return 0, false, errors.New("input box unavailable")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return b.queueControlRevision, false, errors.New("input box closed")
	}
	if expectedRevision != 0 && expectedRevision != b.queueControlRevision {
		return b.queueControlRevision, false, ErrQueueControlRevisionConflict
	}
	if b.queuePaused == paused {
		return b.queueControlRevision, false, nil
	}
	if b.queueControlRevision == ^uint64(0) {
		return b.queueControlRevision, false, ErrInputRevisionFull
	}
	b.queuePaused = paused
	b.queueControlRevision++
	if !paused {
		b.signalLocked()
	}
	return b.queueControlRevision, true, nil
}

// RestoreQueueControl aligns the live mailbox with a queue-control state that
// was committed together with terminal facts.
func (b *InputBox) RestoreQueueControl(paused bool, revision uint64) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.queuePaused = paused
	b.queueControlRevision = revision
	if b.queueControlRevision == 0 {
		b.queueControlRevision = 1
	}
	b.mu.Unlock()
}

// Append accepts one external input and returns its monotonic session seq.
func (b *InputBox) Append(kind InputKind, env queue.Envelope) (uint64, error) {
	return b.append(kind, env, false)
}

// AppendDeferred persists the accepted record before allowing the consumer to
// claim it. CommitDeferred releases it after the durable runtime snapshot has
// succeeded; RollbackDeferred removes it if that write fails.
func (b *InputBox) AppendDeferred(kind InputKind, env queue.Envelope) (uint64, error) {
	return b.append(kind, env, true)
}

// AppendDeferredWithReceipt performs the request-ID check and FIFO append
// under the same mailbox lock. The caller must persist Snapshot before
// CommitDeferred, and must RollbackDeferred on failure.
func (b *InputBox) AppendDeferredWithReceipt(kind InputKind, env queue.Envelope, receipt InputReceipt) (InputReceipt, bool, error) {
	if b == nil {
		return InputReceipt{}, false, errors.New("input box unavailable")
	}
	requestID := strings.TrimSpace(receipt.ClientRequestID)
	if kind != InputKindUser || requestID == "" || len(requestID) > 128 || strings.TrimSpace(receipt.PayloadDigest) == "" || receipt.InputSeq != 0 {
		return InputReceipt{}, false, errors.New("invalid input receipt")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return InputReceipt{}, false, errors.New("input box closed")
	}
	if b.pendingMutation != 0 {
		return InputReceipt{}, false, ErrInputMutationInProgress
	}
	if receipt.ConversationEpoch == 0 || receipt.ConversationEpoch != b.conversationEpoch {
		return InputReceipt{}, false, ErrInputConversationChanged
	}
	if previous, exists := b.receipts[requestID]; exists {
		if previous.ConversationEpoch != receipt.ConversationEpoch {
			return InputReceipt{}, false, ErrInputConversationChanged
		}
		if previous.PayloadDigest != receipt.PayloadDigest {
			return InputReceipt{}, false, ErrInputRequestConflict
		}
		return b.currentReceiptLocked(previous), true, nil
	}
	if len(b.items) >= InputBoxMaxItems {
		return InputReceipt{}, false, ErrInputBoxFull
	}
	seq := b.seq + 1
	if seq == 0 {
		return InputReceipt{}, false, ErrInputSequenceFull
	}
	record := InputRecord{Seq: seq, Revision: 1, Kind: kind, Env: env, ClientRequestID: requestID}
	if err := validateInputRecord(record); err != nil {
		return InputReceipt{}, false, err
	}
	b.seq = seq
	b.items = append(b.items, record)
	b.queueRevision++
	receipt.InputSeq = seq
	receipt.InputRevision = record.Revision
	receipt.Status = "queued"
	receipt.ClientRequestID = requestID
	b.receipts[requestID] = receipt
	if b.deferred == nil {
		b.deferred = make(map[uint64]struct{})
	}
	b.deferred[seq] = struct{}{}
	return receipt, false, nil
}

// LookupReceipt returns the original durable receipt plus the current queue
// status. A stale epoch is rejected even when the request ID is unknown.
func (b *InputBox) LookupReceipt(requestID string, conversationEpoch uint64) (InputReceipt, bool, error) {
	if b == nil {
		return InputReceipt{}, false, errors.New("input box unavailable")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lookupReceiptLocked(requestID, conversationEpoch)
}

func (b *InputBox) lookupReceiptLocked(requestID string, conversationEpoch uint64) (InputReceipt, bool, error) {
	if conversationEpoch == 0 || conversationEpoch != b.conversationEpoch {
		return InputReceipt{}, false, ErrInputConversationChanged
	}
	receipt, ok := b.receipts[strings.TrimSpace(requestID)]
	if !ok {
		return InputReceipt{}, false, nil
	}
	if receipt.ConversationEpoch != conversationEpoch {
		return InputReceipt{}, false, ErrInputConversationChanged
	}
	return b.currentReceiptLocked(receipt), true, nil
}

func (b *InputBox) currentReceiptLocked(receipt InputReceipt) InputReceipt {
	for _, item := range b.items {
		if item.Seq == receipt.InputSeq {
			receipt.Status = "queued"
			receipt.InputRevision = item.Revision
			return receipt
		}
	}
	if b.inFlight != nil && b.inFlight.Seq == receipt.InputSeq {
		receipt.InputRevision = b.inFlight.Revision
		if b.inFlight.Completed {
			receipt.Status = "consumed"
		} else {
			receipt.Status = "claimed"
		}
		return receipt
	}
	if receipt.Status == "queued" || receipt.Status == "claimed" {
		receipt.Status = "cancelled"
	}
	return receipt
}

// ConversationEpoch exposes the mailbox generation for hydrate and POST
// validation. It changes only when the conversation is explicitly cleared.
func (b *InputBox) ConversationEpoch() uint64 {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.conversationEpoch
}

// LookupReceiptInSnapshot is the cold-runtime equivalent of LookupReceipt.
func LookupReceiptInSnapshot(raw []byte, requestID string, conversationEpoch uint64) (InputReceipt, bool, error) {
	var state inputBoxState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &state); err != nil {
			return InputReceipt{}, false, fmt.Errorf("decode input box state: %w", err)
		}
	}
	if state.ConversationEpoch == 0 {
		state.ConversationEpoch = 1
	}
	if conversationEpoch == 0 || conversationEpoch != state.ConversationEpoch {
		return InputReceipt{}, false, ErrInputConversationChanged
	}
	receipt, ok := state.Receipts[strings.TrimSpace(requestID)]
	if !ok {
		return InputReceipt{}, false, nil
	}
	return projectInputReceipt(state, receipt, conversationEpoch)
}

func projectInputReceipt(state inputBoxState, receipt InputReceipt, conversationEpoch uint64) (InputReceipt, bool, error) {
	if receipt.ConversationEpoch != conversationEpoch {
		return InputReceipt{}, false, ErrInputConversationChanged
	}
	for _, item := range state.Items {
		if item.Seq == receipt.InputSeq {
			receipt.Status = "queued"
			receipt.InputRevision = item.Revision
			return receipt, true, nil
		}
	}
	if state.InFlight != nil && state.InFlight.Seq == receipt.InputSeq {
		receipt.InputRevision = state.InFlight.Revision
		if state.InFlight.Completed {
			receipt.Status = "consumed"
		} else {
			receipt.Status = "claimed"
		}
		return receipt, true, nil
	}
	if receipt.Status == "queued" || receipt.Status == "claimed" {
		receipt.Status = "cancelled"
	}
	return receipt, true, nil
}

// ProjectInputReceiptInSnapshot combines a separately stored receipt with the
// mailbox snapshot to resolve its current queue state without storing an
// ever-growing receipt map in the runtime JSON.
func ProjectInputReceiptInSnapshot(raw []byte, receipt InputReceipt, conversationEpoch uint64) (InputReceipt, bool, error) {
	var state inputBoxState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &state); err != nil {
			return InputReceipt{}, false, fmt.Errorf("decode input box state: %w", err)
		}
	}
	if state.ConversationEpoch == 0 {
		state.ConversationEpoch = 1
	}
	if conversationEpoch == 0 || conversationEpoch != state.ConversationEpoch {
		return InputReceipt{}, false, ErrInputConversationChanged
	}
	if state.QueueStorageVersion == 1 {
		// The indexed receipt row is authoritative for row-backed snapshots; the
		// runtime header deliberately contains no input payloads to project.
		return receipt, true, nil
	}
	return projectInputReceipt(state, receipt, conversationEpoch)
}

func ConversationEpochFromSnapshot(raw []byte) (uint64, error) {
	var state inputBoxState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &state); err != nil {
			return 0, fmt.Errorf("decode input box state: %w", err)
		}
	}
	if state.ConversationEpoch == 0 {
		return 1, nil
	}
	return state.ConversationEpoch, nil
}

func (b *InputBox) append(kind InputKind, env queue.Envelope, deferred bool) (uint64, error) {
	if b == nil {
		return 0, errors.New("input box unavailable")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, errors.New("input box closed")
	}
	if b.pendingMutation != 0 {
		return 0, ErrInputMutationInProgress
	}
	if len(b.items) >= InputBoxMaxItems {
		return 0, ErrInputBoxFull
	}
	record := InputRecord{Seq: b.seq + 1, Revision: 1, Kind: kind, Env: env}
	if record.Seq == 0 {
		return 0, ErrInputSequenceFull
	}
	if err := validateInputRecord(record); err != nil {
		return 0, err
	}
	b.seq = record.Seq
	b.items = append(b.items, record)
	b.queueRevision++
	if deferred {
		if b.deferred == nil {
			b.deferred = make(map[uint64]struct{})
		}
		b.deferred[record.Seq] = struct{}{}
	} else {
		b.signalLocked()
	}
	return record.Seq, nil
}

func (b *InputBox) signalLocked() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

func (b *InputBox) CommitDeferred(seq uint64) bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.deferred[seq]; !ok {
		return false
	}
	delete(b.deferred, seq)
	b.signalLocked()
	return true
}

func (b *InputBox) RollbackDeferred(seq uint64) bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.deferred[seq]; !ok {
		return false
	}
	delete(b.deferred, seq)
	for i := range b.items {
		if b.items[i].Seq == seq {
			b.items = append(b.items[:i], b.items[i+1:]...)
			b.queueRevision++
			for requestID, receipt := range b.receipts {
				if receipt.InputSeq == seq {
					delete(b.receipts, requestID)
				}
			}
			return true
		}
	}
	return false
}

func (b *InputBox) Peek() (InputRecord, bool) {
	if b == nil {
		return InputRecord{}, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pendingMutation != 0 || len(b.items) == 0 || b.isDeferredLocked(b.items[0].Seq) {
		return InputRecord{}, false
	}
	return b.items[0], true
}

func (b *InputBox) Pop() (InputRecord, bool) {
	if b == nil {
		return InputRecord{}, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.queuePaused || b.pendingMutation != 0 || len(b.items) == 0 || b.inFlight != nil || b.isDeferredLocked(b.items[0].Seq) {
		return InputRecord{}, false
	}
	record := b.items[0]
	b.items = b.items[1:]
	b.inFlight = &record
	b.queueRevision++
	return record, true
}

// PopForIdle gives a queued user message precedence over an earlier system
// Auto wakeup. Other records retain their FIFO order.
func (b *InputBox) PopForIdle() (InputRecord, bool) {
	if b == nil {
		return InputRecord{}, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.queuePaused || b.pendingMutation != 0 || len(b.items) == 0 || b.inFlight != nil || b.isDeferredLocked(b.items[0].Seq) {
		return InputRecord{}, false
	}
	index := 0
	if b.items[0].Kind == InputKindSystemAuto {
		for i := 1; i < len(b.items); i++ {
			if b.items[i].Kind == InputKindUser && !b.isDeferredLocked(b.items[i].Seq) {
				index = i
				break
			}
			if b.items[i].Kind != InputKindSystemAuto {
				break
			}
		}
	}
	record := b.items[index]
	b.items = append(b.items[:index], b.items[index+1:]...)
	b.inFlight = &record
	b.queueRevision++
	return record, true
}

// BeginQueuedEdit applies an expected-revision update while temporarily
// pausing queue claims. The caller must durably persist Snapshot before
// committing the mutation; on persistence failure it must roll back.
func (b *InputBox) BeginQueuedEdit(seq, expectedRevision uint64, env queue.Envelope) (InputRecord, *inputBoxMutation, error) {
	if b == nil {
		return InputRecord{}, nil, errors.New("input box unavailable")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return InputRecord{}, nil, errors.New("input box closed")
	}
	if b.pendingMutation != 0 {
		return InputRecord{}, nil, ErrInputMutationInProgress
	}
	for index := range b.items {
		current := b.items[index]
		if current.Seq != seq {
			continue
		}
		if current.Kind != InputKindUser || b.isDeferredLocked(seq) {
			return InputRecord{}, nil, ErrInputNotQueued
		}
		if current.Revision != expectedRevision {
			return InputRecord{}, nil, ErrInputRevisionConflict
		}
		if current.Revision == ^uint64(0) {
			return InputRecord{}, nil, ErrInputRevisionFull
		}
		replacement := InputRecord{Seq: seq, Revision: current.Revision + 1, Kind: InputKindUser, Env: env}
		if err := validateInputRecord(replacement); err != nil {
			return InputRecord{}, nil, err
		}
		mutation := b.beginMutationLocked()
		b.items[index] = replacement
		b.queueRevision++
		return replacement, mutation, nil
	}
	if b.inFlight != nil && b.inFlight.Seq == seq {
		return InputRecord{}, nil, ErrInputNotQueued
	}
	return InputRecord{}, nil, ErrInputNotFound
}

// BeginQueuedDelete removes only a queued human input at the expected
// revision. It blocks the consumer until the caller commits or rolls back.
func (b *InputBox) BeginQueuedDelete(seq, expectedRevision uint64) (InputRecord, *inputBoxMutation, error) {
	if b == nil {
		return InputRecord{}, nil, errors.New("input box unavailable")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return InputRecord{}, nil, errors.New("input box closed")
	}
	if b.pendingMutation != 0 {
		return InputRecord{}, nil, ErrInputMutationInProgress
	}
	for index := range b.items {
		current := b.items[index]
		if current.Seq != seq {
			continue
		}
		if current.Kind != InputKindUser || b.isDeferredLocked(seq) {
			return InputRecord{}, nil, ErrInputNotQueued
		}
		if current.Revision != expectedRevision {
			return InputRecord{}, nil, ErrInputRevisionConflict
		}
		mutation := b.beginMutationLocked()
		b.items = append(b.items[:index], b.items[index+1:]...)
		b.queueRevision++
		return current, mutation, nil
	}
	if b.inFlight != nil && b.inFlight.Seq == seq {
		return InputRecord{}, nil, ErrInputNotQueued
	}
	return InputRecord{}, nil, ErrInputNotFound
}

func (b *InputBox) beginMutationLocked() *inputBoxMutation {
	b.mutationSeq++
	if b.mutationSeq == 0 {
		b.mutationSeq++
	}
	b.pendingMutation = b.mutationSeq
	b.mutationBefore = append([]InputRecord(nil), b.items...)
	return &inputBoxMutation{seq: b.pendingMutation, previousItems: append([]InputRecord(nil), b.items...), previousQueueRevision: b.queueRevision}
}

// CommitMutation releases the consumer after the changed queue state has been
// persisted. It is safe to call only with the token returned by Begin*.
func (b *InputBox) CommitMutation(mutation *inputBoxMutation) bool {
	if b == nil || mutation == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pendingMutation != mutation.seq {
		return false
	}
	b.pendingMutation = 0
	b.mutationBefore = nil
	b.signalLocked()
	return true
}

// RollbackMutation restores the exact queued order if persistence failed.
func (b *InputBox) RollbackMutation(mutation *inputBoxMutation) bool {
	if b == nil || mutation == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pendingMutation != mutation.seq {
		return false
	}
	b.items = append([]InputRecord(nil), mutation.previousItems...)
	b.queueRevision = mutation.previousQueueRevision
	b.pendingMutation = 0
	b.mutationBefore = nil
	b.signalLocked()
	return true
}

// InFlight returns the input currently owned by the consumer. It is kept out
// of the FIFO tail while a Turn is running, but remains durable so a process
// restart can distinguish an accepted input from an unconsumed one.
func (b *InputBox) InFlight() (InputRecord, bool) {
	if b == nil {
		return InputRecord{}, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.inFlight == nil {
		return InputRecord{}, false
	}
	return *b.inFlight, true
}

// PendingUserInputs returns user messages that are queued or currently being
// claimed. It excludes triggers and child-agent inputs from the user-facing
// transcript projection.
func (b *InputBox) PendingUserInputs() []InputRecord {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	items := b.items
	if b.pendingMutation != 0 {
		items = b.mutationBefore
	}
	out := make([]InputRecord, 0, len(items)+1)
	for _, item := range items {
		if item.Kind == InputKindUser && !b.isDeferredLocked(item.Seq) {
			out = append(out, item)
		}
	}
	if b.inFlight != nil && b.inFlight.Kind == InputKindUser && !b.inFlight.Completed {
		record := *b.inFlight
		record.Claimed = true
		out = append(out, record)
	}
	return out
}

func (b *InputBox) QueuedUserInput(seq uint64) (InputRecord, error) {
	if b == nil {
		return InputRecord{}, errors.New("input box unavailable")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, item := range b.items {
		if item.Seq != seq {
			continue
		}
		if item.Kind != InputKindUser || b.isDeferredLocked(seq) || b.pendingMutation != 0 {
			return InputRecord{}, ErrInputNotQueued
		}
		return item, nil
	}
	if b.inFlight != nil && b.inFlight.Seq == seq {
		return InputRecord{}, ErrInputNotQueued
	}
	return InputRecord{}, ErrInputNotFound
}

func pendingUserInputsFromSnapshot(raw []byte) []InputRecord {
	if len(raw) == 0 {
		return nil
	}
	var state inputBoxState
	if json.Unmarshal(raw, &state) != nil {
		return nil
	}
	out := make([]InputRecord, 0, len(state.Items)+1)
	for _, item := range state.Items {
		if item.Kind == InputKindUser {
			out = append(out, item)
		}
	}
	if state.InFlight != nil && state.InFlight.Kind == InputKindUser && !state.InFlight.Completed {
		record := *state.InFlight
		record.Claimed = true
		out = append(out, record)
	}
	return out
}

func inputRecordsFromQueueRows(rows []store.InputQueueRow) ([]InputRecord, error) {
	ordered := append([]store.InputQueueRow(nil), rows...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].InputSeq < ordered[j].InputSeq })
	items := make([]InputRecord, 0, len(ordered))
	for _, row := range ordered {
		var record InputRecord
		if err := json.Unmarshal(row.PayloadJSON, &record); err != nil {
			return nil, fmt.Errorf("decode persisted input %d: %w", row.InputSeq, err)
		}
		if record.Seq != row.InputSeq || record.Revision != row.InputRevision || string(record.Kind) != row.Kind {
			return nil, fmt.Errorf("persisted input row %d identity mismatch", row.InputSeq)
		}
		if err := validateInputRecord(record); err != nil {
			return nil, fmt.Errorf("invalid persisted input %d: %w", row.InputSeq, err)
		}
		if row.Status == "claimed" {
			record.Claimed = true
		} else if row.Status != "queued" {
			return nil, fmt.Errorf("invalid persisted input status %q", row.Status)
		}
		items = append(items, record)
	}
	return items, nil
}

func pendingUserInputsFromQueueRows(rows []store.InputQueueRow) ([]InputRecord, error) {
	items, err := inputRecordsFromQueueRows(rows)
	if err != nil {
		return nil, err
	}
	out := make([]InputRecord, 0, len(items))
	for _, item := range items {
		if item.Kind == InputKindUser {
			out = append(out, item)
		}
	}
	return out, nil
}

func (b *InputBox) isDeferredLocked(seq uint64) bool {
	_, ok := b.deferred[seq]
	return ok
}

// MarkCompleted records that the consumer finished the input's Turn. The
// record is cleared only after this marker has been persisted together with
// the resulting history, which closes the crash window between execution and
// acknowledgement.
func (b *InputBox) MarkCompleted(seq uint64) bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.inFlight == nil || b.inFlight.Seq != seq {
		return false
	}
	b.inFlight.Completed = true
	b.queueRevision++
	for requestID, receipt := range b.receipts {
		if receipt.InputSeq == seq {
			receipt.InputRevision = b.inFlight.Revision
			receipt.Status = "consumed"
			b.receipts[requestID] = receipt
		}
	}
	return true
}

// Ack clears a completed or otherwise discarded in-flight input.
func (b *InputBox) Ack(seq uint64) bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.inFlight == nil || b.inFlight.Seq != seq {
		return false
	}
	b.inFlight = nil
	b.queueRevision++
	return true
}

// RestoreCompletedInFlight reinstates the ownership guard when the
// post-ack snapshot cannot be persisted. The consumer must stop so a later
// input cannot overwrite this unresolved recovery boundary.
func (b *InputBox) RestoreCompletedInFlight(record InputRecord) bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.inFlight != nil {
		return false
	}
	record.Completed = true
	b.inFlight = &record
	b.queueRevision++
	return true
}

// RequeueInFlight puts an uncompleted input back at the head of the FIFO.
// It is used when startup finds that the consumer had not entered a live Turn
// before the process stopped.
func (b *InputBox) RequeueInFlight() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.inFlight == nil {
		return false
	}
	record := *b.inFlight
	record.Completed = false
	b.items = append([]InputRecord{record}, b.items...)
	b.inFlight = nil
	b.queueRevision++
	select {
	case b.wake <- struct{}{}:
	default:
	}
	return true
}

func (b *InputBox) Len() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.items)
}

func (b *InputBox) HasInFlight() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.inFlight != nil
}

// Snapshot returns the durable FIFO tail.  The monotonic sequence is stored
// even when Items is empty, so a restart can never reuse an input sequence.
func (b *InputBox) Snapshot() json.RawMessage {
	return b.snapshot(true)
}

// SnapshotForPersistence stores only queue watermarks. Input payloads and retry
// receipts are stored in their indexed tables.
func (b *InputBox) SnapshotForPersistence() json.RawMessage {
	raw, _, _, _ := b.PersistenceState()
	return raw
}

// PersistenceState returns a consistent queue header and row projection. The
// caller persists them in one SQLite transaction with runtime metadata.
func (b *InputBox) PersistenceState() (json.RawMessage, []store.InputQueueRow, uint64, uint64) {
	if b == nil {
		return nil, nil, 0, 0
	}
	b.mu.Lock()
	state := inputBoxState{Seq: b.seq, ConversationEpoch: b.conversationEpoch, QueueRevision: b.queueRevision, QueuePaused: b.queuePaused, QueueControlRevision: b.queueControlRevision, QueueStorageVersion: 1}
	var rows []store.InputQueueRow
	if b.persistedQueueEpoch != b.conversationEpoch || b.persistedQueueRevision != b.queueRevision {
		rows = make([]store.InputQueueRow, 0, len(b.items)+1)
		for _, item := range b.items {
			payload, err := json.Marshal(item)
			if err != nil {
				b.mu.Unlock()
				return nil, nil, 0, 0
			}
			rows = append(rows, store.InputQueueRow{ConversationEpoch: b.conversationEpoch, InputSeq: item.Seq, InputRevision: item.Revision, Kind: string(item.Kind), Status: "queued", PayloadJSON: payload})
		}
		if b.inFlight != nil && !b.inFlight.Completed {
			payload, err := json.Marshal(*b.inFlight)
			if err != nil {
				b.mu.Unlock()
				return nil, nil, 0, 0
			}
			rows = append(rows, store.InputQueueRow{ConversationEpoch: b.conversationEpoch, InputSeq: b.inFlight.Seq, InputRevision: b.inFlight.Revision, Kind: string(b.inFlight.Kind), Status: "claimed", PayloadJSON: payload})
		}
	}
	var completedSeq, completedRevision uint64
	if b.inFlight != nil && b.inFlight.Completed {
		completedSeq, completedRevision = b.inFlight.Seq, b.inFlight.Revision
	}
	b.mu.Unlock()
	if rows != nil {
		sort.Slice(rows, func(i, j int) bool { return rows[i].InputSeq < rows[j].InputSeq })
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return nil, nil, 0, 0
	}
	return raw, rows, completedSeq, completedRevision
}

// MarkPersistenceCommitted records which row projection reached durable
// storage. If the live queue advanced while the transaction was committing,
// the next snapshot still observes the newer revision and includes its rows.
func (b *InputBox) MarkPersistenceCommitted(raw []byte) bool {
	if b == nil || len(raw) == 0 {
		return false
	}
	var state inputBoxState
	if err := json.Unmarshal(raw, &state); err != nil || state.QueueStorageVersion != 1 || state.ConversationEpoch == 0 || state.QueueRevision == 0 {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if state.ConversationEpoch > b.persistedQueueEpoch ||
		(state.ConversationEpoch == b.persistedQueueEpoch && state.QueueRevision > b.persistedQueueRevision) {
		b.persistedQueueEpoch = state.ConversationEpoch
		b.persistedQueueRevision = state.QueueRevision
	}
	return true
}

func (b *InputBox) snapshot(includeReceipts bool) json.RawMessage {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	state := inputBoxState{
		Seq:                  b.seq,
		ConversationEpoch:    b.conversationEpoch,
		QueueRevision:        b.queueRevision,
		QueuePaused:          b.queuePaused,
		QueueControlRevision: b.queueControlRevision,
		Items:                append([]InputRecord(nil), b.items...),
	}
	if includeReceipts {
		state.Receipts = make(map[string]InputReceipt, len(b.receipts))
		for requestID, receipt := range b.receipts {
			state.Receipts[requestID] = receipt
		}
	}
	if b.inFlight != nil {
		record := *b.inFlight
		state.InFlight = &record
	}
	b.mu.Unlock()
	raw, err := json.Marshal(state)
	if err != nil {
		return nil
	}
	return raw
}

func InputReceiptsFromSnapshot(raw []byte) ([]InputReceipt, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var state inputBoxState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("decode input box state: %w", err)
	}
	receipts := make([]InputReceipt, 0, len(state.Receipts))
	for _, receipt := range state.Receipts {
		receipts = append(receipts, receipt)
	}
	return receipts, nil
}

func (b *InputBox) ForgetReceipt(requestID string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	delete(b.receipts, strings.TrimSpace(requestID))
	b.mu.Unlock()
}

func (b *InputBox) ClearReceiptCache() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.receipts = make(map[string]InputReceipt)
	b.mu.Unlock()
}

func (b *InputBox) ProjectReceipt(receipt InputReceipt, conversationEpoch uint64) (InputReceipt, bool, error) {
	if b == nil {
		return InputReceipt{}, false, errors.New("input box unavailable")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if conversationEpoch == 0 || conversationEpoch != b.conversationEpoch || receipt.ConversationEpoch != conversationEpoch {
		return InputReceipt{}, false, ErrInputConversationChanged
	}
	return b.currentReceiptLocked(receipt), true, nil
}

// Restore replaces the in-memory FIFO with a previously persisted tail.
// Restore is used only during runtime construction, before the consumer is
// started; it deliberately does not restore the closed bit.
func (b *InputBox) Restore(raw []byte) error {
	if b == nil {
		return errors.New("input box unavailable")
	}
	if len(raw) == 0 {
		return nil
	}
	var state inputBoxState
	if err := json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf("decode input box state: %w", err)
	}
	for i := range state.Items {
		if state.Items[i].Revision == 0 {
			state.Items[i].Revision = 1
		}
		if state.Items[i].Kind == InputKindTrigger && state.Items[i].Env.DeliveryID == "" {
			state.Items[i].RecoveredLegacy = true
		}
	}
	if state.InFlight != nil && state.InFlight.Kind == InputKindTrigger && state.InFlight.Env.DeliveryID == "" {
		state.InFlight.RecoveredLegacy = true
	}
	if len(state.Items) > InputBoxMaxItems {
		return fmt.Errorf("input box state exceeds %d items", InputBoxMaxItems)
	}
	if state.ConversationEpoch == 0 {
		state.ConversationEpoch = 1
	}
	if state.QueueControlRevision == 0 {
		state.QueueControlRevision = 1
	}
	if state.Receipts == nil {
		state.Receipts = make(map[string]InputReceipt)
	}
	for requestID, receipt := range state.Receipts {
		if strings.TrimSpace(requestID) == "" || len(requestID) > 128 || receipt.ClientRequestID != requestID || strings.TrimSpace(receipt.PayloadDigest) == "" || receipt.ConversationEpoch == 0 || receipt.InputSeq == 0 || receipt.InputRevision == 0 {
			return fmt.Errorf("input box state contains invalid receipt %q", requestID)
		}
		if receipt.Status != "queued" && receipt.Status != "claimed" && receipt.Status != "consumed" && receipt.Status != "cancelled" {
			return fmt.Errorf("input box state contains invalid receipt status %q", receipt.Status)
		}
	}
	maxSeq := state.Seq
	seen := make(map[uint64]struct{}, len(state.Items)+1)
	for _, item := range state.Items {
		if item.Seq == 0 {
			return errors.New("input box state contains zero sequence")
		}
		if _, ok := seen[item.Seq]; ok {
			return fmt.Errorf("input box state contains duplicate sequence %d", item.Seq)
		}
		seen[item.Seq] = struct{}{}
		if err := validateInputRecord(item); err != nil {
			return fmt.Errorf("invalid input box item %d: %w", item.Seq, err)
		}
		if item.Seq > maxSeq {
			maxSeq = item.Seq
		}
	}
	if state.InFlight != nil {
		if state.InFlight.Revision == 0 {
			state.InFlight.Revision = 1
		}
		if state.InFlight.Seq == 0 {
			return errors.New("input box state contains zero in-flight sequence")
		}
		if _, ok := seen[state.InFlight.Seq]; ok {
			return fmt.Errorf("input box state contains duplicate sequence %d", state.InFlight.Seq)
		}
		if err := validateInputRecord(*state.InFlight); err != nil {
			return fmt.Errorf("invalid in-flight input %d: %w", state.InFlight.Seq, err)
		}
		if state.InFlight.Seq > maxSeq {
			maxSeq = state.InFlight.Seq
		}
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errors.New("input box closed")
	}
	b.seq = maxSeq
	b.conversationEpoch = state.ConversationEpoch
	b.queueRevision = state.QueueRevision
	if b.queueRevision == 0 {
		b.queueRevision = 1
	}
	if state.QueueStorageVersion == 1 {
		b.persistedQueueEpoch = state.ConversationEpoch
		b.persistedQueueRevision = b.queueRevision
	} else {
		b.persistedQueueEpoch = 0
		b.persistedQueueRevision = 0
	}
	b.queuePaused = state.QueuePaused
	b.queueControlRevision = state.QueueControlRevision
	if b.queueControlRevision == 0 {
		b.queueControlRevision = 1
	}
	b.items = append([]InputRecord(nil), state.Items...)
	b.receipts = state.Receipts
	b.deferred = make(map[uint64]struct{})
	b.inFlight = nil
	if state.InFlight != nil {
		record := *state.InFlight
		b.inFlight = &record
	}
	b.mu.Unlock()
	if len(state.Items) > 0 {
		b.Signal()
	}
	return nil
}

// RestoreQueueRows hydrates a versioned mailbox header with its indexed input
// payloads. The row table is authoritative once queue_storage_version is 1.
func (b *InputBox) RestoreQueueRows(raw []byte, rows []store.InputQueueRow) error {
	var state inputBoxState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &state); err != nil {
			return fmt.Errorf("decode input queue header: %w", err)
		}
	}
	if state.QueueStorageVersion != 1 {
		return fmt.Errorf("input queue snapshot is not row-backed")
	}
	if state.ConversationEpoch == 0 {
		return fmt.Errorf("input queue snapshot has no conversation epoch")
	}
	if len(rows) > InputBoxMaxItems+1 {
		return fmt.Errorf("input queue has too many persisted rows")
	}
	rows = append([]store.InputQueueRow(nil), rows...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].InputSeq < rows[j].InputSeq })
	state.Items = nil
	state.InFlight = nil
	state.Receipts = nil
	for _, row := range rows {
		if row.ConversationEpoch != state.ConversationEpoch || row.InputSeq > state.Seq {
			return fmt.Errorf("persisted input row is outside the mailbox watermark")
		}
		var record InputRecord
		if err := json.Unmarshal(row.PayloadJSON, &record); err != nil {
			return fmt.Errorf("decode persisted input %d: %w", row.InputSeq, err)
		}
		if record.Seq != row.InputSeq || record.Revision != row.InputRevision || string(record.Kind) != row.Kind {
			return fmt.Errorf("persisted input row %d identity mismatch", row.InputSeq)
		}
		if err := validateInputRecord(record); err != nil {
			return fmt.Errorf("invalid persisted input %d: %w", row.InputSeq, err)
		}
		switch row.Status {
		case "queued":
			state.Items = append(state.Items, record)
		case "claimed":
			if state.InFlight != nil {
				return fmt.Errorf("multiple claimed inputs in persisted queue")
			}
			state.InFlight = &record
		default:
			return fmt.Errorf("invalid persisted input status %q", row.Status)
		}
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return b.Restore(encoded)
}

// DropStale removes inputs accepted before a context epoch boundary while
// preserving the monotonic sequence for newer inputs racing with the clear.
func (b *InputBox) DropStale(epoch uint64) int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	kept := b.items[:0]
	dropped := 0
	for _, item := range b.items {
		if item.Env.SessionEpoch != epoch {
			delete(b.deferred, item.Seq)
			dropped++
			continue
		}
		kept = append(kept, item)
	}
	b.items = kept
	changed := dropped > 0
	if b.inFlight != nil && b.inFlight.Env.SessionEpoch != epoch {
		b.inFlight = nil
		dropped++
		changed = true
	}
	if epoch > 0 {
		if epoch > b.conversationEpoch {
			// Receipts are scoped to one conversation. Advancing the epoch itself
			// fences every late old request, so retaining those receipts would only
			// grow runtime snapshots across repeated clears.
			b.receipts = make(map[string]InputReceipt)
			if b.queuePaused {
				b.queuePaused = false
				if b.queueControlRevision < ^uint64(0) {
					b.queueControlRevision++
				}
			}
			changed = true
		}
		b.conversationEpoch = epoch
	}
	if changed {
		b.queueRevision++
	}
	return dropped
}

func (b *InputBox) Wake() <-chan struct{} {
	if b == nil {
		return nil
	}
	return b.wake
}

// Signal wakes a consumer after a Turn changes from busy to idle.  It is
// separate from Append because an input may arrive while the current Turn is
// waiting for approval and must remain buffered until resume/cancel.
func (b *InputBox) Signal() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

func (b *InputBox) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

func (b *InputBox) Closed() bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}
