import { describe, expect, it } from "vitest";
import {
  createHydrateEventBuffer,
  drainHydrateEvents,
  enqueueHydrateEvent,
} from "./hydrateEventBuffer.js";

function event(agentSeq, overrides = {}) {
  return { agent_id: "agent-1", stream_epoch: "epoch-1", agent_seq: agentSeq, ...overrides };
}

describe("hydrate event buffer", () => {
  it("replays only events beyond the snapshot cursor and preserves ephemeral order", () => {
    const buffer = createHydrateEventBuffer("agent-1", 4);
    enqueueHydrateEvent(buffer, event(8));
    enqueueHydrateEvent(buffer, event(0, { event_type: "tool_call", data: { partial: true } }));
    enqueueHydrateEvent(buffer, event(9));

    const result = drainHydrateEvents(buffer, {
      agentId: "agent-1",
      generation: 4,
      streamEpoch: "epoch-1",
      coveredAgentSeq: 8,
      coverageValid: true,
    });

    expect(result.events.map((item) => item.agent_seq)).toEqual([0, 9]);
    expect(result.epochMismatch).toBe(false);
  });

  it("drops conversation deltas already covered by the same global snapshot cut", () => {
    const buffer = createHydrateEventBuffer("agent-1", 4);
    enqueueHydrateEvent(buffer, event(0, { seq: 20, event_type: "conversation_delta", data: { chunk_seq: 3 } }));
    enqueueHydrateEvent(buffer, event(0, { seq: 21, event_type: "conversation_delta", data: { chunk_seq: 4 } }));
    enqueueHydrateEvent(buffer, event(8, { seq: 22, event_type: "tool_result" }));

    const result = drainHydrateEvents(buffer, {
      agentId: "agent-1",
      generation: 4,
      streamEpoch: "epoch-1",
      coveredAgentSeq: 7,
      coveredStreamSeq: 20,
      coverageValid: true,
    });

    expect(result.events.map(item => [item.event_type, item.seq])).toEqual([
      ["conversation_delta", 21],
      ["tool_result", 22],
    ]);
  });

  it("does not discard buffered events when snapshot coverage is unavailable", () => {
    const buffer = createHydrateEventBuffer("agent-1", 4);
    enqueueHydrateEvent(buffer, event(8));
    enqueueHydrateEvent(buffer, event(9));

    const result = drainHydrateEvents(buffer, { agentId: "agent-1", generation: 4 });

    expect(result.events.map((item) => item.agent_seq)).toEqual([8, 9]);
  });

  it("rejects events from another stream epoch and reports that a fresh hydrate is needed", () => {
    const buffer = createHydrateEventBuffer("agent-1", 4);
    enqueueHydrateEvent(buffer, event(8, { stream_epoch: "old-epoch" }));
    enqueueHydrateEvent(buffer, event(9));

    const result = drainHydrateEvents(buffer, {
      agentId: "agent-1",
      generation: 4,
      streamEpoch: "epoch-1",
      coveredAgentSeq: 7,
      coverageValid: true,
    });

    expect(result.events.map((item) => item.agent_seq)).toEqual([9]);
    expect(result.epochMismatch).toBe(true);
  });

  it("marks overflow at the configured bound and stops growing", () => {
    const buffer = createHydrateEventBuffer("agent-1", 4, 2);
    expect(enqueueHydrateEvent(buffer, event(1))).toBe(true);
    expect(enqueueHydrateEvent(buffer, event(2))).toBe(true);
    expect(enqueueHydrateEvent(buffer, event(3))).toBe(false);
    expect(buffer.events).toHaveLength(2);

    const result = drainHydrateEvents(buffer, { agentId: "agent-1", generation: 4 });
    expect(result.overflow).toBe(true);
  });

  it("bounds retained payload size as well as event count", () => {
    const buffer = createHydrateEventBuffer("agent-1", 4, 20, 1024);
    expect(enqueueHydrateEvent(buffer, event(1, { data: { content: "x".repeat(800) } }))).toBe(false);
    expect(buffer.events).toHaveLength(0);
    expect(buffer.overflow).toBe(true);
  });

  it("does not drain a buffer owned by another agent generation", () => {
    const buffer = createHydrateEventBuffer("agent-1", 4);
    enqueueHydrateEvent(buffer, event(1));

    expect(drainHydrateEvents(buffer, { agentId: "agent-1", generation: 5 }).events).toEqual([]);
    expect(buffer.events).toHaveLength(1);
  });
});
