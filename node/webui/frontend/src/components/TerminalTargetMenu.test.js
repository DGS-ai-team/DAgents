/** @vitest-environment jsdom */
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import TerminalTargetMenu from "./TerminalTargetMenu.vue";

const targets = [{ kind: "local", id: "", shell: "powershell", label: "本机 · PowerShell", description: "Windows PowerShell" }];

describe("TerminalTargetMenu", () => {
  it("opens and closes from the trigger, Escape, and outside pointer", async () => {
    const wrapper = mount(TerminalTargetMenu, { props: { targets } });
    const trigger = wrapper.get('button[aria-label="新建终端"]');

    await trigger.trigger("click");
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true);
    await trigger.trigger("click");
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false);

    await trigger.trigger("click");
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    await wrapper.vm.$nextTick();
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false);

    await trigger.trigger("click");
    document.dispatchEvent(new Event("pointerdown"));
    await wrapper.vm.$nextTick();
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false);
    wrapper.unmount();
  });
});
