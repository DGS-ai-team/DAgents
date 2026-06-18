package llm

import "testing"

func TestDefaultBaseURL(t *testing.T) {
	cases := []struct {
		provider ProviderName
		want     string
	}{
		{ProviderDeepSeek, defaultDeepSeekBaseURL},
		{ProviderQwen, defaultQwenBaseURL},
		{ProviderVLLM, defaultVLLMBaseURL},
		{ProviderOpenAI, ""},
	}
	for _, tc := range cases {
		if got := defaultBaseURL(tc.provider); got != tc.want {
			t.Fatalf("provider=%s got %q want %q", tc.provider, got, tc.want)
		}
	}
}

func TestNewMessageAdapter_providers(t *testing.T) {
	for provider, want := range map[string]ProviderName{
		"deepseek": ProviderDeepSeek,
		"qwen":     ProviderQwen,
		"vllm":     ProviderVLLM,
		"openai":   ProviderOpenAI,
		"unknown":  ProviderOpenAI,
	} {
		if got := NewMessageAdapter(provider).Name(); got != want {
			t.Fatalf("provider=%s got %s want %s", provider, got, want)
		}
	}
}
