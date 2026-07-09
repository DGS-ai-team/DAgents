package full

import (
	"strings"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	tuishared "github.com/DGS-ai-team/DAgents/client/internal/tui/shared"
)

func (m *model) hydrateSession() error {
	sid := m.currentSession()
	if sid == "" {
		return nil
	}
	data, err := m.client.GetSessionHydrate(m.ctx, sid)
	if err != nil {
		return err
	}
	return m.applyHydrate(data)
}

func (m *model) applyHydrate(data *nodeapi.SessionHydrate) error {
	if data == nil {
		return nil
	}
	tuishared.LoadTranscriptFromHydrate(m.transcript, data.Transcript, tuishared.HydrateTranscriptOptions{
		ShowReasoning: m.showReasoning,
		Verbose:       m.toolFold.Verbose(),
		ToolRegistry:  m.toolBlocks,
	})
	m.resetHITLQueue()
	m.resetHITLState()
	if data.PendingHITL != nil {
		m.enqueueHITLRequired(data.PendingHITL)
	}
	m.turn.ApplyHydrateSeqHint(data.SSESeqHint)
	m.sseFromSeq = m.turn.SSEStartSeq()
	applyHydrateTurnState(m, data)
	if data.SSESeqHint > 0 {
		go func(seq int) {
			_ = m.client.PostSessionAck(m.ctx, m.currentSession(), seq)
		}(data.SSESeqHint)
	}
	if m.program != nil {
		m.program.Send(pendingHITLChangedMsg{})
		m.program.Send(syncChildAgentsMsg{})
	}
	m.syncViewport()
	return nil
}

func applyHydrateTurnState(m *model, data *nodeapi.SessionHydrate) {
	phase := strings.TrimSpace(data.RunTurnPhase)
	if hydrateHasPendingHITL(data.PendingHITL) || phase == "awaiting_hitl" {
		m.turn.FinishTurn()
		return
	}
	activePhases := map[string]struct{}{
		"model_streaming":           {},
		"awaiting_tool_execution":   {},
		"tool_loop":                 {},
		"open_batch":                {},
		"other":                     {},
	}
	if data.HasActiveTurn {
		if _, ok := activePhases[phase]; ok {
			m.turn.BeginImplicitTurn()
			return
		}
	}
	m.turn.FinishTurn()
}

func hydrateHasPendingHITL(pending map[string]any) bool {
	if pending == nil {
		return false
	}
	items, ok := pending["items"].([]any)
	return ok && len(items) > 0
}
