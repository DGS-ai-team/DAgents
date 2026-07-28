package stream

// Publisher 为 turn 编排向 SSE 投递事件的接口；*Hub 与子 Agent RelayHub 均实现。
type Publisher interface {
	Publish(agentID, eventType string, data map[string]any) Event
}
