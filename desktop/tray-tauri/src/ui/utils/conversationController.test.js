import { describe, expect, it, vi } from "vitest";
import { createConversationController } from "./conversationController.js";

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

function createClock() {
  let now = 1000;
  let nextId = 0;
  const timers = new Map();
  return {
    now: () => now,
    setTimer(callback, delay) {
      const id = ++nextId;
      timers.set(id, { callback, dueAt: now + delay });
      return id;
    },
    clearTimer(id) { timers.delete(id); },
    advance(ms) {
      now += ms;
      const due = [...timers.entries()]
        .filter(([, timer]) => timer.dueAt <= now)
        .sort((left, right) => left[1].dueAt - right[1].dueAt);
      for (const [id, timer] of due) {
        if (!timers.delete(id)) continue;
        timer.callback();
      }
    },
    timerCount: () => timers.size,
  };
}

describe("conversation sync scheduler", () => {
  it("coalesces target watermarks and preserves the max-wait deadline", () => {
    const clock = createClock();
    const run = vi.fn();
    const scheduler = createConversationController({ ...clock, run });
    scheduler.reset("agent-1", 4);

    scheduler.request("agent-1", {
      generation: 4,
      delay: 250,
      requestedTarget: { data: { conversation_revision: 2, lifecycle_seq: 7 } },
    });
    clock.advance(100);
    scheduler.request("agent-1", {
      generation: 4,
      delay: 250,
      requestedTarget: { conversation_revision: 3, agent_seq: 12 },
    });

    expect(scheduler.getTarget("agent-1", 4)).toEqual({
      historyRevision: 0,
      conversationRevision: 3,
      lifecycleSeq: 7,
      agentSeq: 12,
      resultCommitStatus: "",
    });
    expect(clock.timerCount()).toBe(1);
    clock.advance(149);
    expect(run).not.toHaveBeenCalled();
    clock.advance(1);
    expect(run).toHaveBeenCalledTimes(1);
    expect(run).toHaveBeenLastCalledWith("agent-1", 4);
  });

  it("pulls a delayed request forward for a terminal event", () => {
    const clock = createClock();
    const run = vi.fn();
    const scheduler = createConversationController({ ...clock, run });
    scheduler.reset("agent-1", 1);
    scheduler.request("agent-1", { generation: 1, delay: 250 });
    clock.advance(100);
    scheduler.request("agent-1", { generation: 1, delay: 0, force: true });

    clock.advance(0);
    expect(run).toHaveBeenCalledTimes(1);
    expect(run).toHaveBeenLastCalledWith("agent-1", 1);
  });

  it("cancels a queued hydrate after a reliable patch covers its requested revisions", () => {
    const clock = createClock();
    const run = vi.fn();
    const scheduler = createConversationController({ ...clock, run });
    scheduler.reset("agent-1", 2);
    scheduler.request("agent-1", {
      generation: 2,
      delay: 120,
      requestedTarget: {
        agent_id: "agent-1",
        agent_seq: 8,
        event_type: "tool_result",
        data: { conversation_revision: 4, history_revision: 7, lifecycle_seq: 6 },
      },
    });

    expect(scheduler.coverWithPatch("agent-1", 2, {
      conversation_revision: 5,
      history_revision: 8,
      turn_state: { lifecycle_seq: 7 },
      result_commit_status: "committed",
    }, 9)).toBe(true);
    expect(scheduler.isDirty("agent-1", 2)).toBe(false);
    expect(clock.timerCount()).toBe(0);
    clock.advance(500);
    expect(run).not.toHaveBeenCalled();
  });

  it("keeps a conversation dirty during the single flight and schedules one follow-up after it settles", async () => {
    const clock = createClock();
    const run = vi.fn();
    const scheduler = createConversationController({ ...clock, run });
    scheduler.reset("agent-1", 2);
    let finish;
    const blocked = new Promise(resolve => { finish = resolve; });
    const flight = scheduler.runSingleFlight("agent-1", 2, async () => {
      scheduler.markDirty("agent-1", 2);
      scheduler.consumeDirty("agent-1", 2);
      await blocked;
    });
    await Promise.resolve();

    const result = scheduler.request("agent-1", { generation: 2, delay: 0 });
    expect(result.reason).toBe("in_flight");
    expect(clock.timerCount()).toBe(0);
    expect(scheduler.isDirty("agent-1", 2)).toBe(true);

    finish();
    await flight;
    expect(clock.timerCount()).toBe(1);
    clock.advance(0);
    expect(run).toHaveBeenCalledTimes(1);
  });

  it("resets ownership and ignores stale timers after switching conversations", () => {
    const clock = createClock();
    const run = vi.fn();
    const scheduler = createConversationController({ ...clock, run });
    scheduler.reset("agent-1", 8);
    scheduler.request("agent-1", { generation: 8, delay: 100 });
    scheduler.reset("agent-2", 9);
    clock.advance(1000);

    expect(run).not.toHaveBeenCalled();
    expect(scheduler.getTarget("agent-2", 9).conversationRevision).toBe(0);
    expect(scheduler.isDirty("agent-1", 8)).toBe(false);
  });

  it("clears only the process-local sequence target when the Node stream epoch changes", () => {
    const clock = createClock();
    const scheduler = createConversationController({ ...clock, run: vi.fn() });
    scheduler.reset("agent-1", 5);
    scheduler.request("agent-1", {
      generation: 5,
      requestedTarget: { history_revision: 6, conversation_revision: 9, lifecycle_seq: 11, agent_seq: 80 },
    });

    scheduler.resetAgentSequenceTarget("agent-1", 5);

    expect(scheduler.getTarget("agent-1", 5)).toEqual({
      historyRevision: 6,
      conversationRevision: 9,
      lifecycleSeq: 11,
      agentSeq: 0,
      resultCommitStatus: "",
    });
  });

  it("owns the snapshot event buffer and applies only uncovered events", () => {
    const applied = vi.fn();
    const scheduler = createConversationController({ applyEvent: applied });
    scheduler.reset("agent-1", 4);
    const buffer = scheduler.beginEventBuffer("agent-1", 4);

    expect(scheduler.bufferEvent(event(5, 50))).toBe(true);
    expect(scheduler.bufferEvent(event(6, 51, { event_type: "notification_changed" }))).toBe(false);
    expect(scheduler.bufferEvent(event(7, 52, { agent_id: "agent-2" }))).toBe(false);
    expect(buffer.events).toHaveLength(1);

    const result = scheduler.drainEventBuffer(buffer, {
      agentId: "agent-1",
      generation: 4,
      streamEpoch: "epoch-1",
      coveredAgentSeq: 4,
      coverageValid: true,
    });
    expect(result.events).toHaveLength(1);
    expect(applied).toHaveBeenCalledTimes(1);
    expect(applied).toHaveBeenLastCalledWith(event(5, 50));
    expect(scheduler.releaseEventBuffer(buffer)).toBe(true);
  });

  it("releases its Node event subscription and ignores callbacks after disposal", async () => {
    const scheduler = createConversationController();
    const detach = vi.fn();
    const onEvent = vi.fn();
    let deliver;

    await scheduler.subscribe(handler => {
      deliver = handler;
      return Promise.resolve(detach);
    }, onEvent);
    deliver({ event_type: "turn_state" });
    scheduler.dispose();
    deliver({ event_type: "turn_finished" });

    expect(onEvent).toHaveBeenCalledTimes(1);
    expect(detach).toHaveBeenCalledTimes(1);
  });

  it("detaches a late subscription result when the controller is disposed during setup", async () => {
    const scheduler = createConversationController();
    const detach = vi.fn();
    let resolveSubscription;
    const subscription = scheduler.subscribe(() => new Promise(resolve => { resolveSubscription = resolve; }), vi.fn());

    scheduler.dispose();
    resolveSubscription(detach);

    expect(await subscription).toBe(false);
    expect(detach).toHaveBeenCalledTimes(1);
  });

  it("owns bounded exponential retries and allows a changed target to reopen scheduling", () => {
    const clock = createClock();
    const run = vi.fn();
    const scheduler = createConversationController({ ...clock, run });
    scheduler.reset("agent-1", 3);

    for (const delay of [500, 1000, 2000, 4000, 8000]) {
      expect(scheduler.scheduleRetry("agent-1", 3)).toBe(delay);
      clock.advance(delay);
    }
    expect(run).toHaveBeenCalledTimes(5);
    expect(scheduler.scheduleRetry("agent-1", 3)).toBeNull();
    expect(scheduler.isRetryExhausted("agent-1", 3)).toBe(true);
    expect(scheduler.request("agent-1", { generation: 3 }).reason).toBe("retry_exhausted");

    const reopened = scheduler.request("agent-1", {
      generation: 3,
      requestedTarget: { conversation_revision: 1 },
    });
    expect(reopened).toMatchObject({ scheduled: true, targetChanged: true, resetRetryBudget: true });
    expect(scheduler.isRetryExhausted("agent-1", 3)).toBe(false);
    expect(clock.timerCount()).toBe(1);
  });

  it("cancels a pending retry and runs immediately after a manual retry", () => {
    const clock = createClock();
    const run = vi.fn();
    const scheduler = createConversationController({ ...clock, run });
    scheduler.reset("agent-1", 6);
    scheduler.scheduleRetry("agent-1", 6);

    expect(scheduler.retryNow("agent-1", 6)).toBe(true);
    expect(clock.timerCount()).toBe(0);
    expect(run).toHaveBeenCalledTimes(1);
    expect(scheduler.isRetryExhausted("agent-1", 6)).toBe(false);
  });
});
