package llm

import "strings"

const chatCompletionsPath = "/chat/completions"

// normalizeOpenAIBaseURL 去掉首尾空白与尾部斜杠；若误把完整路径写进 base_url 则剥掉 /chat/completions。
func normalizeOpenAIBaseURL(base string) string {
	base = strings.TrimSpace(base)
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, chatCompletionsPath) {
		base = strings.TrimSuffix(base, chatCompletionsPath)
		base = strings.TrimRight(base, "/")
	}
	return base
}

// normalizeQwenBaseURL 补全百炼业务空间 MaaS 域名常见缺省路径。
func normalizeQwenBaseURL(base string) string {
	if base == "" {
		return base
	}
	if strings.Contains(base, ".maas.aliyuncs.com") && !strings.Contains(base, "/compatible-mode/") {
		return base + "/compatible-mode/v1"
	}
	return base
}

func resolveBaseURL(provider ProviderName, configured string) string {
	base := normalizeOpenAIBaseURL(configured)
	if base == "" {
		return defaultBaseURL(provider)
	}
	if provider == ProviderQwen {
		base = normalizeQwenBaseURL(base)
	}
	return base
}

func chatCompletionsEndpoint(baseURL string) string {
	return normalizeOpenAIBaseURL(baseURL) + chatCompletionsPath
}

// mismatchBaseURLWarning 在 provider 与 base_url 明显不一致时返回告警文案（供启动日志）。
func mismatchBaseURLWarning(provider ProviderName, configured string) string {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return ""
	}
	lower := strings.ToLower(configured)
	switch provider {
	case ProviderQwen:
		if strings.Contains(lower, "deepseek.com") || strings.Contains(lower, "api.openai.com") {
			return "llm.base_url 与 provider=qwen 不匹配，易出现 llm http 404"
		}
	case ProviderDeepSeek:
		if strings.Contains(lower, "dashscope.aliyuncs.com") || strings.Contains(lower, ".maas.aliyuncs.com") {
			return "llm.base_url 与 provider=deepseek 不匹配"
		}
	}
	return ""
}
