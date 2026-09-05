package browser

import "encoding/json"

// LLMSettings 为 Node 解析后的单次浏览器任务模型配置。
// 它只在 Node 到本机 sidecar 的瞬时请求中传输，不作为 sidecar 配置落盘。
type LLMSettings struct {
	Provider          string `json:"provider"`
	BaseURL           string `json:"base_url,omitempty"`
	Model             string `json:"model"`
	APIKeyEnv         string `json:"api_key_env,omitempty"`
	APIKey            string `json:"api_key,omitempty"`
	Mock              bool   `json:"mock,omitempty"`
	MultimodalEnabled bool   `json:"multimodal_enabled,omitempty"`
	Thinking          string `json:"thinking,omitempty"`
	ReasoningEffort   string `json:"reasoning_effort,omitempty"`
}

// Request 为 BrowserManager → dagents-browser 的内部请求（任务级 + session 生命周期）。
type Request struct {
	Op         string `json:"op"`
	SessionKey string `json:"session_key,omitempty"`
	Headed     *bool  `json:"headed,omitempty"`
	ViewportW  int    `json:"viewport_width,omitempty"`
	ViewportH  int    `json:"viewport_height,omitempty"`
	TimeoutMS  int    `json:"timeout_ms,omitempty"`
	// 任务级伴生派发（op=run_task / task_status / task_cancel）
	Task     string       `json:"task,omitempty"`
	TaskID   string       `json:"task_id,omitempty"`
	MaxSteps int          `json:"max_steps,omitempty"`
	LLM      *LLMSettings `json:"llm,omitempty"`
}

// Response 为 dagents-browser → BrowserManager 的内部响应。
type Response struct {
	OK             bool           `json:"ok"`
	URL            string         `json:"url,omitempty"`
	Title          string         `json:"title,omitempty"`
	ScreenshotPath string         `json:"screenshot_path,omitempty"`
	Error          string         `json:"error,omitempty"`
	Detail         map[string]any `json:"detail,omitempty"`
}

// ToolResult 为 browser_* 工具返回给 LLM 的统一 JSON 形状。
type ToolResult struct {
	OK                bool           `json:"ok"`
	URL               string         `json:"url,omitempty"`
	Title             string         `json:"title,omitempty"`
	ScreenshotPath    string         `json:"screenshot_path,omitempty"`
	LLMRepresentation string         `json:"llm_representation,omitempty"`
	ExtractedContent  string         `json:"extracted_content,omitempty"`
	Error             string         `json:"error,omitempty"`
	Detail            map[string]any `json:"detail,omitempty"`
}

// FormatToolResult 序列化 tool 返回文本。
func FormatToolResult(r ToolResult) string {
	raw, err := json.Marshal(r)
	if err != nil {
		return `{"ok":false,"error":"marshal tool result"}`
	}
	return string(raw)
}

func toolResultFromResponse(resp Response) ToolResult {
	out := ToolResult{
		OK:             resp.OK,
		URL:            resp.URL,
		Title:          resp.Title,
		ScreenshotPath: resp.ScreenshotPath,
		Error:          resp.Error,
		Detail:         resp.Detail,
	}
	if resp.Detail != nil {
		detail := make(map[string]any, len(resp.Detail))
		for k, v := range resp.Detail {
			if k == "llm_representation" {
				if s, ok := v.(string); ok && s != "" {
					out.LLMRepresentation = s
				}
				continue
			}
			if k == "extracted_content" {
				if s, ok := v.(string); ok && s != "" {
					out.ExtractedContent = s
				}
				continue
			}
			if k == "summary" {
				if s, ok := v.(string); ok && s != "" && out.ExtractedContent == "" {
					out.ExtractedContent = s
				}
			}
			detail[k] = v
		}
		if len(detail) > 0 {
			out.Detail = detail
		}
	}
	return out
}
