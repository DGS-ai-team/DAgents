package session

import (
	"context"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

func TestRunDreamingWaitingCancelPersistsCancelledAttempt(t *testing.T) {
	client := &dreamingResumeClient{}
	mgr, _, _, _, sessionID, cleanup := dreamingResumeFixture(t, client, policy.ModeAlways)
	defer cleanup()
	leaseCtx, release, ok, err := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1")
	if err != nil || !ok {
		t.Fatalf("maintenance lease: ok=%v err=%v", ok, err)
	}
	_, runErr := mgr.RunDreaming(leaseCtx, sessionID, "取消等待", 3)
	release()
	if runErr == nil {
		t.Fatal("dreaming unexpectedly completed")
	}
	rt := mgr.getRuntime(sessionID)
	if rt == nil || !rt.cancelTurn() {
		t.Fatal("waiting dreaming was not cancellable")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		attempt, found, _ := mgr.GetDreamingAttempt(sessionID)
		if found && attempt.State == DreamingAttemptCancelled {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	attempt, found, _ := mgr.GetDreamingAttempt(sessionID)
	t.Fatalf("cancelled dreaming attempt not persisted: found=%v attempt=%+v", found, attempt)
}
