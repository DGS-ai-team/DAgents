package session

import (
	"context"
	"fmt"
)

func (r *runtime) setInputQueuePaused(paused bool, expectedRevision uint64) (uint64, bool, error) {
	if r == nil || r.inputBox == nil {
		return 0, false, fmt.Errorf("input queue unavailable")
	}
	// Serialize with accepted inputs and clear, then with the same gate used by
	// the consumer's claim decision. Persist while both are held so a stop can
	// never return before the queue pause is durable.
	r.ingressMu.Lock()
	defer r.ingressMu.Unlock()
	gate := r.executionGate
	if gate != nil {
		gate.mu.Lock()
		defer gate.mu.Unlock()
	}
	r.persistMu.Lock()

	previousPaused, previousRevision := r.inputBox.QueueControlState()
	revision, changed, err := r.inputBox.SetQueuePaused(paused, expectedRevision)
	if err != nil || !changed {
		r.persistMu.Unlock()
		return revision, false, err
	}
	if r.store != nil && !r.isChildSession() {
		if err := r.persistLocked(context.Background()); err != nil {
			r.inputBox.RestoreQueueControl(previousPaused, previousRevision)
			r.persistMu.Unlock()
			return previousRevision, false, fmt.Errorf("persist input queue control: %w", err)
		}
	}
	r.persistMu.Unlock()
	r.publishInputQueueSnapshot()
	if gate != nil {
		gate.notify()
	}
	if !paused {
		r.inputBox.Signal()
	}
	return revision, true, nil
}
