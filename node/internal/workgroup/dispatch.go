package workgroup

import (
	"context"
	"encoding/json"
	"strings"
)

// DispatchResult 为处理一条 Manage→Node 业务帧的结果。
type DispatchResult struct {
	AckEnvelope map[string]any
	Handled     bool
	ErrorCode   ErrorCode
	// PendingAck：业务已处理，需在 ACK 帧写出成功后再推进本地 delivery 游标。
	PendingAck     bool
	AckWorkgroupID string
	AckDeliverySeq int64
}

// DispatchEnvelope 处理 Manage 下发的 WSEnvelope（不含 session/resume 控制面）。
func (w *Worker) DispatchEnvelope(env WSEnvelope) (*DispatchResult, error) {
	if err := w.Session.FenceFrame(env.ConnectionGeneration); err != nil {
		we, _ := err.(*Error)
		code := CodeFencingRejected
		if we != nil {
			code = we.Code
		}
		return &DispatchResult{
			Handled:   false,
			ErrorCode: code,
			AckEnvelope: map[string]any{
				"type": "session.error",
				"payload": map[string]any{
					"code":    string(code),
					"message": err.Error(),
				},
			},
		}, err
	}

	switch env.Type {
	case "agent.session.open":
		if w.AgentSessions == nil {
			return agentSessionFailureResult(env, "agent session handler not configured")
		}
		req, err := agentSessionOpenFromPayload(env.Payload)
		if err != nil {
			return agentSessionFailureResult(env, err.Error())
		}
		res, err := w.AgentSessions.OpenAgentSession(context.Background(), req)
		if err != nil {
			return agentSessionFailureResult(env, err.Error())
		}
		return &DispatchResult{
			Handled:        true,
			PendingAck:     env.DeliverySeq > 0,
			AckWorkgroupID: deliveryWorkgroupID(env, ""),
			AckDeliverySeq: env.DeliverySeq,
			AckEnvelope: map[string]any{
				"type":    "agent.session.ready",
				"payload": agentResponsePayload(env, agentSessionResultPayload(res), w.Session.Generation()),
			},
		}, nil

	case "agent.turn.start":
		if w.AgentSessions == nil {
			return agentTurnFailureResult(env, "agent session handler not configured")
		}
		req, err := agentTurnStartFromPayload(env.Payload)
		if err != nil {
			return agentTurnFailureResult(env, err.Error())
		}
		if err := w.AgentSessions.StartAgentTurn(context.Background(), req); err != nil {
			return agentTurnFailureResult(env, err.Error())
		}
		return &DispatchResult{
			Handled:        true,
			PendingAck:     env.DeliverySeq > 0,
			AckWorkgroupID: deliveryWorkgroupID(env, ""),
			AckDeliverySeq: env.DeliverySeq,
			AckEnvelope: map[string]any{
				"type":    "agent.turn.accepted",
				"payload": agentResponsePayload(env, agentTurnIdentityPayload(req), w.Session.Generation()),
			},
		}, nil

	case "agent.turn.cancel":
		if w.AgentSessions == nil {
			return agentTurnFailureResult(env, "agent session handler not configured")
		}
		req, err := agentTurnCancelFromPayload(env.Payload)
		if err != nil {
			return agentTurnFailureResult(env, err.Error())
		}
		if err := w.AgentSessions.CancelAgentTurn(context.Background(), req); err != nil {
			return agentTurnFailureResult(env, err.Error())
		}
		return &DispatchResult{
			Handled:        true,
			PendingAck:     env.DeliverySeq > 0,
			AckWorkgroupID: deliveryWorkgroupID(env, ""),
			AckDeliverySeq: env.DeliverySeq,
			AckEnvelope: map[string]any{
				"type":    "agent.turn.cancelled",
				"payload": agentResponsePayload(env, agentTurnCancelPayload(req), w.Session.Generation()),
			},
		}, nil

	case "agent.tool.cancel":
		handler, ok := w.AgentSessions.(AgentToolCancelHandler)
		if !ok {
			return agentTurnFailureResult(env, "agent tool cancellation is not supported")
		}
		req, err := agentToolCancelFromPayload(env.Payload)
		if err != nil {
			return agentTurnFailureResult(env, err.Error())
		}
		if err := handler.CancelAgentTool(context.Background(), req); err != nil {
			return agentTurnFailureResult(env, err.Error())
		}
		return &DispatchResult{
			Handled:        true,
			PendingAck:     env.DeliverySeq > 0,
			AckWorkgroupID: deliveryWorkgroupID(env, ""),
			AckDeliverySeq: env.DeliverySeq,
			AckEnvelope: map[string]any{
				"type":    "agent.tool.cancelled",
				"payload": agentResponsePayload(env, agentToolCancelPayload(req), w.Session.Generation()),
			},
		}, nil

	case "agent.turn.resume":
		if w.AgentSessions == nil {
			return agentTurnFailureResult(env, "agent session handler not configured")
		}
		req, err := agentTurnResumeFromPayload(env.Payload)
		if err != nil {
			return agentTurnFailureResult(env, err.Error())
		}
		if err := w.AgentSessions.ResumeAgentTurn(context.Background(), req); err != nil {
			return agentTurnFailureResult(env, err.Error())
		}
		return &DispatchResult{
			Handled:        true,
			PendingAck:     env.DeliverySeq > 0,
			AckWorkgroupID: deliveryWorkgroupID(env, ""),
			AckDeliverySeq: env.DeliverySeq,
			AckEnvelope: map[string]any{
				"type": "agent.turn.resumed",
				"payload": agentResponsePayload(env, map[string]any{
					"workgroup_id":  req.WorkgroupID,
					"member_id":     req.MemberID,
					"agent_id":      req.AgentID,
					"session_id":    req.SessionID,
					"assign_id":     req.AssignID,
					"child_turn_id": req.ChildTurnID,
					"attempt_id":    req.AttemptID,
					"hitl_id":       req.HitlID,
					"status":        "resumed",
				}, w.Session.Generation()),
			},
		}, nil

	case "agent.session.close":
		if w.AgentSessions == nil {
			return agentSessionFailureResult(env, "agent session handler not configured")
		}
		req, err := agentSessionOpenFromPayload(env.Payload)
		if err != nil {
			return agentSessionFailureResult(env, err.Error())
		}
		if err := w.AgentSessions.CloseAgentSession(context.Background(), req); err != nil {
			return agentSessionFailureResult(env, err.Error())
		}
		return &DispatchResult{
			Handled:        true,
			PendingAck:     env.DeliverySeq > 0,
			AckWorkgroupID: deliveryWorkgroupID(env, ""),
			AckDeliverySeq: env.DeliverySeq,
			AckEnvelope: map[string]any{
				"type": "agent.session.closed",
				"payload": agentResponsePayload(env, agentSessionResultPayload(AgentSessionResult{
					WorkgroupID: req.WorkgroupID, MemberID: req.MemberID, AgentID: req.AgentID,
					SessionID: req.SessionID, Status: "closed",
				}), w.Session.Generation()),
			},
		}, nil

	case "timeline.event":
		if w.OnTimelineEvent != nil {
			w.OnTimelineEvent(env)
		}
		wgID := deliveryWorkgroupID(env, "")
		return &DispatchResult{
			Handled:        true,
			PendingAck:     true,
			AckWorkgroupID: wgID,
			AckDeliverySeq: env.DeliverySeq,
			AckEnvelope: map[string]any{
				"type": "delivery.ack",
				"payload": map[string]any{
					"workgroup_id":          wgID,
					"delivery_seq":          env.DeliverySeq,
					"connection_generation": w.Session.Generation(),
					"type":                  env.Type,
				},
			},
		}, nil

	default:
		return nil, errf(CodeSchemaMismatch, "unsupported envelope type %q", env.Type)
	}
}

