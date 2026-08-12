//go:build integration

package llm

import (
	"context"
	"os"
	"strings"
	"testing"
)

// 本地探测百炼 Qwen OpenAI 兼容接口：
//
//	QWEN_API_KEY=sk-... \
//	QWEN_BASE_URL=https://ws-xxx.cn-beijing.maas.aliyuncs.com/compatible-mode/v1 \
//	go test -tags integration -run TestQwenWorkspaceLive -v ./node/internal/llm/
func TestQwenWorkspaceLive(t *testing.T) {
	key := strings.TrimSpace(os.Getenv("QWEN_API_KEY"))
	if key == "" {
		t.Skip("QWEN_API_KEY not set")
	}
	baseURL := strings.TrimSpace(os.Getenv("QWEN_BASE_URL"))
	if baseURL == "" {
		baseURL = "https://ws-i94zryik2nyqcc90.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
	}
	model := strings.TrimSpace(os.Getenv("QWEN_MODEL"))
	if model == "" {
		model = "qwen-plus"
	}

	client := NewOpenAIClient(OpenAIConfig{
		BaseURL: baseURL,
		Model:   model,
		APIKey:  key,
	})
	endpoint := chatCompletionsEndpoint(baseURL)
	t.Logf("POST %s model=%s", endpoint, model)

	text, err := client.CompleteText(context.Background(), CompleteRequest{
		UserPrompt: "只回复 ok",
	})
	if err != nil {
		t.Fatalf("CompleteText: %v", err)
	}
	if strings.TrimSpace(text) == "" {
		t.Fatal("empty completion")
	}
	t.Logf("reply: %q", text)
}
