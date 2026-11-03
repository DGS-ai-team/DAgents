// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { flushPromises, shallowMount } from "@vue/test-utils";
import { nextTick, reactive } from "vue";
import MainChatPanel from "./MainChatPanel.vue";
import MessageBubble from "./MessageBubble.vue";
import ToolSummaryRow from "./ToolSummaryRow.vue";
import { applyTurnState, resetTurnState } from "../stores/turnState.js";
import * as streamBuilder from "../composables/useStream.js";

let wrapper;
let frames;

async function flushFrames() {
  while (frames.length) {
    const current = frames;
    frames = [];
    current.forEach((callback) => callback());
    await nextTick();
    await flushPromises();
  }
}

beforeEach(() => {
  resetTurnState();
  frames = [];
  vi.stubGlobal("requestAnimationFrame", (callback) => {
    frames.push(callback);
    return frames.length;
  });
  vi.stubGlobal("cancelAnimationFrame", vi.fn());
});

afterEach(() => {
  wrapper?.unmount();
  wrapper = null;
  vi.unstubAllGlobals();
});

describe("MainChatPanel tool lifecycle", () => {
  it("rebuilds a tool row when authoritative execution changes without changing running IDs", async () => {
    const call = {
      id: "tool-call-row",
      kind: "tool_call",
      blockId: "call-approval",
      data: { tool_call_id: "call-approval", tool_name: "write_file", arguments: "{}" },
    };
    applyTurnState({
      authority: "turn_coordinator",
      phase: "tool_waiting",
      turn_id: "turn-1",
      generation: 1,
      lifecycle_seq: 1,
      tool_executions: [{ tool_call_id: "call-approval", status: "proposed" }],
    });

    wrapper = shallowMount(MainChatPanel, { props: { entries: [call] } });
    await flushFrames();
    expect(wrapper.findComponent(ToolSummaryRow).props("executionHint")).toBe("pending");

    applyTurnState({
      authority: "turn_coordinator",
      phase: "tool_waiting",
      turn_id: "turn-1",
      generation: 1,
      lifecycle_seq: 2,
      tool_executions: [{ tool_call_id: "call-approval", status: "denied" }],
    });
    await nextTick();
    await flushFrames();

    expect(wrapper.findComponent(ToolSummaryRow).props("executionHint")).toBe("failed");
  });
});

describe("MainChatPanel incremental history rendering", () => {
  it("updates streaming assistant text in place without rebuilding history", async () => {
    const buildSpy = vi.spyOn(streamBuilder, "buildStream");
    wrapper = shallowMount(MainChatPanel, {
      props: {
        agentId: "agent-a",
        entries: [{ id: "user-1", kind: "user", text: "question" }],
        streamingText: "alpha",
      },
    });
    await flushFrames();
    expect(buildSpy).toHaveBeenCalledTimes(1);
    expect(wrapper.findAllComponents(MessageBubble).at(-1).props("entry").text).toBe("alpha");

    await wrapper.setProps({ streamingText: "bravo" });
    await nextTick();
    expect(wrapper.findAllComponents(MessageBubble).at(-1).props("entry").text).toBe("bravo");
    expect(buildSpy).toHaveBeenCalledTimes(1);
    buildSpy.mockRestore();
  });

  it("rebuilds when a same-length history row changes", async () => {
    wrapper = shallowMount(MainChatPanel, {
      props: { entries: [{ id: "assistant-1", kind: "assistant", text: "old" }] },
    });
    await flushFrames();
    await wrapper.setProps({ entries: [{ id: "assistant-1", kind: "assistant", text: "new" }] });
    await nextTick();
    await flushFrames();

    expect(wrapper.findComponent(MessageBubble).props("entry").text).toBe("new");
  });

  it("rebuilds for same-length tool argument changes in a reactive history row", async () => {
    const entries = reactive([{
      id: "tool-1",
      kind: "tool_call",
      blockId: "call-1",
      data: { tool_call_id: "call-1", tool_name: "read_file", arguments: "one" },
    }]);
    wrapper = shallowMount(MainChatPanel, { props: { entries } });
    await flushFrames();

    entries[0].data.arguments = "two";
    await nextTick();
    await flushFrames();

    expect(wrapper.findComponent(ToolSummaryRow).props("callEntry").data.arguments).toBe("two");
  });
});
