package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/queue"
)

// InputKind identifies an external input which may start a Turn.  Control
// operations such as resume and cancel deliberately do not use InputBox:
// they operate on the currently active Turn rather than becoming transcript
// messages.
type InputKind string

const (
	InputKindUser       InputKind = "user"
	InputKindTrigger    InputKind = "trigger"
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
	ErrInputBoxFull      = errors.New("input box is full")
	ErrInputRecordTooBig = errors.New("input record is too large")
	ErrInvalidInputKind  = errors.New("invalid input kind")
	ErrInputSequenceFull = errors.New("input sequence exhausted")
)

func validateInputRecord(record InputRecord) error {
	if record.Kind != InputKindUser && record.Kind != InputKindTrigger && record.Kind != InputKindChildAgent {
		return fmt.Errorf("%w: %q", ErrInvalidInputKind, record.Kind)
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
	Seq       uint64         `json:"seq"`
	Kind      InputKind      `json:"kind"`
	Env       queue.Envelope `json:"env"`
	Completed bool           `json:"completed,omitempty"`
}

type inputBoxState struct {
	Seq      uint64        `json:"seq"`
	Items    []InputRecord `json:"items,omitempty"`
	InFlight *InputRecord  `json:"in_flight,omitempty"`
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

// InputBox is a per-session FIFO mailbox for external inputs.  It does not
// execute turns, apply history, or reorder records.  Wake is intentionally a
// coalesced notification; consumers must always drain with Peek/Pop so a
// notification cannot represent the data itself.
type InputBox struct {
	mu       sync.Mutex
	seq      uint64
	items    []InputRecord
	inFlight *InputRecord
	wake     chan struct{}
	closed   bool
}

func NewInputBox() *InputBox {
	return &InputBox{wake: make(chan struct{}, 1)}
}

// Append accepts one external input and returns its monotonic session seq.
func (b *InputBox) Append(kind InputKind, env queue.Envelope) (uint64, error) {
	if b == nil {
		return 0, errors.New("input box unavailable")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, errors.New("input box closed")
	}
	if len(b.items) >= InputBoxMaxItems {
		return 0, ErrInputBoxFull
	}
	record := InputRecord{Seq: b.seq + 1, Kind: kind, Env: env}
	if record.Seq == 0 {
		return 0, ErrInputSequenceFull
	}
	if err := validateInputRecord(record); err != nil {
		return 0, err
	}
	b.seq = record.Seq
	b.items = append(b.items, record)
	select {
	case b.wake <- struct{}{}:
	default:
	}
	return record.Seq, nil
}

func (b *InputBox) Peek() (InputRecord, bool) {
	if b == nil {
		return InputRecord{}, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.items) == 0 {
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
	if len(b.items) == 0 {
		return InputRecord{}, false
	}
	record := b.items[0]
	b.items = b.items[1:]
	b.inFlight = &record
	return record, true
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

// Snapshot returns the durable FIFO tail.  The monotonic sequence is stored
// even when Items is empty, so a restart can never reuse an input sequence.
func (b *InputBox) Snapshot() json.RawMessage {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	state := inputBoxState{
		Seq:   b.seq,
		Items: append([]InputRecord(nil), b.items...),
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
	if len(state.Items) > InputBoxMaxItems {
		return fmt.Errorf("input box state exceeds %d items", InputBoxMaxItems)
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
	b.items = append([]InputRecord(nil), state.Items...)
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
		if item.Env.SessionEpoch != 0 && item.Env.SessionEpoch != epoch {
			dropped++
			continue
		}
		kept = append(kept, item)
	}
	b.items = kept
	if b.inFlight != nil && b.inFlight.Env.SessionEpoch != 0 && b.inFlight.Env.SessionEpoch != epoch {
		b.inFlight = nil
		dropped++
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
