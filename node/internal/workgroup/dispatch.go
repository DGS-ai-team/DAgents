package workgroup

import (
	"context"
	"encoding/json"
	"fmt"
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
					"workgroup_id": req.WorkgroupID,
					"member_id":    req.MemberID,
					"agent_id":     req.AgentID,
					"session_id":   req.SessionID,
					"assign_id":    req.AssignID,
					"hitl_id":      req.HitlID,
					"status":       "resumed",
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

	case "member.provision":
		req, err := provisionFromPayload(env.Payload)
		if err != nil {
			return provisionFailureResult(env, req, err)
		}
		res, err := w.HandleProvision(req)
		if err != nil {
			return provisionFailureResult(env, req, err)
		}
		wgID := deliveryWorkgroupID(env, res.Binding.WorkgroupID)
		return &DispatchResult{
			Handled:        true,
			PendingAck:     true,
			AckWorkgroupID: wgID,
			AckDeliverySeq: env.DeliverySeq,
			AckEnvelope: map[string]any{
				"type": "member.provision_result",
				"payload": map[string]any{
					"provision_id":          res.Binding.ProvisionID,
					"member_id":             res.Binding.MemberID,
					"workgroup_id":          res.Binding.WorkgroupID,
					"workspace_path":        res.Binding.WorkspacePath,
					"tool_catalog_revision": res.Binding.ToolCatalogRevision,
					"status":                res.Binding.Status,
					"delivery_seq":          env.DeliverySeq,
					"connection_generation": w.Session.Generation(),
				},
			},
		}, nil

	case "tool.command":
		cmd, err := toolCommandFromPayload(env.Payload)
		if err != nil {
			return toolCommandFailureResult(env, cmd, err)
		}
		res, err := w.HandleCommand(cmd)
		// rejected 时 HandleCommand 仍返回 AcceptResult + error
		if res == nil {
			// 绑定缺失等：必须回 tool.result，否则 Manage wait_command_result 空等至 60s 超时
			return toolCommandFailureResult(env, cmd, err)
		}
		wgID := deliveryWorkgroupID(env, cmd.WorkgroupID)
		ackPayload := map[string]any{
			"command_id":            res.Ack.CommandID,
			"status":                res.Ack.Status,
			"connection_generation": res.Ack.ConnectionGeneration,
			"journaled_at":          res.Ack.JournaledAt,
			"delivery_seq":          env.DeliverySeq,
			"workgroup_id":          cmd.WorkgroupID,
			"member_id":             cmd.MemberID,
			"assign_id":             cmd.AssignID,
		}
		ack := map[string]any{"type": "tool.ack", "payload": ackPayload}
		out := &DispatchResult{
			Handled:        true,
			PendingAck:     true,
			AckWorkgroupID: wgID,
			AckDeliverySeq: env.DeliverySeq,
			AckEnvelope:    ack,
		}
		if isTerminalCommandStatus(res.Entry.Status) {
			out.AckEnvelope = map[string]any{
				"type": "tool.result",
				"payload": mergeMaps(ackPayload, map[string]any{
					"result_json": res.Entry.ResultJSON,
					"error_code":  res.Entry.ErrorCode,
					"status":      res.Entry.Status,
					"result_text": extractResultText(res.Entry.ResultJSON),
				}),
			}
		} else if res.Entry.Status == "accepted" || res.Entry.Status == "running" {
			// 非终态却已从 Accept 返回：本路径不会再异步回传，强制 tool.result 避免 Manage 空等
			status := "indeterminate"
			code := string(CodeIndeterminate)
			out.AckEnvelope = map[string]any{
				"type": "tool.result",
				"payload": mergeMaps(ackPayload, map[string]any{
					"status":      status,
					"error_code":  code,
					"result_text": "command left non-terminal without result callback",
					"result_json": "",
				}),
			}
		}
		if err != nil && res.Rejected {
			out.ErrorCode = res.ErrorCode
			return out, err
		}
		return out, nil

	case "tool.cancel":
		commandID, workgroupID, assignID, memberID := cancelFromPayload(env.Payload)
		if commandID == "" {
			return nil, errf(CodeSchemaMismatch, "tool.cancel requires command_id")
		}
		wgFallback := workgroupID
		if wgFallback == "" {
			wgFallback = deliveryWorkgroupID(env, "")
		}
		res, err := w.HandleCancel(commandID, wgFallback)
		if err != nil {
			return dispatchErr(err)
		}
		wgID := deliveryWorkgroupID(env, wgFallback)
		payload := map[string]any{
			"command_id":            res.Ack.CommandID,
			"status":                res.Entry.Status,
			"error_code":            res.Entry.ErrorCode,
			"result_text":           "",
			"connection_generation": res.Ack.ConnectionGeneration,
			"journaled_at":          res.Ack.JournaledAt,
			"delivery_seq":          env.DeliverySeq,
			"workgroup_id":          wgFallback,
			"member_id":             memberID,
			"assign_id":             assignID,
		}
		return &DispatchResult{
			Handled:        true,
			PendingAck:     true,
			AckWorkgroupID: wgID,
			AckDeliverySeq: env.DeliverySeq,
			AckEnvelope: map[string]any{
				"type":    "tool.result",
				"payload": payload,
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

	case "workgroup.tombstone":
		var t ArchiveTombstone
		raw, _ := json.Marshal(env.Payload)
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, errf(CodeSchemaMismatch, "tombstone: %v", err)
		}
		if err := w.HandleArchive(t); err != nil {
			return dispatchErr(err)
		}
		wgID := deliveryWorkgroupID(env, t.WorkgroupID)
		return &DispatchResult{
			Handled:        true,
			PendingAck:     true,
			AckWorkgroupID: wgID,
			AckDeliverySeq: env.DeliverySeq,
			AckEnvelope: map[string]any{
				"type": "workgroup.tombstone_ack",
				"payload": map[string]any{
					"workgroup_id":          t.WorkgroupID,
					"member_id":             t.MemberID,
					"delivery_seq":          env.DeliverySeq,
					"connection_generation": w.Session.Generation(),
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

func dispatchErr(err error) (*DispatchResult, error) {
	code := CodeConflict
	msg := fmt.Sprint(err)
	if we, ok := err.(*Error); ok {
		code = we.Code
		msg = we.Message
	}
	return &DispatchResult{
		Handled:   false,
		ErrorCode: code,
		AckEnvelope: map[string]any{
			"type": "session.error",
			"payload": map[string]any{
				"code":    string(code),
				"message": msg,
			},
		},
	}, err
}

// provisionFailureResult 将 member.provision 失败转为 provision_result(status=error)，
// 让 Manage complete_provision 推进成员状态，避免永久停在 provisioning（UI「配置中」）。
func provisionFailureResult(env WSEnvelope, req ProvisionRequest, err error) (*DispatchResult, error) {
	code := CodeConflict
	msg := fmt.Sprint(err)
	if we, ok := err.(*Error); ok {
		code = we.Code
		msg = we.Message
	}
	if err == nil {
		msg = "member provision failed"
	}
	provisionID := strings.TrimSpace(req.ProvisionID)
	memberID := strings.TrimSpace(req.MemberID)
	workgroupID := strings.TrimSpace(req.WorkgroupID)
	if env.Payload != nil {
		if provisionID == "" {
			provisionID, _ = env.Payload["provision_id"].(string)
		}
		if memberID == "" {
			memberID, _ = env.Payload["member_id"].(string)
		}
		if workgroupID == "" {
			workgroupID, _ = env.Payload["workgroup_id"].(string)
		}
	}
	wgID := deliveryWorkgroupID(env, workgroupID)
	return &DispatchResult{
		Handled:        true,
		PendingAck:     env.DeliverySeq > 0 && strings.TrimSpace(wgID) != "",
		AckWorkgroupID: wgID,
		AckDeliverySeq: env.DeliverySeq,
		ErrorCode:      code,
		AckEnvelope: map[string]any{
			"type": "member.provision_result",
			"payload": map[string]any{
				"provision_id":          provisionID,
				"member_id":             memberID,
				"workgroup_id":          firstNonEmpty(workgroupID, wgID),
				"workspace_path":        "",
				"tool_catalog_revision": "",
				"status":                "error",
				"error_code":            string(code),
				"message":               msg,
				"delivery_seq":          env.DeliverySeq,
				"connection_generation": env.ConnectionGeneration,
			},
		},
	}, err
}

func isTerminalCommandStatus(status string) bool {
	switch status {
	case "succeeded", "failed", "indeterminate", "rejected", "canceled":
		return true
	default:
		return false
	}
}

// toolCommandFailureResult 将 tool.command 处理失败转为 tool.result，唤醒 Manage waiter。
func toolCommandFailureResult(env WSEnvelope, cmd ToolCommand, err error) (*DispatchResult, error) {
	code := CodeConflict
	msg := fmt.Sprint(err)
	if we, ok := err.(*Error); ok {
		code = we.Code
		msg = we.Message
	}
	if err == nil {
		msg = "tool command failed"
	}
	wgID := deliveryWorkgroupID(env, cmd.WorkgroupID)
	status := "failed"
	if code == CodeIndeterminate {
		status = "indeterminate"
	}
	payload := map[string]any{
		"command_id":            cmd.CommandID,
		"status":                status,
		"error_code":            string(code),
		"result_text":           msg,
		"result_json":           "",
		"delivery_seq":          env.DeliverySeq,
		"workgroup_id":          firstNonEmpty(cmd.WorkgroupID, wgID),
		"member_id":             cmd.MemberID,
		"assign_id":             cmd.AssignID,
		"connection_generation": env.ConnectionGeneration,
	}
	return &DispatchResult{
		Handled:        true,
		PendingAck:     env.DeliverySeq > 0,
		AckWorkgroupID: wgID,
		AckDeliverySeq: env.DeliverySeq,
		ErrorCode:      code,
		AckEnvelope: map[string]any{
			"type":    "tool.result",
			"payload": payload,
		},
	}, err
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
		strings.TrimSpace(req.AssignID) == "" || strings.TrimSpace(req.UserMessage) == "" {
		return req, errf(CodeSchemaMismatch, "agent.turn.start requires workgroup_id, member_id, agent_id, session_id, assign_id, user_message")
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
		strings.TrimSpace(req.AssignID) == "" || len(req.ResumeValue) == 0 {
		return req, errf(CodeSchemaMismatch, "agent.turn.resume requires workgroup_id, member_id, agent_id, session_id, assign_id, resume_value")
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
		"turn_id":           req.TurnID,
		"client_message_id": req.ClientMessageID,
	}
}

func agentTurnCancelPayload(req AgentTurnCancelRequest) map[string]any {
	return map[string]any{
		"workgroup_id": req.WorkgroupID,
		"member_id":    req.MemberID,
		"agent_id":     req.AgentID,
		"session_id":   req.SessionID,
		"assign_id":    req.AssignID,
		"status":       "canceled",
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
	out := mergeMaps(base, map[string]any{
		"workgroup_id":          firstNonEmpty(payloadString(base["workgroup_id"]), env.WorkgroupID),
		"delivery_seq":          env.DeliverySeq,
		"connection_generation": generation,
	})
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

func provisionFromPayload(p map[string]any) (ProvisionRequest, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return ProvisionRequest{}, err
	}
	var aux struct {
		ProvisionID      string   `json:"provision_id"`
		WorkgroupID      string   `json:"workgroup_id"`
		MemberID         string   `json:"member_id"`
		HomeNodeID       string   `json:"home_node_id"`
		MemberSpecDigest string   `json:"member_spec_digest"`
		LeaseEpoch       int64    `json:"lease_epoch"`
		MemberGeneration int64    `json:"member_generation"`
		ToolAllowNames   []string `json:"tool_allow_names"`
		WorkspaceRoot    string   `json:"workspace_root"`
	}
	if err := json.Unmarshal(raw, &aux); err != nil {
		return ProvisionRequest{}, errf(CodeSchemaMismatch, "%v", err)
	}
	return ProvisionRequest{
		ProvisionID:      aux.ProvisionID,
		WorkgroupID:      aux.WorkgroupID,
		MemberID:         aux.MemberID,
		HomeNodeID:       aux.HomeNodeID,
		MemberSpecDigest: aux.MemberSpecDigest,
		LeaseEpoch:       aux.LeaseEpoch,
		MemberGeneration: aux.MemberGeneration,
		ToolAllowNames:   aux.ToolAllowNames,
		WorkspaceRoot:    aux.WorkspaceRoot,
	}, nil
}

func toolCommandFromPayload(p map[string]any) (ToolCommand, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return ToolCommand{}, err
	}
	var cmd ToolCommand
	if err := json.Unmarshal(raw, &cmd); err != nil {
		return ToolCommand{}, errf(CodeSchemaMismatch, "%v", err)
	}
	return cmd, nil
}

func cancelFromPayload(p map[string]any) (commandID, workgroupID, assignID, memberID string) {
	if p == nil {
		return "", "", "", ""
	}
	return strFromAny(p["command_id"]), strFromAny(p["workgroup_id"]), strFromAny(p["assign_id"]), strFromAny(p["member_id"])
}

func strFromAny(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		return ""
	}
}

func mergeMaps(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func extractResultText(resultJSON string) string {
	if resultJSON == "" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(resultJSON), &m); err != nil {
		return resultJSON
	}
	if c, ok := m["content"].(string); ok {
		return c
	}
	return resultJSON
}
