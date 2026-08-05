package workgroup

import (
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
	PendingAck         bool
	AckWorkgroupID     string
	AckDeliverySeq     int64
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
	case "member.provision":
		req, err := provisionFromPayload(env.Payload)
		if err != nil {
			return nil, err
		}
		res, err := w.HandleProvision(req)
		if err != nil {
			return dispatchErr(err)
		}
		wgID := deliveryWorkgroupID(env, res.Binding.WorkgroupID)
		return &DispatchResult{
			Handled:            true,
			PendingAck:         true,
			AckWorkgroupID:     wgID,
			AckDeliverySeq:     env.DeliverySeq,
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
			return nil, err
		}
		res, err := w.HandleCommand(cmd)
		// rejected 时 HandleCommand 仍返回 AcceptResult + error
		if res == nil {
			return dispatchErr(err)
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
			Handled:            true,
			PendingAck:         true,
			AckWorkgroupID:     wgID,
			AckDeliverySeq:     env.DeliverySeq,
			AckEnvelope:        ack,
		}
		if res.Entry.Status == "succeeded" || res.Entry.Status == "failed" || res.Entry.Status == "indeterminate" || res.Entry.Status == "rejected" {
			out.AckEnvelope = map[string]any{
				"type": "tool.result",
				"payload": mergeMaps(ackPayload, map[string]any{
					"result_json": res.Entry.ResultJSON,
					"error_code":  res.Entry.ErrorCode,
					"status":      res.Entry.Status,
					"result_text": extractResultText(res.Entry.ResultJSON),
				}),
			}
		}
		if err != nil && res.Rejected {
			out.ErrorCode = res.ErrorCode
			return out, err
		}
		return out, nil

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
			Handled:            true,
			PendingAck:         true,
			AckWorkgroupID:     wgID,
			AckDeliverySeq:     env.DeliverySeq,
			AckEnvelope: map[string]any{
				"type": "workgroup.tombstone_ack",
				"payload": map[string]any{
					"workgroup_id":          t.WorkgroupID,
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
