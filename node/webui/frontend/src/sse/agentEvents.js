/**
 * The named events emitted on the normal per-agent SSE stream.
 *
 * Keep this registry next to the transport rather than duplicating the list
 * in a view. EventSource does not expose a wildcard listener for named
 * events, so adding a backend event without adding it here makes the event
 * silently disappear from the Web UI.
 */
export const AGENT_STREAM_EVENT_POLICIES = Object.freeze({
  assistant: "transcript",
  reasoning: "transcript",
  execution: "activity",
  tool_call: "transcript",
  tool_result: "transcript",
  usage: "transcript",
  turn_state: "turn",
  error: "transcript",
  done: "turn",
  hitl_required: "hitl",
  temporary_agent_created: "child-agent",
  temporary_agent_completed: "child-agent",
  temporary_agent_cancelled: "child-agent",
  context_compression_blocking: "compression",
  context_compression_silent: "compression",
  user_message_deferred: "side-effect",
  side_effect_turn_start: "side-effect",
  side_effect_applied: "side-effect",
  side_effects_cleared: "side-effect",
  "runtime/config-changed": "runtime-config",
  "memory/changed": "runtime-memory",
  "skills/changed": "runtime-skills",
  "mcp/catalog-changed": "runtime-mcp",
  system_notice: "notice",
  "terminal.opened": "terminal",
  "terminal.updated": "terminal",
  "terminal.closed": "terminal",
});

export const AGENT_STREAM_EVENT_TYPES = Object.freeze(
  Object.keys(AGENT_STREAM_EVENT_POLICIES),
);

export function getAgentStreamEventPolicy(type) {
  return AGENT_STREAM_EVENT_POLICIES[String(type || "").trim()] || "unknown";
}
