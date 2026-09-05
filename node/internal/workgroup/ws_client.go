package workgroup

import (
	"encoding/json"
	"time"
)

// ClientSession 将本地 Session 与 Manage 侧 hello/resume 载荷对齐。
type ClientSession struct {
	Worker     *Worker
	OnRealtime func(map[string]any)
}

// BuildHello 构造 session.hello 载荷。
func (c *ClientSession) BuildHello() map[string]any {
	cur := c.Worker.Session.OfferResume()
	gen := c.Worker.Connect()
	return map[string]any{
		"type": "session.hello",
		"payload": map[string]any{
			"node_id":               c.Worker.NodeID,
			"protocol_version":      ProtocolVersion,
			"schema_version":        SchemaVersion,
			"last_ack_delivery_seq": cur.LastAckDeliverySeq,
			"connection_generation": gen,
			"capabilities":          append([]string(nil), c.Worker.Capabilities...),
			"client_time":           time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
}

// ApplyWelcome 应用 session.welcome（对齐 Manage 分配的 generation）。
func (c *ClientSession) ApplyWelcome(payload map[string]any) {
	if g, ok := asInt64(payload["connection_generation"]); ok && g > 0 {
		c.Worker.Session.mu.Lock()
		c.Worker.Session.ConnectionGeneration = g
		c.Worker.Session.Active = true
		c.Worker.Session.mu.Unlock()
	}
}

// HandleIncomingJSON 解析并分发一条下行 JSON 帧。
func (c *ClientSession) HandleIncomingJSON(raw []byte) (*DispatchResult, error) {
	var loose map[string]any
	if err := json.Unmarshal(raw, &loose); err == nil {
		switch t, _ := loose["type"].(string); t {
		case "workgroup.realtime":
			if c.OnRealtime != nil {
				if payload, ok := loose["payload"].(map[string]any); ok {
					c.OnRealtime(payload)
				}
			}
			return &DispatchResult{Handled: true}, nil
		case "session.welcome":
			payload, _ := loose["payload"].(map[string]any)
			c.ApplyWelcome(payload)
			return &DispatchResult{Handled: true}, nil
		case "resume.complete", "resume.error", "resume.batch", "delivery.acked", "session.error":
			return &DispatchResult{Handled: true}, nil
		}
	}
	var env WSEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, errf(CodeSchemaMismatch, "invalid json: %v", err)
	}
	return c.Worker.DispatchEnvelope(env)
}

func asInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case float64:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}
