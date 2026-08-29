package session

// refreshSkillsCatalogView captures the live directory at an explicit model
// context boundary. The resulting view is used by system prompt metadata and
// skill body loading; list_available_skills uses the live catalog separately.
func (r *runtime) refreshSkillsCatalogView(appliedBoundary string) {
	if r == nil || r.skillsCatalog == nil {
		return
	}
	view := r.skillsCatalog.NewTurnView()
	if view == nil {
		return
	}
	current := view.Revision()
	if current == "" {
		return
	}
	r.mu.Lock()
	previous := r.skillRevision
	r.skillRevision = current
	r.skillsTurnCatalog = view
	r.mu.Unlock()
	if r.orch != nil {
		r.orch.SetSkillsCatalog(view)
	}
	if previous != current && r.hub != nil {
		r.hub.Publish(r.session.ID, "skills/changed", map[string]any{
			"agent_id":         r.agentID,
			"previous":         previous,
			"revision":         current,
			"applied_boundary": appliedBoundary,
			"change":           "catalog_revision",
		})
	}
}

// scheduleModelContextRebuild is the single runtime boundary for changes
// that must be visible to the next model request. It invalidates the
// Orchestrator's request snapshot and refreshes the frozen Skills view before
// the next build; neither operation mutates an in-flight model request.
func (r *runtime) scheduleModelContextRebuild(reason, appliedBoundary string) {
	if r == nil {
		return
	}
	if r.orch != nil {
		r.orch.RequestModelContextRefresh(r.session.ID, reason)
	}
	r.refreshSkillsCatalogView(appliedBoundary)
}

// observeSkillCatalogChange applies external Skill edits only at a new human
// Turn boundary. The active Turn snapshot remains untouched.
func (r *runtime) observeSkillCatalogChange() {
	r.refreshSkillsCatalogView("next_human_turn")
}
