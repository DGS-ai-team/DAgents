package turn

import "github.com/DGS-ai-team/DAgents/node/internal/llm"

func (o *Orchestrator) appendHistory(sessionID string, history *[]llm.Message, message llm.Message) {
	normalized := o.llm.NormalizeAssistant(*history, message)
	if o.journal != nil {
		o.journal.AppendMessage(sessionID, history, normalized)
	} else {
		*history = append(*history, normalized)
	}
}

func (o *Orchestrator) insertHistory(sessionID string, history *[]llm.Message, index int, message llm.Message) {
	normalized := o.llm.NormalizeAssistant(*history, message)
	if o.journal != nil {
		o.journal.InsertMessage(sessionID, history, index, normalized)
	} else {
		if index < 0 {
			index = 0
		}
		if index > len(*history) {
			index = len(*history)
		}
		out := append([]llm.Message(nil), (*history)[:index]...)
		out = append(out, normalized)
		out = append(out, (*history)[index:]...)
		*history = out
	}
}
