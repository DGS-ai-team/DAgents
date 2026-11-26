import { hydrateSchedulePlan, nextHydrateRetryDelay, shouldReplaceHydrateTimer } from "./hydrateSnapshot.js";
import { createHydrateEventBuffer, drainHydrateEvents, enqueueHydrateEvent } from "./hydrateEventBuffer.js";

const EMPTY_TARGET = () => ({
  historyRevision: 0,
  conversationRevision: 0,
  lifecycleSeq: 0,
  agentSeq: 0,
  resultCommitStatus: "",
});

function targetFrom(value) {
  const payload = value?.data || value || {};
  return {
    historyRevision: Number(payload.history_revision) || 0,
    conversationRevision: Number(payload.conversation_revision) || 0,
    lifecycleSeq: Number(payload.lifecycle_seq) || 0,
    agentSeq: Number(value?.agent_seq ?? payload.agent_seq) || 0,
    resultCommitStatus: String(payload.result_commit_status || ""),
  };
}

function mergeTarget(current, incoming) {
  const changed = incoming.historyRevision > current.historyRevision
    || incoming.conversationRevision > current.conversationRevision
    || incoming.lifecycleSeq > current.lifecycleSeq
    || incoming.agentSeq > current.agentSeq
    || Boolean(incoming.resultCommitStatus && incoming.resultCommitStatus !== current.resultCommitStatus);
  return {
    changed,
    target: {
      historyRevision: Math.max(current.historyRevision, incoming.historyRevision),
      conversationRevision: Math.max(current.conversationRevision, incoming.conversationRevision),
      lifecycleSeq: Math.max(current.lifecycleSeq, incoming.lifecycleSeq),
      agentSeq: Math.max(current.agentSeq, incoming.agentSeq),
      resultCommitStatus: incoming.resultCommitStatus || current.resultCommitStatus,
    },
  };
}

