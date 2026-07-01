package browser

import "encoding/json"

// Request 为 BrowserManager → dagents-browser 的内部请求。
type Request struct {
	Op          string   `json:"op"`
	SessionKey  string   `json:"session_key,omitempty"`
	Headed      *bool    `json:"headed,omitempty"`
	ViewportW   int      `json:"viewport_width,omitempty"`
	ViewportH   int      `json:"viewport_height,omitempty"`
	URL         string   `json:"url,omitempty"`
	Index       int      `json:"index,omitempty"`
	Selector    string   `json:"selector,omitempty"`
	Fallbacks   []string `json:"selector_fallbacks,omitempty"`
	Text        string   `json:"text,omitempty"`
	Key         string   `json:"key,omitempty"`
	Path        string   `json:"path,omitempty"`
	WaitUntil   string   `json:"wait_until,omitempty"`
	LoadState       string   `json:"load_state,omitempty"`
	TimeoutMS       int      `json:"timeout_ms,omitempty"`
	MaxElements       int    `json:"max_elements,omitempty"`
	IncludeScreenshot bool   `json:"include_screenshot,omitempty"`
	CoordX            int    `json:"coordinate_x,omitempty"`
	CoordY            int    `json:"coordinate_y,omitempty"`
	Button            string   `json:"button,omitempty"`
	Query             string   `json:"query,omitempty"`
	Engine            string   `json:"engine,omitempty"`
	Down              *bool    `json:"down,omitempty"`
	Pages             float64  `json:"pages,omitempty"`
	TabID             string   `json:"tab_id,omitempty"`
	Pattern           string   `json:"pattern,omitempty"`
	Regex             bool     `json:"regex,omitempty"`
	CaseSensitive     bool     `json:"case_sensitive,omitempty"`
	ContextChars      int      `json:"context_chars,omitempty"`
	CSSScope          string   `json:"css_scope,omitempty"`
	MaxResults        int      `json:"max_results,omitempty"`
	Attributes        []string `json:"attributes,omitempty"`
	IncludeText       *bool    `json:"include_text,omitempty"`
	Code              string   `json:"code,omitempty"`
	ExtractLinks      bool     `json:"extract_links,omitempty"`
	ExtractImages     bool     `json:"extract_images,omitempty"`
	StartFromChar     int      `json:"start_from_char,omitempty"`
	AlreadyCollected  []string `json:"already_collected,omitempty"`
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
	OK                  bool           `json:"ok"`
	URL                 string         `json:"url,omitempty"`
	Title               string         `json:"title,omitempty"`
	ScreenshotPath      string         `json:"screenshot_path,omitempty"`
	LLMRepresentation   string         `json:"llm_representation,omitempty"`
	ExtractedContent    string         `json:"extracted_content,omitempty"`
	Error               string         `json:"error,omitempty"`
	Detail              map[string]any `json:"detail,omitempty"`
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
			detail[k] = v
		}
		if len(detail) > 0 {
			out.Detail = detail
		}
	}
	return out
}

func errorResult(msg string) string {
	return FormatToolResult(ToolResult{OK: false, Error: msg})
}
