package notify

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/pending"
)

func TestPlanSync_retainPreventsRefocusRepush(t *testing.T) {
	entry := pending.Entry{
		AgentID:   "agt-1",
		HITLItems: 1,
	}
	last := map[string]pending.Entry{"agt-1": entry}

	// UI focused: toast list empty, but retain the agent.
	plan := PlanSync(last, nil, map[string]struct{}{"agt-1": {}})
	if len(plan.ToPush) != 0 {
		t.Fatalf("focused should not push: %+v", plan.ToPush)
	}
	if _, ok := plan.NextLast["agt-1"]; !ok {
		t.Fatal("retain must keep last snapshot")
	}

	// Unfocus with same pending: should not re-push.
	plan2 := PlanSync(plan.NextLast, []pending.Entry{entry}, nil)
	if len(plan2.ToPush) != 0 {
		t.Fatalf("identical pending after unfocus must not re-push: %+v", plan2.ToPush)
	}
}

func TestPlanSync_unfocusWithoutPriorToastStillPushes(t *testing.T) {
	entry := pending.Entry{AgentID: "agt-2", HITLItems: 1}
	// Arrived while focused: never pushed, last empty, only retain.
	plan := PlanSync(nil, nil, map[string]struct{}{"agt-2": {}})
	if len(plan.NextLast) != 0 {
		t.Fatalf("retain without prior last should stay empty: %+v", plan.NextLast)
	}
	plan2 := PlanSync(plan.NextLast, []pending.Entry{entry}, nil)
	if len(plan2.ToPush) != 1 {
		t.Fatalf("first unfocus with pending should push: %+v", plan2.ToPush)
	}
}

func TestPlanSync_clearedPendingDropsLast(t *testing.T) {
	last := map[string]pending.Entry{
		"agt-1": {AgentID: "agt-1", HITLItems: 1},
	}
	plan := PlanSync(last, nil, nil)
	if len(plan.NextLast) != 0 {
		t.Fatalf("cleared pending should drop last: %+v", plan.NextLast)
	}
}