// CommitPendingAck 在 ACK 帧成功写出后推进本地 delivery 游标。
func (w *Worker) CommitPendingAck(res *DispatchResult) error {
	if w == nil || res == nil || !res.PendingAck {
		return nil
	}
	if err := w.Session.AckDelivery(res.AckWorkgroupID, res.AckDeliverySeq); err != nil {
		// Manage may have queued a resume replay and a live frame from
		// different request paths. If a lower sequence arrives after a higher
		// one, the higher cursor already covers it; do not tear down the WS.
		// Session.AckDelivery remains strict for direct callers/tests.
		if conflict, ok := err.(*Error); ok && conflict.Code == CodeConflict &&
			strings.HasPrefix(conflict.Message, "delivery_seq regress") {
			res.PendingAck = false
			return nil
		}
		return err
	}
	res.PendingAck = false
	return nil
}

func deliveryWorkgroupID(env WSEnvelope, fallback string) string {
	if wg := strings.TrimSpace(env.WorkgroupID); wg != "" {
		return wg
	}
	return strings.TrimSpace(fallback)
}

func agentSessionOpenFromPayload(payload map[string]any) (AgentSessionOpenRequest, error) {
	var req AgentSessionOpenRequest
	raw, _ := json.Marshal(payload)
	if err := json.Unmarshal(raw, &req); err != nil {
		return req, errf(CodeSchemaMismatch, "agent.session payload: %v", err)
	}
	if strings.TrimSpace(req.WorkgroupID) == "" || strings.TrimSpace(req.MemberID) == "" ||
		strings.TrimSpace(req.AgentID) == "" || strings.TrimSpace(req.SessionID) == "" {
		return req, errf(CodeSchemaMismatch, "agent.session requires workgroup_id, member_id, agent_id, session_id")
	}
	return req, nil
}

