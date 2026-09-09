package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/events"
	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
)

// AutoIntentProjector is a one-way, idempotent projection from the durable
// Goal intent to a Trigger. It never fires a trigger or starts a new loop.
type AutoIntentProjector struct {
	Goals          *goals.Store
	Triggers       *triggers.Store
	SourceRegistry interface {
		GetRegistration(string) (events.SourceRegistration, bool)
	}
}

func (p *AutoIntentProjector) Project(goalID, purpose string) (triggers.Definition, error) {
	if p == nil || p.Goals == nil || p.Triggers == nil {
		return triggers.Definition{}, fmt.Errorf("projector_unavailable")
	}
	g, ok := p.Goals.Get(goalID)
	if !ok {
		return triggers.Definition{}, goals.ErrNotFound
	}
	if strings.TrimSpace(g.SessionID) == "" {
		return triggers.Definition{}, fmt.Errorf("intent_projection_session_required")
	}
	i, err := p.Goals.GetScheduleIntent(goalID, purpose)
	if err != nil {
		return triggers.Definition{}, err
	}
	id := managedIntentTriggerID(goalID, purpose)
	if g.TriggerID != "" && g.TriggerID != id {
		if old, exists := p.Triggers.GetTrigger(g.TriggerID); exists && old.ManagedGoalID == g.ID && old.OwnerAgentID == g.AgentID && old.Controller == "goal" && old.ControllerID == g.ID && old.ManagedIntentID == i.ID && old.ManagedGeneration == i.Generation && old.ManagedFingerprint == i.Fingerprint {
			if _, err := p.Triggers.DisableManagedProjection(*old); err != nil {
				return triggers.Definition{}, err
			}
		}
	}
	if i.State == goals.IntentRevoked {
		if existing, exists := p.Triggers.GetTrigger(id); exists {
			if existing.ManagedGoalID != goalID || existing.OwnerAgentID != g.AgentID || existing.Controller != "goal" || existing.ControllerID != goalID || existing.ManagedGeneration > i.Generation {
				return triggers.Definition{}, fmt.Errorf("intent_projection_owner_mismatch")
			}
			updated, err := p.Triggers.DisableManagedProjection(*existing)
			if err != nil {
				return triggers.Definition{}, err
			}
			return updated, nil
		}
		return triggers.Definition{TriggerID: id, Enabled: false, ManagedGoalID: goalID}, nil
	}
	profile, ok := p.Goals.GetProfile(g.AgentID)
	if !ok || !profile.Enabled || profile.CurrentGoalID != g.ID || profile.Revision != i.ProfileRevision {
		return triggers.Definition{}, fmt.Errorf("intent_projection_fenced")
	}
	if !g.Managed || g.Status == goals.StatusPaused || g.Status == goals.StatusStopped || g.Status == goals.StatusCompleted {
		return triggers.Definition{}, fmt.Errorf("intent_projection_fenced")
	}
	isEvent := i.Decision.NextAction == goals.NextEvent
	if isEvent {
		source := ""
		if i.Decision.Event != nil {
			source = strings.TrimSpace(i.Decision.Event.SourceID)
		}
		reg, ok := events.SourceRegistration{}, false
		if p.SourceRegistry != nil {
			reg, ok = p.SourceRegistry.GetRegistration(source)
		}
		if source == "" || !ok || !reg.Enabled || reg.OwnerAgentID != g.AgentID {
			return triggers.Definition{}, fmt.Errorf("event_source_not_registered")
		}
	}
	next := (*float64)(nil)
	if i.DueAt != nil {
		v := float64(i.DueAt.UnixNano()) / 1e9
		next = &v
	}
	if next == nil && !isEvent {
		return triggers.Definition{}, fmt.Errorf("intent_projection_missing_due_at")
	}
	condition := map[string]any{}
	if isEvent {
		condition["event_source_id"] = strings.TrimSpace(i.Decision.Event.SourceID)
		condition["event_filter"] = i.Decision.Event.Filter
	} else {
		condition["fire_at"] = *next
	}
	session := strings.TrimSpace(g.SessionID)
	def := triggers.Definition{TriggerID: id, Name: g.Title, Condition: condition, TargetAgentID: g.AgentID, TargetSessionID: &session, SessionTargetMode: triggers.SessionTargetFixed, TaskTemplate: g.Objective, Enabled: true, NextFireAt: next, ManagedGoalID: g.ID, OwnerAgentID: g.AgentID, Controller: "goal", ControllerID: g.ID, Revision: 1, CreatedBy: "autonomy", ManagedIntentID: i.ID, ManagedGeneration: i.Generation, ManagedFingerprint: i.Fingerprint}
	if existing, exists := p.Triggers.GetTrigger(id); exists {
		if existing.ManagedGoalID != g.ID || existing.OwnerAgentID != g.AgentID || existing.Controller != "goal" || existing.ControllerID != g.ID || existing.ManagedGeneration > i.Generation {
			return triggers.Definition{}, triggers.ErrRevisionConflict
		}
		_ = existing
		if i.State == goals.IntentProjected && existing.ManagedGeneration == i.Generation && existing.LastFiredAt != nil {
			updated, err := p.Triggers.DisableManagedProjection(*existing)
			if err != nil {
				return triggers.Definition{}, err
			}
			return updated, nil
		}
		if _, err := p.Triggers.UpsertManagedProjection(def); err != nil {
			return triggers.Definition{}, err
		}
	} else if _, err := p.Triggers.UpsertManagedProjection(def); err != nil {
		return triggers.Definition{}, err
	}
	if i.State == goals.IntentProjected {
		got, ok := p.Triggers.GetTrigger(id)
		if !ok || got.ManagedGeneration != i.Generation || got.ManagedFingerprint != i.Fingerprint || got.ManagedGoalID != g.ID || got.OwnerAgentID != g.AgentID || got.Controller != "goal" || got.ControllerID != g.ID {
			return triggers.Definition{}, fmt.Errorf("projected_intent_mismatch")
		}
		if g.TriggerID != id {
			if _, err := p.Goals.BindTrigger(g.ID, id, time.Now().UTC()); err != nil {
				return triggers.Definition{}, err
			}
		}
		return *got, nil
	}
	if err := p.Goals.ConfirmProjected(goalID, purpose, i.Generation, i.Fingerprint); err != nil {
		return triggers.Definition{}, err
	}
	if _, err := p.Goals.BindTrigger(g.ID, id, time.Now().UTC()); err != nil {
		return triggers.Definition{}, err
	}
	got, _ := p.Triggers.GetTrigger(id)
	if got == nil {
		return triggers.Definition{}, fmt.Errorf("projected trigger missing")
	}
	return *got, nil
}

func managedIntentTriggerID(goalID, purpose string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(goalID) + "\x00" + strings.TrimSpace(purpose)))
	return "auto-intent-" + hex.EncodeToString(h[:12])
}
