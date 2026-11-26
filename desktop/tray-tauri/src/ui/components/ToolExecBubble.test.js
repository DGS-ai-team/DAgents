/** @vitest-environment jsdom */
import { mount } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import ToolExecBubble from "./ToolExecBubble.vue";

describe("ToolExecBubble elapsed state", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("refreshes a partial tool-call elapsed label and releases the shared clock when settled", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(10_000);

    const wrapper = mount(ToolExecBubble, {
      props: {
        entry: {
          id: "call-1",
          kind: "tool_call",
          partial: true,
          startedAt: Date.now(),
          data: { tool_name: "read_file", path: "report.md" },
        },
      },
      global: {
        stubs: {
          ImageResultPreview: true,
          ReadFileResultPreview: true,
          ToolCardFieldList: true,
          ToolGroupIcon: true,
          UiIcon: true,
        },
      },
    });

    expect(wrapper.get('[role="status"]').attributes("aria-label")).toBe("生成中 · 0ms");
    vi.advanceTimersByTime(1_000);
    await wrapper.vm.$nextTick();
    expect(wrapper.get('[role="status"]').attributes("aria-label")).toBe("生成中 · 1.0s");

    await wrapper.setProps({
      entry: {
        id: "call-1",
        kind: "tool_call",
        partial: false,
        startedAt: Date.now() - 1_000,
        data: { tool_name: "read_file", path: "report.md" },
      },
    });
    expect(wrapper.get('[role="status"]').attributes("aria-label")).toBe("执行中");
    expect(vi.getTimerCount()).toBe(0);
    wrapper.unmount();
  });
});
