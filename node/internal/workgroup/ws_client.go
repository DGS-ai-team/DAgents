package workgroup

import (
	"encoding/json"
)

// ClientSession 将本地 Session 与 Manage 侧 hello/resume 载荷对齐（无真实拨号）。
// 真实 net.Conn/WebSocket 拨号可在后续挂到 Dialer；D3 先固化帧处理契约。
type ClientSession struct {
	Worker *Worker
}

// BuildHello 构造 session.hello 载荷。
func (c *ClientSession) BuildHello() map[string]any {
	cur := c.Worker.Session.OfferResume()
	gen := c.Worker.Connect()
	return map[string]any{
		"type": "session.hello",
		"payload": map[string]any{
			"node_id":                c.Worker.NodeID,
			"last_ack_delivery_seq":  cur.LastAckDeliverySeq,
			"connection_generation":  gen,
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
		c.Worker.mu.Lock()
		c.Worker.Commands.ConnectionGeneration = g
		c.Worker.mu.Unlock()
	}
}

// HandleIncomingJSON 解析并分发一条下行 JSON 帧。
func (c *ClientSession) HandleIncomingJSON(raw []byte) (*DispatchResult, error) {
	var env WSEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		// 控制面消息（welcome/resume）不是完整 WSEnvelope
		var loose map[string]any
		if err2 := json.Unmarshal(raw, &loose); err2 != nil {
			return nil, errf(CodeSchemaMismatch, "invalid json: %v", err)
		}
		t, _ := loose["type"].(string)
		switch t {
		case "session.welcome":
			payload, _ := loose["payload"].(map[string]any)
			c.ApplyWelcome(payload)
			return &DispatchResult{Handled: true}, nil
		case "resume.complete", "resume.error", "resume.batch", "delivery.acked", "session.error":
			return &DispatchResult{Handled: true}, nil
		default:
			return nil, errf(CodeSchemaMismatch, "not a WSEnvelope: %v", err)
		}
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
