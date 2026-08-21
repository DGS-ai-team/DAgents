/**
 * Ephemeral realtime frames sent by Manage through the Node workgroup SSE.
 * Timeline events are intentionally open-ended and are rendered from the
 * durable event payload; only realtime frames need a local type registry.
 */
export const WORKGROUP_REALTIME_EVENT_TYPES = Object.freeze([
  "queued",
  "status",
  "delta",
  "assistant_final",
  "final",
]);

export function isKnownWorkgroupRealtimeEvent(type) {
  return WORKGROUP_REALTIME_EVENT_TYPES.includes(String(type || "").trim());
}
