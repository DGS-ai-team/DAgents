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
	InputKindUser    InputKind = "user"
	InputKindTrigger InputKind = "trigger"
	InputKindA2A     InputKind = "a2a"
)

// InputRecord is the FIFO record accepted by a session.  Seq is assigned at
// append time and is never reused.  The queue.Envelope is retained as a
// compatibility transport while callers migrate away from MessageQueue.
type InputRecord struct {
	Seq  uint64         `json:"seq"`
	Kind InputKind      `json:"kind"`
	Env  queue.Envelope `json:"env"`
}

type inputBoxState struct {
	Seq   uint64        `json:"seq"`
	Items []InputRecord `json:"items,omitempty"`
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
	mu     sync.Mutex
	seq    uint64
	items  []InputRecord
	wake   chan struct{}
	closed bool
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
	b.seq++
	record := InputRecord{Seq: b.seq, Kind: kind, Env: env}
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
	return record, true
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
	maxSeq := state.Seq
	for _, item := range state.Items {
		if item.Seq == 0 {
			return errors.New("input box state contains zero sequence")
		}
		if item.Seq > maxSeq {
			maxSeq = item.Seq
		}
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errors.New("input box closed")
	}
	b.seq = maxSeq
	b.items = append([]InputRecord(nil), state.Items...)
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
