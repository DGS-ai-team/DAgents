package session

import (
	"context"
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/queue"
)

// agentExecutionGate serializes the decision to start maintenance or a turn.
// It is deliberately independent from Manager.mu: a dispatch owns the gate
// only until its input/control record has been claimed, never while running
// tools or the model.
type agentExecutionGate struct {
	mu                sync.Mutex
	maintenance       bool
	dispatches        int
	runtimes          map[*runtime]struct{}
	wake              chan struct{}
	maintenanceCancel context.CancelFunc
	maintenanceLease  *maintenanceLeaseToken
}

type maintenanceLeaseKey struct{}
type maintenanceLeaseToken struct{}

func newAgentExecutionGate() *agentExecutionGate {
	return &agentExecutionGate{runtimes: make(map[*runtime]struct{}), wake: make(chan struct{}, 1)}
}

func (g *agentExecutionGate) notify() {
	close(g.wake)
	g.wake = make(chan struct{}, 1)
}
func (g *agentExecutionGate) Wake() <-chan struct{} {
	g.mu.Lock()
	ch := g.wake
	g.mu.Unlock()
	return ch
}

func (g *agentExecutionGate) register(r *runtime) {
	if g == nil || r == nil {
		return
	}
	g.mu.Lock()
	g.runtimes[r] = struct{}{}
	g.mu.Unlock()
}
func (g *agentExecutionGate) unregister(r *runtime) {
	if g == nil || r == nil {
		return
	}
	g.mu.Lock()
	delete(g.runtimes, r)
	g.notify()
	g.mu.Unlock()
}

func (g *agentExecutionGate) idleLocked() bool {
	if g.maintenance || g.dispatches != 0 {
		return false
	}
	for r := range g.runtimes {
		if r.queue.Len() > 0 || r.inputBox.Len() > 0 || r.inputBox.HasInFlight() || r.turnState() != "idle" {
			return false
		}
	}
	return true
}

func (g *agentExecutionGate) acquireMaintenance(ctx context.Context) (func(), bool, error) {
	_, release, acquired, err := g.acquireMaintenanceContext(ctx)
	return release, acquired, err
}
func (g *agentExecutionGate) acquireMaintenanceContext(ctx context.Context) (context.Context, func(), bool, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, nil, false, ctx.Err()
		default:
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.idleLocked() {
		return nil, nil, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	leaseCtx, cancel := context.WithCancel(ctx)
	// The opaque lease marker proves ownership.  Observing maintenance=true is
	// insufficient because any caller could otherwise run against another
	// caller's lease.
	lease := &maintenanceLeaseToken{}
	leaseCtx = context.WithValue(leaseCtx, maintenanceLeaseKey{}, lease)
	g.maintenance = true
	g.maintenanceCancel = cancel
	g.maintenanceLease = lease
	var once sync.Once
	return leaseCtx, func() {
		once.Do(func() {
			g.mu.Lock()
			g.maintenance = false
			g.maintenanceCancel = nil
			g.maintenanceLease = nil
			cancel()
			g.notify()
			for r := range g.runtimes {
				r.signalInputBox()
			}
			g.mu.Unlock()
		})
	}, true, nil
}

func (g *agentExecutionGate) notifyInput() {
	g.mu.Lock()
	if g.maintenanceCancel != nil {
		g.maintenanceCancel()
	}
	g.mu.Unlock()
}
func (g *agentExecutionGate) waitSnapshot() (bool, <-chan struct{}) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.maintenance, g.wake
}

// claimControl and claimInput are the atomic consumer half of the protocol.
// A successful claim increments dispatches before releasing the gate, so a
// concurrent maintenance attempt cannot observe the Pop→active window.
func (g *agentExecutionGate) claimControl(ctx context.Context, r *runtime) (queue.Envelope, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.maintenance || r.queue.Len() == 0 {
		return queue.Envelope{}, false
	}
	env, err := r.queue.Dequeue(ctx)
	if err != nil {
		return queue.Envelope{}, false
	}
	g.dispatches++
	return env, true
}
func (g *agentExecutionGate) claimInput(r *runtime) (InputRecord, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.maintenance || r.turnState() != "idle" || r.queue.Len() > 0 {
		return InputRecord{}, false
	}
	rec, ok := r.inputBox.PopForIdle()
	if ok {
		g.dispatches++
	}
	return rec, ok
}
func (g *agentExecutionGate) finishDispatch() {
	g.mu.Lock()
	if g.dispatches > 0 {
		g.dispatches--
	}
	g.notify()
	g.mu.Unlock()
}
func (g *agentExecutionGate) owns(ctx context.Context) bool {
	if g == nil || ctx == nil {
		return false
	}
	v, ok := ctx.Value(maintenanceLeaseKey{}).(*maintenanceLeaseToken)
	if !ok || v == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.maintenance && g.maintenanceLease == v && ctx.Err() == nil
}
