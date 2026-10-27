/** @vitest-environment jsdom */
import { afterEach, describe, expect, it } from "vitest";
import { mount } from "@vue/test-utils";
import ComposerToolbar from "./ComposerToolbar.vue";
import { resetStatusLines, syncTurnStatus } from "../stores/statusLines.js";

describe("ComposerToolbar status", () => {
  afterEach(() => resetStatusLines());

  it("shows the active turn status before the model name", () => {
    syncTurnStatus({ phase: "tool_executing" });
    const wrapper = mount(ComposerToolbar, {
      props: { llmSettings: { model: "deepseek-chat", profiles: [] }, desktopCompact: true },
    });

    const status = wrapper.find('[role="status"]');
    expect(status.text()).toBe("工具中");
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
