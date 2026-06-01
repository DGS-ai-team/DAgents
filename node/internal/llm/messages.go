package llm

import "strings"

// MessagesWithSystem 在对话 history 前插入 system 消息（不落库 history）。

// 逻辑：
// 1. systemPrompt 为空则原样返回 history；
// 2. 否则构造 role=system 消息并 prepend。
func MessagesWithSystem(systemPrompt string, history []Message) []Message {
	if strings.TrimSpace(systemPrompt) == "" {
		return history
	}
	out := make([]Message, 0, len(history)+1)
	out = append(out, Message{Role: "system", Content: systemPrompt})
	out = append(out, history...)
	return out
}
