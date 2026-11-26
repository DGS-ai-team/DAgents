import { describe, expect, it } from "vitest";
import { createConversationEventSequencer } from "./conversationEventSequencer.js";

function event(agentSeq, streamSeq = agentSeq, overrides = {}) {
  return {
    agent_id: "agent-1",
    agent_seq: agentSeq,
    seq: streamSeq,
    stream_epoch: "epoch-1",
    event_type: "turn_state",
    ...overrides,
  };
}

describe("conversation event sequencer", () => {
  it("holds a future event until the missing sequence is applied", () => {
    const sequencer = createConversationEventSequencer();
    expect(sequencer.offer(event(12), { agentId: "agent-1", generation: 2, appliedSequence: 10 }).action).toBe("buffered");
    expect(sequencer.takeNext({ agentId: "agent-1", generation: 2, appliedSequence: 10 })).toBeNull();

    expect(sequencer.offer(event(11), { agentId: "agent-1", generation: 2, appliedSequence: 10 }).action).toBe("apply");
    expect(sequencer.takeNext({ agentId: "agent-1", generation: 2, appliedSequence: 11 }).event.agent_seq).toBe(12);
    expect(sequencer.hasPending("agent-1", 2)).toBe(false);
  });

  it("drops delayed duplicates and sorts held events by Node delivery sequence", () => {
    const sequencer = createConversationEventSequencer();
    expect(sequencer.offer(event(7, 70), { agentId: "agent-1", generation: 2, appliedSequence: 3 }).action).toBe("buffered");
    expect(sequencer.offer(event(5, 50), { agentId: "agent-1", generation: 2, appliedSequence: 3 }).action).toBe("buffered");
    expect(sequencer.offer(event(5, 51), { agentId: "agent-1", generation: 2, appliedSequence: 3 }).action).toBe("buffered");

    expect(sequencer.takeNext({ agentId: "agent-1", generation: 2, appliedSequence: 3 })).toBeNull();
    expect(sequencer.takeNext({ agentId: "agent-1", generation: 3, appliedSequence: 3 })).toBeNull();
    expect(sequencer.takeAll({ agentId: "agent-1", generation: 2 }).events.map(item => item.seq)).toEqual([50, 70]);
  });

  it("keeps unsequenced events behind the gap and releases them in stream order", () => {
    const sequencer = createConversationEventSequencer();
    sequencer.offer(event(5, 50), { agentId: "agent-1", generation: 2, appliedSequence: 3 });
    expect(sequencer.offer(event(0, 51, { event_type: "tool_call", data: { partial: true } }), {
      agentId: "agent-1", generation: 2, appliedSequence: 3,
    }).action).toBe("buffered");

    const next = sequencer.takeNext({ agentId: "agent-1", generation: 2, appliedSequence: 4 });
    expect(next.event.agent_seq).toBe(5);
    expect(sequencer.takeNext({ agentId: "agent-1", generation: 2, appliedSequence: 5 }).event.event_type).toBe("tool_call");
  });

  it("bounds pending events and reports overflow for snapshot recovery", () => {
    const sequencer = createConversationEventSequencer({ limit: 2 });
    for (const sequence of [5, 6, 7]) {
      sequencer.offer(event(sequence), { agentId: "agent-1", generation: 2, appliedSequence: 3 });
    }

    expect(sequencer.hasPending("agent-1", 2)).toBe(true);
    expect(sequencer.takeAll({ agentId: "agent-1", generation: 2 })).toMatchObject({ events: expect.any(Array), overflow: true });
    expect(sequencer.hasPending("agent-1", 2)).toBe(false);
  });
});
