// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { flushPromises, shallowMount } from "@vue/test-utils";
import { nextTick, reactive } from "vue";
import MainChatPanel from "./MainChatPanel.vue";
import MessageBubble from "./MessageBubble.vue";
import ApprovalBubble from "./ApprovalBubble.vue";
import UserInfoBubble from "./UserInfoBubble.vue";
import ToolGroupRow from "./ToolGroupRow.vue";
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
    let step = wrapper.findComponent(ToolGroupRow).props("steps")[0];
    expect(step.executionHint).toBe("pending");

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

    step = wrapper.findComponent(ToolGroupRow).props("steps")[0];
    expect(step.executionHint).toBe("rejected");
  });
});

describe("MainChatPanel incremental history rendering", () => {
  it("keeps a failed-turn message visible alongside already generated partial output", async () => {
    wrapper = shallowMount(MainChatPanel, {
      props: {
        agentId: "agent-a",
        entries: [{ id: "user-1", kind: "user", text: "question" }],
        streamingText: "partial answer",
        turnError: "模型服务暂时不可用，已重试2次。已保留本轮已生成的部分内容。",
      },
    });
    await flushFrames();
    expect(wrapper.find('[role="alert"]').text()).toContain("已重试2次");
    expect(wrapper.findAllComponents(MessageBubble).at(-1).props("entry").text).toBe("partial answer");
  });

  it("labels model metadata as the current selection while the prior turn error remains visible", async () => {
    wrapper = shallowMount(MainChatPanel, {
      props: {
        turnError: "模型账户额度不足。",
        modelRecoveryAvailable: true,
        modelName: "qwen3.7-plus",
        modelProfile: "qwen-free",
      },
    });

    expect(wrapper.get('[role="alert"]').text()).toBe("模型账户额度不足。");
    expect(wrapper.get(".chat__turn-error-model").text()).toBe("当前选择 · 模型：qwen3.7-plus · 配置：qwen-free");
    const switchModel = wrapper.get('[aria-label="切换模型配置"]');
    expect(switchModel.attributes('title')).toBe('切换模型配置');
    const openSettings = wrapper.get('[aria-label="打开模型设置"]');
    await switchModel.trigger("click");
    await openSettings.trigger("click");
    expect(wrapper.emitted("open-model-picker")).toEqual([[]]);
    expect(wrapper.emitted("open-model-settings")).toEqual([[]]);

    await wrapper.setProps({ modelName: "deepseek-flash", modelProfile: "deepseek-v4-flash" });
    expect(wrapper.get('[role="alert"]').text()).toBe("模型账户额度不足。");
    expect(wrapper.get(".chat__turn-error-model").text()).toBe("当前选择 · 模型：deepseek-flash · 配置：deepseek-v4-flash");
  });

  it("never offers to replay a failed user turn after a completed file write", async () => {
    wrapper = shallowMount(MainChatPanel, {
      props: {
        entries: [
          { id: "user-1", kind: "user", text: "update the report" },
          { id: "write-call", kind: "tool_call", blockId: "write-block", data: { tool_call_id: "write-1", tool_name: "write_file", arguments: { path: "report.md" } } },
          { id: "write-result", kind: "tool_result", blockId: "write-block", data: { tool_call_id: "write-1", tool_name: "write_file", status: "succeeded", content: "wrote report.md" } },
        ],
        turnError: "模型服务暂时不可用。",
        modelRecoveryAvailable: true,
      },
    });
    await flushFrames();

    expect(wrapper.findComponent(ToolGroupRow).exists()).toBe(true);
    expect(wrapper.find('[aria-label="重试本轮"]').exists()).toBe(false);
    expect(wrapper.find('[aria-label="切换模型配置"]').exists()).toBe(true);
  });

  it("retains an approval card after a long transcript reaches the tail", async () => {
    const entries = Array.from({ length: 220 }, (_, index) => ({
      id: `assistant-${index}`,
      kind: "assistant",
      text: `historical message ${index}`,
    }));
    wrapper = shallowMount(MainChatPanel, {
      props: {
        entries,
        hitlQueue: [{
          kind: "approval",
          data: {
            approval_id: "approval-at-tail",
            approval_args: { tool_calls: [{ id: "write-review", name: "write_file", arguments: { path: "review.md" } }] },
          },
        }],
      },
    });
    await flushFrames();

    expect(wrapper.findComponent(ApprovalBubble).exists()).toBe(true);
    expect(wrapper.findComponent(ApprovalBubble).props("requestKey")).toBe("approval::approval-at-tail");
  });

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

    expect(wrapper.findComponent(ToolGroupRow).props("steps")[0].callEntry.data.arguments).toBe("two");
  });

  it("forwards the approval selection with its queue index and uses reject-all for the batch action", async () => {
    wrapper = shallowMount(MainChatPanel, {
      props: {
        hitlQueue: [{
          kind: "approval",
          data: {
            approval_id: "approval-main",
            approval_args: { tool_calls: [{ id: "call-a", name: "read_file", arguments: { path: "a.txt" } }] },
          },
        }],
      },
    });
    await flushFrames();

    expect(wrapper.findComponent(ApprovalBubble).props("requestKey")).toBe("approval::approval-main");
    wrapper.findComponent(ApprovalBubble).vm.$emit("approve-selection", ["call-a"]);
    wrapper.findComponent(ApprovalBubble).vm.$emit("reject-all");
    expect(wrapper.emitted("approve-selection")).toEqual([[{ index: 0, approvedCallIds: ["call-a"] }]]);
    expect(wrapper.emitted("reject-all")).toEqual([[0]]);
    expect(wrapper.emitted("approve-one")).toBeUndefined();
    expect(wrapper.emitted("approve-all")).toBeUndefined();
  });
});

describe("MainChatPanel user input requests", () => {
  it("forwards the user-answer action to the Desktop composer flow", async () => {
    wrapper = shallowMount(MainChatPanel, {
      props: {
        hitlQueue: [{
          kind: "user_information",
          data: { request_id: "ask-1", user_information_args: { question: "Which date range?", options: [{ id: "q1", label: "This quarter" }] } },
        }],
      },
    });
    await flushFrames();

    const request = wrapper.findComponent(UserInfoBubble);
    expect(request.exists()).toBe(true);
    request.vm.$emit("submit", "");
    expect(wrapper.emitted("user-info-submit")).toEqual([[0]]);
  });
});
