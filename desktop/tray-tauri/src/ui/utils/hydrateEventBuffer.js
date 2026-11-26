export const DEFAULT_HYDRATE_EVENT_BUFFER_LIMIT = 512;
export const DEFAULT_HYDRATE_EVENT_BUFFER_BYTES = 8 * 1024 * 1024;

function approximateEventBytes(root, limit) {
  const pending = [root];
  const visited = new WeakSet();
  let bytes = 0;
  let entries = 0;
  while (pending.length && bytes <= limit) {
    const value = pending.pop();
    if (typeof value === "string") {
      bytes += value.length * 2;
      continue;
    }
    if (typeof value === "number" || typeof value === "boolean") {
      bytes += 8;
      continue;
    }
    if (!value || typeof value !== "object" || visited.has(value)) continue;
    visited.add(value);
    for (const [key, child] of Object.entries(value)) {
      bytes += key.length * 2 + 8;
      pending.push(child);
      entries += 1;
      if (entries > 100_000) return limit + 1;
      if (bytes > limit) return bytes;
    }
  }
  return bytes;
}

export function createHydrateEventBuffer(agentId, generation, limit = DEFAULT_HYDRATE_EVENT_BUFFER_LIMIT, limitBytes = DEFAULT_HYDRATE_EVENT_BUFFER_BYTES) {
  return {
    agentId: String(agentId || ""),
    generation: Number(generation) || 0,
    limit: Math.max(1, Number(limit) || DEFAULT_HYDRATE_EVENT_BUFFER_LIMIT),
    limitBytes: Math.max(1024, Number(limitBytes) || DEFAULT_HYDRATE_EVENT_BUFFER_BYTES),
    events: [],
    bufferedBytes: 0,
    overflow: false,
  };
}

export function enqueueHydrateEvent(buffer, event) {
  if (!buffer || buffer.overflow || !event || String(event.agent_id || "") !== buffer.agentId) return false;
  const eventBytes = approximateEventBytes(event, buffer.limitBytes);
  if (buffer.events.length >= buffer.limit || eventBytes > buffer.limitBytes || buffer.bufferedBytes + eventBytes > buffer.limitBytes) {
    buffer.overflow = true;
    return false;
  }
  buffer.events.push(event);
  buffer.bufferedBytes += eventBytes;
  return true;
}

/**
 * Drain events captured while a hydrate snapshot was in flight. Replay preserves
 * delivery order; replayable events represented by the snapshot are discarded.
 */
export function drainHydrateEvents(buffer, {
  agentId,
  generation,
  streamEpoch = "",
  coveredAgentSeq = -1,
  coveredStreamSeq = -1,
  coverageValid = false,
} = {}) {
  if (!buffer || buffer.agentId !== String(agentId || "") || buffer.generation !== Number(generation)) {
    return { events: [], overflow: false, epochMismatch: false };
  }

  const pending = buffer.events;
  buffer.events = [];
  buffer.bufferedBytes = 0;
  const expectedEpoch = String(streamEpoch || "");
  const covered = Number(coveredAgentSeq);
  const coveredStream = Number(coveredStreamSeq);
  const hasCoverage = coverageValid && Number.isSafeInteger(covered) && covered >= 0;
  const hasStreamCoverage = coverageValid && Number.isSafeInteger(coveredStream) && coveredStream >= 0;
  const events = [];
  let epochMismatch = false;

  for (const event of pending) {
    const eventEpoch = String(event?.stream_epoch || "");
    if (expectedEpoch && eventEpoch && eventEpoch !== expectedEpoch) {
      epochMismatch = true;
      continue;
    }
    const sequence = Number(event?.agent_seq);
    if (hasCoverage && Number.isSafeInteger(sequence) && sequence > 0 && sequence <= covered) continue;
    if (hasStreamCoverage && event?.event_type === "conversation_delta") {
      const streamSequence = Number(event?.seq);
      if (Number.isSafeInteger(streamSequence) && streamSequence > 0 && streamSequence <= coveredStream) continue;
    }
    events.push(event);
  }

  return { events, overflow: buffer.overflow, epochMismatch };
}
