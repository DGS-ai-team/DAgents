package triggers

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

type seqSubmitter struct {
	sessions []string
	lastEnv  capturedSubmit
}

type capturedSubmit struct {
	RequestType string
	Content     string
	TriggerID   string
}

func (s *seqSubmitter) EnsureSession(requestedID string) (string, error) {
	id := requestedID
	if id == "" {
		id = "sess-new"
	}
	s.sessions = append(s.sessions, id)
	return id, nil
}

func (s *seqSubmitter) SubmitTriggerMessage(sessionID, triggerID, content string) error {
	s.lastEnv = capturedSubmit{
		RequestType: "message",
		Content:     content,
		TriggerID:   triggerID,
	}
	return nil
}

func TestNewSessionBindsAfterFirstFire(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "t.json"), 20)
	if err != nil {
		t.Fatal(err)
	}
	def, err := NewDefinitionFromCreate(CreateInput{
		Name:              "job",
		TaskTemplate:      "run",
		Condition:         map[string]any{"interval_seconds": 60},
		SessionTargetMode: SessionTargetNewSession,
	}, "agent-1", time.Unix(200, 0))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTrigger(def); err != nil {
		t.Fatal(err)
	}
	sub := &seqSubmitter{}
	sched := NewScheduler(store, sub, 5)
	first, err := sched.FireTrigger(def.TriggerID, "manual", nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != FireStatusQueued {
		t.Fatalf("first status = %s", first.Status)
	}
	got, ok := store.GetTrigger(def.TriggerID)
	if !ok {
		t.Fatal("trigger missing")
	}
	if got.SessionTargetMode != SessionTargetFixed {
		t.Fatalf("mode = %q", got.SessionTargetMode)
	}
	if got.TargetSessionID == nil || *got.TargetSessionID != "sess-new" {
		t.Fatalf("target = %v", got.TargetSessionID)
	}
	store.ClearPendingDelivery(def.TriggerID)
	second, err := sched.FireTrigger(def.TriggerID, "manual", nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != FireStatusQueued {
		t.Fatalf("second status = %s", second.Status)
	}
	if len(sub.sessions) != 2 || sub.sessions[1] != "sess-new" {
		t.Fatalf("sessions = %v", sub.sessions)
	}
}

type staticResolver struct {
	id string
}

func (r staticResolver) ResolveLatestActiveUserSessionID(_ context.Context) (string, error) {
	return r.id, nil
}

func TestLatestActiveFireUsesResolver(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "t.json"), 20)
	if err != nil {
		t.Fatal(err)
	}
	def, err := NewDefinitionFromCreate(CreateInput{
		Name:              "job",
		TaskTemplate:      "run",
		Condition:         map[string]any{"interval_seconds": 60},
		SessionTargetMode: SessionTargetLatestActive,
	}, "agent-1", time.Unix(200, 0))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTrigger(def); err != nil {
		t.Fatal(err)
	}
	sub := &seqSubmitter{}
	sched := NewScheduler(store, sub, 5)
	sched.SetSessionResolver(staticResolver{id: "sess-active"})
	record, err := sched.FireTrigger(def.TriggerID, "manual", nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != FireStatusQueued {
		t.Fatalf("status = %s", record.Status)
	}
	if len(sub.sessions) != 1 || sub.sessions[0] != "sess-active" {
		t.Fatalf("sessions = %v", sub.sessions)
	}
	if sub.lastEnv.RequestType != "message" {
		t.Fatalf("request_type = %q", sub.lastEnv.RequestType)
	}
}
