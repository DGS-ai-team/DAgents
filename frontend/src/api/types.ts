/* eslint-disable */
/**
 * 由脚本自动生成，请勿手改。
 * 来源: ./openapi.json
 */

// -----------------------------
// Components Schemas
// -----------------------------

/** 取消当前推理 turn 的响应（无在途任务时 `cancelled=false`）。 */
export interface CancelTurnResult {
  session_id: string;
  cancelled: boolean;
}

export interface HTTPValidationError {
  detail?: Array<ValidationError>;
}

export interface MessageIn {
  session_id: string;
  client_id?: string;
  request_type?: "message" | "resume";
  content?: string | null;
  resume_value?: unknown | null;
  source?: string;
  priority?: "tool_result" | "human" | "resume" | "other" | null;
}

export interface SessionCreateIn {
  session_id?: string | null;
}

export interface SessionCreateResult {
  session_id: string;
  created: boolean;
}

export interface SubmitResult {
  accepted: boolean;
  session_id: string;
  priority: "tool_result" | "human" | "resume" | "other";
}

export interface ValidationError {
  loc: Array<string | number>;
  msg: string;
  type: string;
  input?: unknown;
  ctx?: Record<string, unknown>;
}

// -----------------------------
// Operations
// -----------------------------

export interface ApiOperationMap {
  "health_health_get": { method: "GET"; path: "/health"; requestBody: never; response: Record<string, string>; };
  "prometheus_metrics_metrics_get": { method: "GET"; path: "/metrics"; requestBody: never; response: unknown; };
  "create_session_v1_sessions_post": { method: "POST"; path: "/v1/sessions"; requestBody: SessionCreateIn; response: SessionCreateResult; };
  "cancel_current_turn_v1_sessions__session_id__cancel_post": { method: "POST"; path: "/v1/sessions/{session_id}/cancel"; requestBody: never; response: CancelTurnResult; };
  "submit_message_v1_messages_post": { method: "POST"; path: "/v1/messages"; requestBody: MessageIn; response: SubmitResult; };
  "stream_all_v1_streams_get": { method: "GET"; path: "/v1/streams"; requestBody: never; response: unknown; };
}
