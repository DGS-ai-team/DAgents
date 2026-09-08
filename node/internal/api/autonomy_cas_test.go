package api

import (
	"net/http"
	"reflect"
	"testing"
)

// TestAutonomyPUTChecksBothRevisionsOverHTTP exercises the public PUT path.
// A stale profile or cycle revision must be rejected before either store is
// changed; a request carrying both current revisions is accepted.
func TestAutonomyPUTChecksBothRevisionsOverHTTP(t *testing.T) {
	srv, _ := autonomyRegressionServer(t)
	if got := putAutonomy(t, srv, map[string]any{
		"objective": "initial", "acceptance": "evidence", "enabled": false,
	}); got.Code != http.StatusOK {
		t.Fatalf("initial PUT=%d %s", got.Code, got.Body.String())
	}

	p, ok := srv.goalStore.GetProfile("auto-reg")
	if !ok {
		t.Fatal("profile missing")
	}
	g, ok := srv.goalStore.Get(p.CurrentGoalID)
	if !ok {
		t.Fatal("current goal missing")
	}
	assertUnchanged := func(label string, beforeP, beforeG interface{}) {
		t.Helper()
		afterP, ok := srv.goalStore.GetProfile("auto-reg")
		if !ok {
			t.Fatalf("%s: profile disappeared", label)
		}
		afterG, ok := srv.goalStore.Get(g.ID)
		if !ok {
			t.Fatalf("%s: goal disappeared", label)
		}
		if !reflect.DeepEqual(beforeP, afterP) || !reflect.DeepEqual(beforeG, afterG) {
			t.Fatalf("%s changed persisted state: before profile=%#v goal=%#v after profile=%#v goal=%#v", label, beforeP, beforeG, afterP, afterG)
		}
	}

	profileStaleP, profileStaleG := p, g
	resp := putAutonomy(t, srv, map[string]any{
		"objective": "profile-stale", "acceptance": "evidence",
		"expected_revision": p.Revision + 1, "expected_goal_revision": g.Revision,
	})
	if resp.Code != http.StatusConflict {
		t.Fatalf("stale profile status=%d %s", resp.Code, resp.Body.String())
	}
	assertUnchanged("stale profile", profileStaleP, profileStaleG)

	goalStaleP, goalStaleG := p, g
	resp = putAutonomy(t, srv, map[string]any{
		"objective": "goal-stale", "acceptance": "evidence",
		"expected_revision": p.Revision, "expected_goal_revision": g.Revision + 1,
	})
	if resp.Code != http.StatusConflict {
		t.Fatalf("stale goal status=%d %s", resp.Code, resp.Body.String())
	}
	assertUnchanged("stale goal", goalStaleP, goalStaleG)

	resp = putAutonomy(t, srv, map[string]any{
		"objective": "updated", "acceptance": "updated evidence",
		"expected_revision": p.Revision, "expected_goal_revision": g.Revision,
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("matching revisions status=%d %s", resp.Code, resp.Body.String())
	}
	updatedP, ok := srv.goalStore.GetProfile("auto-reg")
	if !ok {
		t.Fatal("updated profile missing")
	}
	updatedG, ok := srv.goalStore.Get(g.ID)
	if !ok {
		t.Fatal("updated goal missing")
	}
	if updatedP.Revision != p.Revision+1 || updatedG.Revision != g.Revision+1 ||
		updatedG.Objective != "updated" || updatedG.Acceptance != "updated evidence" {
		t.Fatalf("matching update not applied: profile=%+v goal=%+v", updatedP, updatedG)
	}
}