func agentTurnStartFromPayload(payload map[string]any) (AgentTurnStartRequest, error) {
	var req AgentTurnStartRequest
	raw, _ := json.Marshal(payload)
	if err := json.Unmarshal(raw, &req); err != nil {
		return req, errf(CodeSchemaMismatch, "agent.turn.start payload: %v", err)
	}
	if strings.TrimSpace(req.WorkgroupID) == "" || strings.TrimSpace(req.MemberID) == "" ||
		strings.TrimSpace(req.AgentID) == "" || strings.TrimSpace(req.SessionID) == "" ||
		strings.TrimSpace(req.AssignID) == "" || strings.TrimSpace(req.Source) == "" ||
		strings.TrimSpace(req.ParentTurnID) == "" || strings.TrimSpace(req.ChildTurnID) == "" ||
		strings.TrimSpace(req.AttemptID) == "" || strings.TrimSpace(req.UserMessage) == "" {
		return req, errf(CodeSchemaMismatch, "agent.turn.start requires workgroup_id, member_id, agent_id, session_id, assign_id, source, parent_turn_id, child_turn_id, attempt_id, user_message")
	}
	if req.Source != "leader_tool" && req.Source != "direct_member" {
		return req, errf(CodeSchemaMismatch, "agent.turn.start source must be leader_tool or direct_member")
	}
	return req, nil
}

func agentTurnCancelFromPayload(payload map[string]any) (AgentTurnCancelRequest, error) {
	var req AgentTurnCancelRequest
	raw, _ := json.Marshal(payload)
	if err := json.Unmarshal(raw, &req); err != nil {
		return req, errf(CodeSchemaMismatch, "agent.turn.cancel payload: %v", err)
	}
	if strings.TrimSpace(req.WorkgroupID) == "" || strings.TrimSpace(req.MemberID) == "" ||
		strings.TrimSpace(req.AgentID) == "" || strings.TrimSpace(req.SessionID) == "" || strings.TrimSpace(req.AssignID) == "" {
		return req, errf(CodeSchemaMismatch, "agent.turn.cancel requires workgroup_id, member_id, agent_id, session_id, assign_id")
	}
	if strings.TrimSpace(req.ChildTurnID) == "" || strings.TrimSpace(req.AttemptID) == "" {
		return req, errf(CodeSchemaMismatch, "agent.turn.cancel requires child_turn_id and attempt_id")
	}
	return req, nil
}

func agentToolCancelFromPayload(payload map[string]any) (AgentToolCancelRequest, error) {
	var req AgentToolCancelRequest
	raw, _ := json.Marshal(payload)
	if err := json.Unmarshal(raw, &req); err != nil {
		return req, errf(CodeSchemaMismatch, "agent.tool.cancel payload: %v", err)
	}
	if strings.TrimSpace(req.WorkgroupID) == "" || strings.TrimSpace(req.MemberID) == "" ||
		strings.TrimSpace(req.AgentID) == "" || strings.TrimSpace(req.SessionID) == "" ||
		strings.TrimSpace(req.AssignID) == "" || strings.TrimSpace(req.ToolCallID) == "" ||
		strings.TrimSpace(req.ToolName) == "" {
		return req, errf(CodeSchemaMismatch, "agent.tool.cancel requires workgroup_id, member_id, agent_id, session_id, assign_id, tool_call_id, tool_name")
	}
	return req, nil
}

func agentTurnResumeFromPayload(payload map[string]any) (AgentTurnResumeRequest, error) {
	var req AgentTurnResumeRequest
	raw, _ := json.Marshal(payload)
	if err := json.Unmarshal(raw, &req); err != nil {
		return req, errf(CodeSchemaMismatch, "agent.turn.resume payload: %v", err)
	}
	if strings.TrimSpace(req.WorkgroupID) == "" || strings.TrimSpace(req.MemberID) == "" ||
		strings.TrimSpace(req.AgentID) == "" || strings.TrimSpace(req.SessionID) == "" ||
		strings.TrimSpace(req.AssignID) == "" || strings.TrimSpace(req.ChildTurnID) == "" ||
		strings.TrimSpace(req.AttemptID) == "" || len(req.ResumeValue) == 0 {
		return req, errf(CodeSchemaMismatch, "agent.turn.resume requires workgroup_id, member_id, agent_id, session_id, assign_id, child_turn_id, attempt_id, resume_value")
	}
	return req, nil
}

