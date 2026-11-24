import { agentEventSequenceAction } from "./conversationWatermarks.js";
import { createHydrateEventBuffer, enqueueHydrateEvent } from "./hydrateEventBuffer.js";

/** Own the active Agent's out-of-order events until a cursor or snapshot closes the gap. */
export function createConversationEventSequencer({ limit, limitBytes } = {}) {
  let deferred = null;

  function ensureBuffer(agentId, generation) {
    const id = String(agentId || "");
    const version = Number(generation) || 0;
    if (!deferred || deferred.agentId !== id || deferred.generation !== version) {
      deferred = createHydrateEventBuffer(id, version, limit, limitBytes);
    }
    return deferred;
  }

  return {
    reset() {
      deferred = null;
    },

    offer(event, { agentId, generation, appliedSequence = 0 } = {}) {
      const action = agentEventSequenceAction(appliedSequence, event?.agent_seq);
      if (action.action === "duplicate") return { action: "duplicate", overflow: false };

      const buffer = deferred;
      const hasDeferred = buffer
        && buffer.agentId === String(agentId || "")
        && buffer.generation === (Number(generation) || 0)
        && (buffer.events.length > 0 || buffer.overflow);
      const shouldDefer = action.action === "buffer"
        || (action.action === "ephemeral" && hasDeferred)
        || (buffer?.overflow && action.action !== "apply");
      if (!shouldDefer) return { action: "apply", overflow: false };

      const target = ensureBuffer(agentId, generation);
      const sequence = Number(event?.agent_seq);
      if (Number.isSafeInteger(sequence) && sequence > 0
        && target.events.some(item => Number(item?.agent_seq) === sequence)) {
        return { action: "buffered", overflow: target.overflow };
      }
      if (!enqueueHydrateEvent(target, event)) return { action: "buffered", overflow: true };
      target.events.sort((left, right) => (Number(left?.seq) || 0) - (Number(right?.seq) || 0));
      return { action: "buffered", overflow: target.overflow };
    },

    takeNext({ agentId, generation, appliedSequence = 0 } = {}) {
      const target = deferred;
      if (!target
        || target.agentId !== String(agentId || "")
        || target.generation !== (Number(generation) || 0)) return null;
      while (target.events.length) {
        const event = target.events[0];
        const action = agentEventSequenceAction(appliedSequence, event?.agent_seq);
        if (action.action === "buffer") return null;
        target.events.shift();
        if (action.action === "duplicate") continue;
        const result = { event, overflow: target.overflow };
        if (!target.events.length && !target.overflow) deferred = null;
        return result;
      }
      if (!target.overflow) deferred = null;
      return null;
    },

    takeAll({ agentId, generation } = {}) {
      const target = deferred;
      if (!target
        || target.agentId !== String(agentId || "")
        || target.generation !== (Number(generation) || 0)) {
        return { events: [], overflow: false };
      }
      const result = { events: target.events, overflow: target.overflow };
      deferred = null;
      return result;
    },

    hasPending(agentId, generation) {
      return Boolean(deferred
        && deferred.agentId === String(agentId || "")
        && deferred.generation === (Number(generation) || 0)
        && (deferred.events.length > 0 || deferred.overflow));
    },
  };
}
