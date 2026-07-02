package llm

import "testing"

func TestNormalizeOpenAIBaseURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://api.openai.com/v1/", "https://api.openai.com/v1"},
		{
			"https://ws-i94zryik2nyqcc90.cn-beijing.maas.aliyuncs.com/compatible-mode/v1/chat/completions",
			"https://ws-i94zryik2nyqcc90.cn-beijing.maas.aliyuncs.com/compatible-mode/v1",
		},
		{"  https://api.deepseek.com  ", "https://api.deepseek.com"},
	}
	for _, tc := range cases {
		if got := normalizeOpenAIBaseURL(tc.in); got != tc.want {
			t.Fatalf("normalizeOpenAIBaseURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeQwenBaseURL(t *testing.T) {
	in := "https://ws-i94zryik2nyqcc90.cn-beijing.maas.aliyuncs.com"
	want := in + "/compatible-mode/v1"
	if got := normalizeQwenBaseURL(in); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	already := "https://ws-i94zryik2nyqcc90.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
	if got := normalizeQwenBaseURL(already); got != already {
		t.Fatalf("got %q want %q", got, already)
	}
}

func TestResolveBaseURL_qwenWorkspace(t *testing.T) {
	got := resolveBaseURL(ProviderQwen, "https://ws-i94zryik2nyqcc90.cn-beijing.maas.aliyuncs.com/compatible-mode/v1")
	want := "https://ws-i94zryik2nyqcc90.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestChatCompletionsEndpoint(t *testing.T) {
	base := "https://ws-i94zryik2nyqcc90.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
	want := base + "/chat/completions"
	if got := chatCompletionsEndpoint(base); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestMismatchBaseURLWarning(t *testing.T) {
	if got := mismatchBaseURLWarning(ProviderQwen, "https://api.deepseek.com"); got == "" {
		t.Fatal("expected warning for qwen + deepseek base_url")
	}
	if got := mismatchBaseURLWarning(ProviderQwen, "https://ws-i94zryik2nyqcc90.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"); got != "" {
		t.Fatalf("unexpected warning: %q", got)
	}
}