func agentSessionResultPayload(res AgentSessionResult) map[string]any {
	return map[string]any{
		"workgroup_id": res.WorkgroupID,
		"member_id":    res.MemberID,
		"agent_id":     res.AgentID,
		"session_id":   res.SessionID,
		"status":       res.Status,
		"message":      res.Message,
	}
}

func agentTurnIdentityPayload(req AgentTurnStartRequest) map[string]any {
	return map[string]any{
		"workgroup_id":      req.WorkgroupID,
		"member_id":         req.MemberID,
		"agent_id":          req.AgentID,
		"session_id":        req.SessionID,
		"assign_id":         req.AssignID,
		"source":            req.Source,
		"parent_turn_id":    req.ParentTurnID,
		"child_turn_id":     req.ChildTurnID,
		"attempt_id":        req.AttemptID,
		"client_message_id": req.ClientMessageID,
	}
}

func agentTurnCancelPayload(req AgentTurnCancelRequest) map[string]any {
	return map[string]any{
		"workgroup_id":  req.WorkgroupID,
		"member_id":     req.MemberID,
		"agent_id":      req.AgentID,
		"session_id":    req.SessionID,
		"assign_id":     req.AssignID,
		"child_turn_id": req.ChildTurnID,
		"attempt_id":    req.AttemptID,
		"status":        "canceled",
	}
}

func agentToolCancelPayload(req AgentToolCancelRequest) map[string]any {
	return map[string]any{
		"workgroup_id": req.WorkgroupID,
		"member_id":    req.MemberID,
		"agent_id":     req.AgentID,
		"session_id":   req.SessionID,
		"assign_id":    req.AssignID,
		"tool_call_id": req.ToolCallID,
		"tool_name":    req.ToolName,
		"status":       "canceled",
	}
}

func agentResponsePayload(env WSEnvelope, base map[string]any, generation int64) map[string]any {
	out := make(map[string]any, len(base)+3)
	for key, value := range base {
		out[key] = value
	}
	workgroupID := payloadString(base["workgroup_id"])
	if workgroupID == "" {
		workgroupID = strings.TrimSpace(env.WorkgroupID)
	}
	out["workgroup_id"] = workgroupID
	out["delivery_seq"] = env.DeliverySeq
	out["connection_generation"] = generation
	return out
}

func payloadString(value any) string {
	s, _ := value.(string)
	return strings.TrimSpace(s)
}

func agentSessionFailureResult(env WSEnvelope, message string) (*DispatchResult, error) {
	payload := map[string]any{
		"message":               message,
		"status":                "error",
		"workgroup_id":          env.Payload["workgroup_id"],
		"member_id":             env.Payload["member_id"],
		"agent_id":              env.Payload["agent_id"],
		"session_id":            env.Payload["session_id"],
		"connection_generation": env.ConnectionGeneration,
		"delivery_seq":          env.DeliverySeq,
	}
	return &DispatchResult{
		Handled:        true,
		PendingAck:     env.DeliverySeq > 0,
		AckWorkgroupID: deliveryWorkgroupID(env, ""),
		AckDeliverySeq: env.DeliverySeq,
		ErrorCode:      CodeConflict,
		AckEnvelope: map[string]any{
			"type": "agent.session.error", "payload": payload,
		},
	}, errf(CodeConflict, "%s", message)
}

func agentTurnFailureResult(env WSEnvelope, message string) (*DispatchResult, error) {
	payload := map[string]any{
		"message":               message,
		"status":                "failed",
		"workgroup_id":          env.Payload["workgroup_id"],
		"member_id":             env.Payload["member_id"],
		"agent_id":              env.Payload["agent_id"],
		"session_id":            env.Payload["session_id"],
		"assign_id":             env.Payload["assign_id"],
		"connection_generation": env.ConnectionGeneration,
		"delivery_seq":          env.DeliverySeq,
	}
	return &DispatchResult{
		Handled:        true,
		PendingAck:     env.DeliverySeq > 0,
		AckWorkgroupID: deliveryWorkgroupID(env, ""),
		AckDeliverySeq: env.DeliverySeq,
		ErrorCode:      CodeConflict,
		AckEnvelope: map[string]any{
			"type": "agent.turn.result", "payload": payload,
		},
	}, errf(CodeConflict, "%s", message)
}
