package llm

// vllmAdapter 为本地/自托管 vLLM OpenAI 兼容端点；行为与 openai 相同（不注入厂商扩展字段）。
type vllmAdapter struct {
	openAIAdapter
}

func (vllmAdapter) Name() ProviderName { return ProviderVLLM }