/** Coalesce hydrate requests for one active conversation and own its debounce timer. */
export function createConversationController({
  now = Date.now,
  setTimer = (callback, delay) => globalThis.setTimeout(callback, delay),
  clearTimer = timer => globalThis.clearTimeout(timer),
  isCurrent = () => true,
  stageDeferredEvents = () => {},
  applyEvent = () => {},
  run,
} = {}) {
  let agentId = "";
  let generation = 0;
  let timer = null;
  let dueAt = 0;
  let retryTimer = null;
  let flight = null;
  let dirty = false;
  let dirtyAt = 0;
  let target = EMPTY_TARGET();
  let retryCount = 0;
  let retryExhausted = false;
  let eventBuffer = null;
  let unlisten = null;
  let subscriptionVersion = 0;

  function owns(id, ownerGeneration = generation) {
    return String(id || "") === agentId
      && (Number(ownerGeneration) || 0) === generation
      && Boolean(agentId)
      && isCurrent(agentId, generation);
  }

  function cancelTimer() {
    if (timer !== null) clearTimer(timer);
    timer = null;
    dueAt = 0;
  }

  function cancelRetryTimer() {
    if (retryTimer !== null) clearTimer(retryTimer);
    retryTimer = null;
  }

  function resetRetryBudget() {
    cancelRetryTimer();
    retryCount = 0;
    retryExhausted = false;
  }

  function reset(nextAgentId = "", nextGeneration = 0) {
    cancelTimer();
    resetRetryBudget();
    flight = null;
    agentId = String(nextAgentId || "");
    generation = Number(nextGeneration) || 0;
    dirty = false;
    dirtyAt = 0;
    target = EMPTY_TARGET();
    eventBuffer = null;
  }

  function markDirty(id, ownerGeneration = generation) {
    if (!owns(id, ownerGeneration)) return false;
    dirty = true;
    dirtyAt ||= now();
    return true;
  }

  function request(id, {
    generation: ownerGeneration = generation,
    delay = 100,
    requestedTarget = null,
    force = false,
  } = {}) {
    if (!owns(id, ownerGeneration)) return { scheduled: false, reason: "stale_owner" };
    dirty = true;
    dirtyAt ||= now();
    const merged = mergeTarget(target, targetFrom(requestedTarget));
    target = merged.target;
    if (merged.changed || force) resetRetryBudget();
    if (flight?.agentId === agentId && flight.generation === generation) {
      return { scheduled: false, reason: "in_flight", targetChanged: merged.changed };
    }

    const plan = hydrateSchedulePlan({
      requestedDelay: delay,
      dirtyAt,
      now: now(),
      retryExhausted,
      targetChanged: merged.changed,
      force,
    });
    if (!plan.allowed) return { scheduled: false, reason: "retry_exhausted", targetChanged: merged.changed };

    const nextDueAt = now() + plan.delay;
    if (!shouldReplaceHydrateTimer(dueAt, nextDueAt)) {
      return { scheduled: timer !== null, reason: "existing_timer_is_earlier", targetChanged: merged.changed };
    }
    cancelTimer();
    dueAt = nextDueAt;
    timer = setTimer(() => {
      timer = null;
      dueAt = 0;
      if (owns(id, ownerGeneration)) void run?.(id, ownerGeneration);
    }, plan.delay);
    return { scheduled: true, reason: "scheduled", targetChanged: merged.changed, resetRetryBudget: plan.resetRetryBudget };
  }

  function consumeDirty(id, ownerGeneration = generation) {
    if (!owns(id, ownerGeneration) || !dirty) return false;
    dirty = false;
    return true;
  }

  function markClean(id, ownerGeneration = generation) {
    if (!owns(id, ownerGeneration)) return false;
    dirty = false;
    dirtyAt = 0;
    resetRetryBudget();
    return true;
  }

  function runSingleFlight(id, ownerGeneration, task) {
    if (!owns(id, ownerGeneration)) return Promise.resolve(undefined);
    if (flight?.agentId === agentId && flight.generation === generation) {
      markDirty(id, ownerGeneration);
      return flight.promise;
    }
    cancelTimer();
    const token = { agentId, generation, promise: null };
    token.promise = Promise.resolve()
      .then(() => owns(id, ownerGeneration) ? task() : undefined)
      .finally(() => {
        if (flight !== token) return;
        flight = null;
        if (owns(id, ownerGeneration) && dirty && timer === null && retryTimer === null && !retryExhausted) {
          request(id, { generation: ownerGeneration, delay: 0 });
        }
      });
    flight = token;
    return token.promise;
  }

  function scheduleRetry(id, ownerGeneration = generation, onExhausted = () => {}) {
    if (!owns(id, ownerGeneration)) return null;
    cancelRetryTimer();
    const delay = nextHydrateRetryDelay(retryCount);
    if (delay === null) {
      retryExhausted = true;
      onExhausted();
      return null;
    }
    retryCount += 1;
    retryTimer = setTimer(() => {
      retryTimer = null;
      if (owns(id, ownerGeneration)) void run?.(id, ownerGeneration);
    }, delay);
    return delay;
  }

  function retryNow(id, ownerGeneration = generation) {
    if (!owns(id, ownerGeneration)) return false;
    resetRetryBudget();
    cancelTimer();
    dirty = true;
    dirtyAt ||= now();
    if (flight?.agentId === agentId && flight.generation === generation) return true;
    void run?.(id, ownerGeneration);
    return true;
  }

  function beginEventBuffer(id, ownerGeneration = generation) {
    if (!owns(id, ownerGeneration)) return null;
    eventBuffer = createHydrateEventBuffer(agentId, generation);
    stageDeferredEvents(eventBuffer);
    return eventBuffer;
  }

  function bufferEvent(event) {
    const buffer = eventBuffer;
    if (!buffer
      || buffer.agentId !== agentId
      || buffer.generation !== generation
      || String(event?.agent_id || "") !== agentId
      || ["notification_changed", "resync_required"].includes(event?.event_type)) return false;
    enqueueHydrateEvent(buffer, event);
    return true;
  }

  function drainEventBuffer(buffer, {
    agentId: targetAgentId,
    generation: targetGeneration,
    streamEpoch = "",
    coveredAgentSeq,
    coveredStreamSeq,
    coverageValid = false,
  } = {}) {
    const result = drainHydrateEvents(buffer, {
      agentId: targetAgentId,
      generation: targetGeneration,
      streamEpoch,
      coveredAgentSeq,
      coveredStreamSeq,
      coverageValid,
    });
    for (const event of result.events) applyEvent(event);
    if (result.epochMismatch) {
      buffer.epochMismatch = true;
      markDirty(targetAgentId, targetGeneration);
    }
    return result;
  }

  function releaseEventBuffer(buffer = null) {
    if (buffer && eventBuffer !== buffer) return false;
    eventBuffer = null;
    return true;
  }

  function releaseSubscription() {
    subscriptionVersion += 1;
    const detach = unlisten;
    unlisten = null;
    detach?.();
  }

  async function subscribe(subscribeFn, onEvent) {
    releaseSubscription();
    const version = subscriptionVersion;
    const detach = await subscribeFn(event => {
      if (version === subscriptionVersion) onEvent(event);
    });
    if (version !== subscriptionVersion) {
      detach?.();
      return false;
    }
    unlisten = detach;
    return true;
  }

  function dispose() {
    releaseSubscription();
    reset();
  }

  function resetAgentSequenceTarget(id, ownerGeneration = generation) {
    if (!owns(id, ownerGeneration)) return false;
    target = { ...target, agentSeq: 0 };
    return true;
  }

  function coverWithPatch(id, ownerGeneration, patch, eventAgentSeq = 0) {
    if (!owns(id, ownerGeneration) || (flight?.agentId === agentId && flight.generation === generation)) return false;
    const payload = patch?.data || patch || {};
    const covered = {
      historyRevision: Number(payload.history_revision) || 0,
      conversationRevision: Number(payload.conversation_revision) || 0,
      lifecycleSeq: Number(payload.turn_state?.lifecycle_seq) || 0,
      agentSeq: Number(eventAgentSeq) || 0,
      resultCommitStatus: String(payload.result_commit_status || ""),
    };
    if (target.historyRevision > covered.historyRevision
      || target.conversationRevision > covered.conversationRevision
      || target.lifecycleSeq > covered.lifecycleSeq
      || target.agentSeq > covered.agentSeq
      || (target.resultCommitStatus && target.resultCommitStatus !== covered.resultCommitStatus)) return false;
    cancelTimer();
    dirty = false;
    dirtyAt = 0;
    target = EMPTY_TARGET();
    resetRetryBudget();
    return true;
  }

  return {
    reset,
    request,
    markDirty,
    consumeDirty,
    markClean,
    resetAgentSequenceTarget,
    coverWithPatch,
    scheduleRetry,
    retryNow,
    runSingleFlight,
    beginEventBuffer,
    stageDeferredEvents,
    bufferEvent,
    drainEventBuffer,
    releaseEventBuffer,
    subscribe,
    releaseSubscription,
    isRetryExhausted: (id, ownerGeneration = generation) => owns(id, ownerGeneration) && retryExhausted,
    hasRetry: (id, ownerGeneration = generation) => owns(id, ownerGeneration) && retryTimer !== null,
    hasFlight: (id, ownerGeneration = generation) => owns(id, ownerGeneration) && flight?.agentId === String(id || "") && flight.generation === (Number(ownerGeneration) || 0),
    cancelTimer,
    isDirty: (id, ownerGeneration = generation) => owns(id, ownerGeneration) && dirty,
    getTarget: (id, ownerGeneration = generation) => owns(id, ownerGeneration) ? { ...target } : EMPTY_TARGET(),
    getOwner: () => ({ agentId, generation }),
    dispose,
  };
}
