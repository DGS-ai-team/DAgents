package hooks

import (
	"encoding/json"
)

type hookContextDTO struct {
	Phase         Phase          `json:"phase"`
	SessionID     string         `json:"session_id"`
	AgentID       string         `json:"agent_id"`
	TurnID        string         `json:"turn_id,omitempty"`
	ParentAgentID string         `json:"parent_agent_id,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`

	MessageEnqueued    *MessageEnqueuedPayload    `json:"message_enqueued,omitempty"`
	TurnBeforeCompress *TurnBeforeCompressPayload `json:"turn_before_compress,omitempty"`
	TurnBeforeStep     *TurnBeforeStepPayload     `json:"turn_before_step,omitempty"`
	PromptBuild        *PromptBuildPayload        `json:"prompt_build,omitempty"`
	LLMBeforeCall      *LLMCallPayload            `json:"llm_before_call,omitempty"`
	LLMAfterCall       *LLMAfterCallPayload       `json:"llm_after_call,omitempty"`
	ToolBeforeEach     *ToolBeforeEachPayload     `json:"tool_before_each,omitempty"`
	ToolAfterEach      *ToolAfterEachPayload      `json:"tool_after_each,omitempty"`
	HITLBeforePause    *HITLBeforePausePayload    `json:"hitl_before_pause,omitempty"`
	HITLAfterResume    *HITLAfterResumePayload    `json:"hitl_after_resume,omitempty"`
	TurnDone           *TurnDonePayload           `json:"turn_done,omitempty"`
	TurnError          *turnErrorDTO              `json:"turn_error,omitempty"`
	TurnCancel         *TurnCancelPayload         `json:"turn_cancel,omitempty"`
	SessionLifecycle   *SessionLifecyclePayload   `json:"session_lifecycle,omitempty"`

	ToolDecision *ToolBeforeEachResult `json:"tool_decision,omitempty"`
}

type turnErrorDTO struct {
	Message string `json:"message,omitempty"`
}

func contextToDTO(hc *Context) hookContextDTO {
	if hc == nil {
		return hookContextDTO{}
	}
	dto := hookContextDTO{
		Phase:              hc.Phase,
		SessionID:          hc.SessionID,
		AgentID:            hc.AgentID,
		TurnID:             hc.TurnID,
		ParentAgentID:      hc.ParentAgentID,
		Metadata:           hc.Metadata,
		MessageEnqueued:    hc.MessageEnqueued,
		TurnBeforeCompress: hc.TurnBeforeCompress,
		TurnBeforeStep:     hc.TurnBeforeStep,
		PromptBuild:        hc.PromptBuild,
		LLMBeforeCall:      hc.LLMBeforeCall,
		LLMAfterCall:       hc.LLMAfterCall,
		ToolBeforeEach:     hc.ToolBeforeEach,
		ToolAfterEach:      hc.ToolAfterEach,
		HITLBeforePause:    hc.HITLBeforePause,
		HITLAfterResume:    hc.HITLAfterResume,
		TurnDone:           hc.TurnDone,
		TurnCancel:         hc.TurnCancel,
		SessionLifecycle:   hc.SessionLifecycle,
		ToolDecision:       hc.ToolDecision,
	}
	if hc.TurnError != nil && hc.TurnError.Err != nil {
		dto.TurnError = &turnErrorDTO{Message: hc.TurnError.Err.Error()}
	}
	return dto
}

func marshalHookContext(hc *Context) ([]byte, error) {
	return json.Marshal(contextToDTO(hc))
}
