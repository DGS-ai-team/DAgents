/** @vitest-environment jsdom */
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import MessageBubble from "./MessageBubble.vue";

describe("MessageBubble context boundary", () => {
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
});
