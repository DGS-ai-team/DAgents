package llm

// qwenAdapter 对齐 DashScope OpenAI 兼容模式（reasoning_content + enable_thinking）。
// 出站 messages 规则与 DeepSeek 相同；顶层 enable_thinking / thinking_budget 由 RuntimeSettings 注入。
type qwenAdapter struct {
	deepSeekAdapter
}

func (qwenAdapter) Name() ProviderName { return ProviderQwen }
