package browser

import "encoding/json"

// Action is one deterministic browser operation executed by browser_call.
// Target and Params are validated again by the sidecar according to Op.
type Action struct {
	Op        string         `json:"op"`
	PageID    string         `json:"page_id,omitempty"`
	Target    map[string]any `json:"target,omitempty"`
	Params    map[string]any `json:"params,omitempty"`
	TimeoutMS int            `json:"timeout_ms,omitempty"`
}

// Request is the Node-to-sidecar protocol payload. Op is call, evaluate,
// start, stop, or ping; start/stop are translated to one-action calls by the
// Manager and are retained only for lifecycle internals.
type Request struct {
	Op         string         `json:"op"`
	SessionKey string         `json:"session_key,omitempty"`
	Headed     *bool          `json:"headed,omitempty"`
	ViewportW  int            `json:"viewport_width,omitempty"`
	ViewportH  int            `json:"viewport_height,omitempty"`
	TimeoutMS  int            `json:"timeout_ms,omitempty"`
	CallID     string         `json:"call_id,omitempty"`
	Actions    []Action       `json:"actions,omitempty"`
	Script     string         `json:"script,omitempty"`
	Arg        any            `json:"arg,omitempty"`
	PageID     string         `json:"page_id,omitempty"`
	Target     map[string]any `json:"target,omitempty"`
}

// Response is the sidecar response envelope. Detail carries the stable v2
// result payload and any driver-only metadata is removed by the Node tool.
type Response struct {
	OK        bool           `json:"ok"`
	URL       string         `json:"url,omitempty"`
	Title     string         `json:"title,omitempty"`
	Error     string         `json:"error,omitempty"`
	ErrorCode string         `json:"error_code,omitempty"`
	Detail    map[string]any `json:"detail,omitempty"`
}

// ToolResult is the JSON envelope returned to the model. Browser v2 results
// live in Detail; execution-specific metadata remains inside that object.
type ToolResult struct {
	OK        bool           `json:"ok"`
	URL       string         `json:"url,omitempty"`
	Title     string         `json:"title,omitempty"`
	Error     string         `json:"error,omitempty"`
	ErrorCode string         `json:"error_code,omitempty"`
	Detail    map[string]any `json:"detail,omitempty"`
}

func FormatToolResult(r ToolResult) string {
	raw, err := json.Marshal(r)
	if err != nil {
		return `{"ok":false,"error":"marshal tool result"}`
	}
	return string(raw)
}

func toolResultFromResponse(resp Response) ToolResult {
	detail := resp.Detail
	return ToolResult{OK: resp.OK, URL: resp.URL, Title: resp.Title, Error: resp.Error, ErrorCode: resp.ErrorCode, Detail: detail}
}
