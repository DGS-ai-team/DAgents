package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func TestManagerRebuildBindsConditionCallbacksBeforeRuntimeStart(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewManager("agent-1", stream.NewHub(8, logx.Discard()), &llm.MockClient{}, nil, nil, st, TurnOptions{}, logx.Discard())
	defer func() { mgr.Stop(); _ = st.Close() }()
	mgr.SetConditionValidator(func(context.Context, turn.ConditionApprovalMetadata) error { return nil })
	mgr.SetConditionCompletionCallback(func(triggers.ConditionRequest, triggers.ConditionResult) error { return nil })
	r, _, err := mgr.CreateWithOptionsAndLLM("condition-runtime", TurnOptions{}, nil, nil, &llm.MockClient{}, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if mgr.getRuntime(r.ID).conditionValidator == nil || mgr.getRuntime(r.ID).conditionCompletion == nil {
		t.Fatal("initial runtime lost condition bindings")
	}
	if _, _, err := mgr.ReplaceWithOptionsAndLLM(r.ID, TurnOptions{}, nil, nil, &llm.MockClient{}, "agent-1"); err != nil {
		t.Fatal(err)
	}
	if mgr.getRuntime(r.ID).conditionValidator == nil || mgr.getRuntime(r.ID).conditionCompletion == nil {
		t.Fatal("replaced runtime lost condition bindings")
	}
}

func TestConditionValidatorAllowsValidApprovalWritesMarker(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "valid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	r := newConditionLifecycleRuntime(t, st, "condition-valid")
	triggerStore, err := triggers.OpenStore(filepath.Join(t.TempDir(), "triggers.json"), 20)
	if err != nil {
		t.Fatal(err)
	}
	d, err := triggers.NewDefinitionFromCreate(triggers.CreateInput{Name: "condition", TargetAgentID: "agent-1", TargetSessionID: strPtr(r.session.ID), TaskTemplate: "x", Condition: map[string]any{"interval_seconds": 60, "cmd": "touch marker"}}, "agent-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	d, err = triggerStore.CreateTrigger(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := triggerStore.ClaimDelivery(d.TriggerID, "delivery-valid", r.session.ID); err != nil {
		t.Fatal(err)
	}
	r.conditionValidator = func(_ context.Context, meta turn.ConditionApprovalMetadata) error {
		current, ok := triggerStore.GetTrigger(meta.TriggerID)
		if !ok {
			return fmt.Errorf("missing trigger")
		}
		return triggers.ValidateConditionIdentity(*current, meta.AgentID, meta.SessionID, meta.DeliveryID, meta.TriggerRevision, meta.Occurrence)
	}
	marker := filepath.Join(r.workspaceRoot, "valid-marker")
	req := conditionRequest("delivery-valid")
	req.Metadata.TriggerID, req.Metadata.SessionID = d.TriggerID, r.session.ID
	req.Metadata.TriggerRevision = d.Revision
	req.Command = "touch " + marker
	if err := r.requestConditionApproval(req); err != nil {
		t.Fatal(err)
	}
	_, err = r.handleConditionResume(context.Background(), map[string]any{"type": "approve"}, r.pendingSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("valid condition marker missing: %v", err)
	}
}

func TestConditionValidatorDisabledApprovalDoesNotRunMarker(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "disabled.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	r := newConditionLifecycleRuntime(t, st, "condition-disabled")
	triggerStore, err := triggers.OpenStore(filepath.Join(t.TempDir(), "triggers.json"), 20)
	if err != nil {
		t.Fatal(err)
	}
	d, err := triggers.NewDefinitionFromCreate(triggers.CreateInput{Name: "condition", TargetAgentID: "agent-1", TargetSessionID: strPtr(r.session.ID), TaskTemplate: "x", Condition: map[string]any{"interval_seconds": 60, "cmd": "touch marker"}}, "agent-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	d, err = triggerStore.CreateTrigger(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := triggerStore.ClaimDelivery(d.TriggerID, "delivery-disabled", r.session.ID); err != nil {
		t.Fatal(err)
	}
	r.conditionValidator = func(_ context.Context, meta turn.ConditionApprovalMetadata) error {
		current, ok := triggerStore.GetTrigger(meta.TriggerID)
		if !ok {
			return fmt.Errorf("missing trigger")
		}
		return triggers.ValidateConditionIdentity(*current, meta.AgentID, meta.SessionID, meta.DeliveryID, meta.TriggerRevision, meta.Occurrence)
	}
	marker := filepath.Join(r.workspaceRoot, "disabled-marker")
	req := conditionRequest("delivery-disabled")
	req.Metadata.TriggerID = d.TriggerID
	req.Metadata.SessionID = r.session.ID
	req.Command = "touch " + marker
	if err := r.requestConditionApproval(req); err != nil {
		t.Fatal(err)
	}
	falseValue := false
	if _, err := triggerStore.UpdateTrigger(d.TriggerID, triggers.UpdatePatch{Enabled: &falseValue}, time.Now()); err != nil {
		t.Fatal(err)
	}
	outcome, err := r.handleConditionResume(context.Background(), map[string]any{"type": "approve"}, r.pendingSnapshot())
	if err == nil || outcome.ConditionHandled {
		t.Fatalf("disabled approval outcome=%+v err=%v", outcome, err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("disabled condition ran marker: %v", statErr)
	}
}

func TestConditionValidatorRevisionChangeDoesNotRunMarker(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "revision.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	r := newConditionLifecycleRuntime(t, st, "condition-revision")
	triggerStore, err := triggers.OpenStore(filepath.Join(t.TempDir(), "triggers.json"), 20)
	if err != nil {
		t.Fatal(err)
	}
	d, err := triggers.NewDefinitionFromCreate(triggers.CreateInput{Name: "condition", TargetAgentID: "agent-1", TargetSessionID: strPtr(r.session.ID), TaskTemplate: "x", Condition: map[string]any{"interval_seconds": 60, "cmd": "touch marker"}}, "agent-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	d, err = triggerStore.CreateTrigger(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := triggerStore.ClaimDelivery(d.TriggerID, "delivery-revision", r.session.ID); err != nil {
		t.Fatal(err)
	}
	r.conditionValidator = func(_ context.Context, meta turn.ConditionApprovalMetadata) error {
		current, ok := triggerStore.GetTrigger(meta.TriggerID)
		if !ok {
			return fmt.Errorf("missing trigger")
		}
		return triggers.ValidateConditionIdentity(*current, meta.AgentID, meta.SessionID, meta.DeliveryID, meta.TriggerRevision, meta.Occurrence)
	}
	marker := filepath.Join(r.workspaceRoot, "revision-marker")
	req := conditionRequest("delivery-revision")
	req.Metadata.TriggerID = d.TriggerID
	req.Metadata.SessionID = r.session.ID
	req.Metadata.TriggerRevision = d.Revision
	req.Command = "touch " + marker
	if err := r.requestConditionApproval(req); err != nil {
		t.Fatal(err)
	}
	name := "changed"
	if _, err := triggerStore.UpdateTrigger(d.TriggerID, triggers.UpdatePatch{Name: &name}, time.Now()); err != nil {
		t.Fatal(err)
	}
	outcome, err := r.handleConditionResume(context.Background(), map[string]any{"type": "approve"}, r.pendingSnapshot())
	if err == nil || outcome.ConditionHandled {
		t.Fatalf("stale approval outcome=%+v err=%v", outcome, err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("stale condition ran marker: %v", statErr)
	}
}
