package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ModelInfo 为 OpenAI 兼容 /models 列表项。
type ModelInfo struct {
	ID string `json:"id"`
}

// ProbeModelsResult 为探测远端模型列表的结果。
type ProbeModelsResult struct {
	Models            []ModelInfo `json:"models"`
	SuggestedProvider string      `json:"suggested_provider,omitempty"`
}

type openAIModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// modelsEndpoint 拼接 OpenAI 兼容 GET /models。
func modelsEndpoint(baseURL string) string {
	return normalizeOpenAIBaseURL(baseURL) + "/models"
}

// SuggestProviderFromBaseURL 根据 base_url 启发式推断 provider（可为空）。
func SuggestProviderFromBaseURL(baseURL string) string {
	lower := strings.ToLower(strings.TrimSpace(baseURL))
	switch {
	case strings.Contains(lower, "deepseek.com"):
		return string(ProviderDeepSeek)
	case strings.Contains(lower, "dashscope.aliyuncs.com"), strings.Contains(lower, ".maas.aliyuncs.com"):
		return string(ProviderQwen)
	case strings.Contains(lower, "api.openai.com"), strings.Contains(lower, "openai.com"):
		return string(ProviderOpenAI)
	case strings.Contains(lower, "bigmodel.cn"), strings.Contains(lower, "api.z.ai"):
		return string(ProviderGLM)
	case strings.Contains(lower, "minimaxi.com"), strings.Contains(lower, "minimax.io"):
		return string(ProviderMiniMax)
	case strings.Contains(lower, "xiaomimimo.com"), strings.Contains(lower, "mimo-v2.com"):
		return string(ProviderMiMo)
	case strings.Contains(lower, "127.0.0.1"), strings.Contains(lower, "localhost"):
		return string(ProviderVLLM)
	default:
		return ""
	}
}

// ProbeModels 请求 OpenAI 兼容 GET {base}/models，返回模型 id 列表。
func ProbeModels(ctx context.Context, provider ProviderName, baseURL, apiKey string) (ProbeModelsResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	base := resolveBaseURL(provider, baseURL)
	if base == "" {
		return ProbeModelsResult{}, fmt.Errorf("base_url is required")
	}
	endpoint := modelsEndpoint(base)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ProbeModelsResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	if key := strings.TrimSpace(apiKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ProbeModelsResult{}, fmt.Errorf("请求 /models 失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 400 {
			msg = msg[:400] + "…"
		}
		if msg == "" {
			msg = resp.Status
		}
		return ProbeModelsResult{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
	}
	var parsed openAIModelsResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ProbeModelsResult{}, fmt.Errorf("解析 /models 响应失败: %w", err)
	}
	out := ProbeModelsResult{
		Models:            make([]ModelInfo, 0, len(parsed.Data)),
		SuggestedProvider: SuggestProviderFromBaseURL(base),
	}
	seen := map[string]struct{}{}
	for _, item := range parsed.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out.Models = append(out.Models, ModelInfo{ID: id})
	}
	if len(out.Models) == 0 {
		return ProbeModelsResult{}, fmt.Errorf("/models 未返回可用模型")
	}
	return out, nil
}
