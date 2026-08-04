package queue

// 与 Python MessageEnvelope.request_type 对齐的入队类型。
const (
	RequestTypeMessage            = "message"
	RequestTypeResume             = "resume"
	RequestTypeAsyncToolResult    = "async_tool_result"
	RequestTypeToolResult         = "tool_result"
	RequestTypeTriggerMessage     = "trigger_message"
	RequestTypeSideEffectContinue = "side_effect_continue"
)

// AsyncToolResultPayload 为异步工具完成回灌载荷（对齐 Python async_tool_result）。
type AsyncToolResultPayload struct {
	JobID                  string `json:"job_id"`
	ToolName               string `json:"tool_name"`
	ToolCallID             string `json:"tool_call_id"`
	Status                 string `json:"status"`
	ResultText             string `json:"result_text"`
	ErrorText              string `json:"error_text"`
	OutputCompressSavedPct int    `json:"output_compress_saved_pct,omitempty"`
	OutputCompressRawRunes int    `json:"output_compress_raw_runes,omitempty"`
	OutputCompressOutRunes int    `json:"output_compress_out_runes,omitempty"`
}
