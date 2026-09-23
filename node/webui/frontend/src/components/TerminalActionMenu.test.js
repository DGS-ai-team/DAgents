/** @vitest-environment jsdom */
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import TerminalActionMenu from "./TerminalActionMenu.vue";

describe("TerminalActionMenu", () => {
  it("opens and closes on explicit clicks without hover timing", async () => {
    const wrapper = mount(TerminalActionMenu, { props: { status: "connected" } });
    const trigger = wrapper.get('button[aria-label="终端操作"]');

    expect(wrapper.find('[role="menu"]').exists()).toBe(false);
    await trigger.trigger("click");
    expect(wrapper.find('[role="menu"]').exists()).toBe(true);
    await trigger.trigger("click");
    expect(wrapper.find('[role="menu"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it("closes from Escape or an outside pointer without closing between trigger and menu", async () => {
    const wrapper = mount(TerminalActionMenu, { props: { status: "connected" } });
    const trigger = wrapper.get('button[aria-label="终端操作"]');
    await trigger.trigger("click");
    expect(wrapper.find('[role="menu"]').exists()).toBe(true);

    await wrapper.trigger("pointerleave");
    expect(wrapper.find('[role="menu"]').exists()).toBe(true);

    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    await wrapper.vm.$nextTick();
    expect(wrapper.find('[role="menu"]').exists()).toBe(false);

    await trigger.trigger("click");
    document.dispatchEvent(new Event("pointerdown"));
    await wrapper.vm.$nextTick();
    expect(wrapper.find('[role="menu"]').exists()).toBe(false);
    wrapper.unmount();
  });
});
