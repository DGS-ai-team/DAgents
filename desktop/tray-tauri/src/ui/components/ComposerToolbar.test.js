/** @vitest-environment jsdom */
import { describe, expect, it } from "vitest";
import { mount } from "@vue/test-utils";
import ComposerToolbar from "./ComposerToolbar.vue";

describe("ComposerToolbar status", () => {
  it("shows the active turn status before the model name", () => {
    const wrapper = mount(ComposerToolbar, {
      props: { llmSettings: { model: "deepseek-chat", profiles: [] }, desktopCompact: true, statusLabel: "工具执行中" },
    });

    const status = wrapper.find('[role="status"]');
    expect(status.text()).toBe("工具执行中");
    expect(status.attributes("title")).toBe("工具执行中");
    expect(wrapper.element.textContent.indexOf(status.text())).toBeLessThan(
      wrapper.element.textContent.indexOf("deepseek-chat"),
    );
    wrapper.unmount();
  });

  it("does not occupy the composer footer while the turn is idle", () => {
    const wrapper = mount(ComposerToolbar, {
      props: { llmSettings: { model: "deepseek-chat", profiles: [] }, desktopCompact: true },
    });

    expect(wrapper.find('[role="status"]').exists()).toBe(false);
    expect(wrapper.text()).toContain("deepseek-chat");
    wrapper.unmount();
  });
});
