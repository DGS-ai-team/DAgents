/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import MessageBubble from "./MessageBubble.vue";
import * as nodeApi from "../api/node.js";

describe("MessageBubble context boundary", () => {
  it("does not render an empty streaming assistant placeholder", () => {
    const wrapper = mount(MessageBubble, {
      props: { entry: { kind: "assistant", text: "", streaming: true } },
    });
    expect(wrapper.find(".msg--assistant").exists()).toBe(false);
    wrapper.unmount();
  });

  it("renders the reset separator without rendering bridge content", () => {
    const wrapper = mount(MessageBubble, {
      props: { entry: { kind: "context_boundary", text: "private bridge body" } },
    });
    expect(wrapper.text()).toBe("新对话 · 上下文已重置");
    expect(wrapper.find('[role="separator"]').exists()).toBe(true);
    wrapper.unmount();
  });

  it("continues to show unrelated system notices", () => {
    const wrapper = mount(MessageBubble, {
      props: { entry: { kind: "system", text: "Auto Agent 即将开始" } },
    });
    expect(wrapper.text()).toBe("Auto Agent 即将开始");
    expect(wrapper.find('[role="separator"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it("shows the user's original text with distinct structured file location cards", () => {
    const wrapper = mount(MessageBubble, {
      props: {
        conversationOrdinal: 2,
        streamItemKey: "e-user-ref",
        entry: {
          kind: "user",
          text: "请比较这两处",
          file_refs: [
            { id: "a", path: "reports/plan.csv", name: "plan.csv", anchor: { label: "A1:A2" } },
            { id: "b", path: "reports/plan.csv", name: "plan.csv", anchor: { label: "C1:C2" } },
          ],
        },
      },
    });
    expect(wrapper.find(".msg__text").text()).toBe("请比较这两处");
    expect(wrapper.find(".msg--user").attributes("data-stream-item-key")).toBe("e-user-ref");
    expect(wrapper.findAll(".msg__file-ref")).toHaveLength(2);
    expect(wrapper.text()).toContain("A1:A2");
    expect(wrapper.text()).toContain("C1:C2");
    wrapper.unmount();
  });

  it("opens queue actions only for a persisted queued input", async () => {
    const wrapper = mount(MessageBubble, {
      props: { entry: { kind: "user", text: "queued", queue_status: "queued", input_id: "agent/input/4", input_seq: 4, input_revision: 2 } },
    });
    const row = wrapper.find(".msg--user");
    expect(row.attributes("tabindex")).toBe("0");
    await row.trigger("contextmenu", { clientX: 22, clientY: 34 });
    expect(wrapper.emitted("queue-context-menu")).toHaveLength(1);
    expect(wrapper.emitted("queue-context-menu")[0][0].entry.input_revision).toBe(2);
    wrapper.unmount();
  });

  it("does not expose queue actions for claimed or not-yet-acknowledged messages", async () => {
    for (const entry of [
      { kind: "user", text: "claimed", queue_status: "claimed", input_seq: 5, input_revision: 1 },
      { kind: "user", text: "sending", queue_status: "queued" },
    ]) {
      const wrapper = mount(MessageBubble, { props: { entry } });
      await wrapper.find(".msg--user").trigger("contextmenu", { clientX: 22, clientY: 34 });
      expect(wrapper.emitted("queue-context-menu")).toBeUndefined();
      expect(wrapper.find(".msg--user").attributes("tabindex")).toBeUndefined();
      wrapper.unmount();
    }
  });

  it("keeps the empty streaming assistant placeholder out of the message area", () => {
    const wrapper = mount(MessageBubble, {
      props: { entry: { kind: "assistant", text: "", streaming: true } },
    });
    expect(wrapper.find(".msg--assistant").exists()).toBe(false);
    wrapper.unmount();
  });

  it("reveals buffered assistant chunks smoothly and keeps animating after the turn completes", async () => {
    const frames = [];
    vi.stubGlobal("requestAnimationFrame", callback => { frames.push(callback); return frames.length; });
    vi.stubGlobal("cancelAnimationFrame", vi.fn());
    const wrapper = mount(MessageBubble, {
      props: { entry: { id: "answer-live", item_id: "answer-live", attempt_id: "attempt-1", kind: "assistant", text: "abcdef", streaming: true } },
      global: {
        stubs: {
          ResourceMarkdown: { props: ["entry"], template: "<span>{{ entry.text }}</span>" },
        },
      },
    });
    const renderedText = () => wrapper.find(".assistant-msg__md").text();
    const runFrame = async timestamp => {
      const frame = frames.shift();
      expect(frame).toBeTypeOf("function");
      frame(timestamp);
      await wrapper.vm.$nextTick();
    };

    expect(wrapper.find(".msg--assistant").exists()).toBe(false);
    await runFrame(16);
    await runFrame(48);
    expect(renderedText()).toBe("a");

    await wrapper.setProps({ entry: { id: "answer-live", item_id: "answer-live", attempt_id: "attempt-1", kind: "assistant", text: "abcdefghij", streaming: true } });
    await wrapper.setProps({ entry: { id: "answer-live", item_id: "answer-live", kind: "assistant", text: "abcdefghij", streaming: false } });
    expect(renderedText()).toBe("a");

    for (let timestamp = 80; timestamp <= 400 && frames.length; timestamp += 32) {
      await runFrame(timestamp);
    }
    expect(renderedText()).toBe("abcdefghij");
    expect(wrapper.find(".assistant-msg__md--streaming").exists()).toBe(false);
    wrapper.unmount();
    vi.unstubAllGlobals();
  });

  it("sleeps at the streamed tail and resumes when another provider delta arrives", async () => {
    const frames = [];
    vi.stubGlobal("requestAnimationFrame", callback => { frames.push(callback); return frames.length; });
    vi.stubGlobal("cancelAnimationFrame", vi.fn());
    const wrapper = mount(MessageBubble, {
      props: { entry: { id: "answer-tail", item_id: "answer-tail", kind: "assistant", text: "a", streaming: true } },
      global: {
        stubs: {
          ResourceMarkdown: { props: ["entry"], template: "<span>{{ entry.text }}</span>" },
        },
      },
    });
    const runFrame = async timestamp => {
      const frame = frames.shift();
      expect(frame).toBeTypeOf("function");
      frame(timestamp);
      await wrapper.vm.$nextTick();
    };

    await runFrame(16);
    await runFrame(48);
    expect(wrapper.find(".assistant-msg__md").text()).toBe("a");
    expect(frames).toHaveLength(0);

    await wrapper.setProps({ entry: { id: "answer-tail", item_id: "answer-tail", kind: "assistant", text: "ab", streaming: true } });
    expect(frames).toHaveLength(1);
    await runFrame(80);
    await runFrame(112);
    expect(wrapper.find(".assistant-msg__md").text()).toBe("ab");
    expect(frames).toHaveLength(0);

    wrapper.unmount();
    vi.unstubAllGlobals();
  });

  it("keeps assistant markdown code copy controls wired through ResourceMarkdown", async () => {
    const wrapper = mount(MessageBubble, {
      props: { entry: { kind: "assistant", text: "```text\ncopy me\n```" }, agentId: "agent-a" },
    });
    const copy = wrapper.find('[data-markdown-action="copy"]');
    expect(copy.exists()).toBe(true);
    await copy.trigger("click");
    expect(wrapper.find('[data-markdown-action="copy"]').exists()).toBe(true);
    wrapper.unmount();
  });

  it("loads an oversized assistant body from its round only when requested", async () => {
    const request = vi.spyOn(nodeApi, "getConversationItemContent").mockResolvedValue({ text: "complete assistant answer" });
    const wrapper = mount(MessageBubble, {
      props: {
        agentId: "agent-detail",
        entry: {
          kind: "assistant", id: "answer-content", item_id: "answer-content", text: "short preview …",
          details_available: true, detail_item_id: "assistant-record", round_id: "round-detail", round_revision_id: "revision-detail",
        },
      },
    });

    expect(wrapper.text()).toContain("short preview …");
    expect(request).not.toHaveBeenCalled();
    await wrapper.get(".msg__content-detail button").trigger("click");
    await flushPromises();
    expect(request).toHaveBeenCalledWith("agent-detail", "agent-detail", "round-detail", "assistant-record", { revisionId: "revision-detail" });
    expect(wrapper.text()).toContain("complete assistant answer");
    await wrapper.get(".msg__content-detail button").trigger("click");
    expect(wrapper.text()).toContain("short preview …");
    wrapper.unmount();
    request.mockRestore();
  });
});
