/** @vitest-environment jsdom */
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import SettingsDisclosure from "./SettingsDisclosure.vue";

describe("SettingsDisclosure", () => {
  it("exposes an accessible trigger and hidden content as inert until opened", async () => {
    const wrapper = mount(SettingsDisclosure, {
      props: { title: "运行限制" },
      slots: { default: '<input aria-label="上限" value="12" />' },
    });
    const trigger = wrapper.get('[role="button"]');
    const content = wrapper.get('[role="region"]');

    expect(trigger.element.tagName).toBe("DIV");
    expect(trigger.attributes("tabindex")).toBe("0");
    expect(trigger.attributes("aria-expanded")).toBe("false");
    expect(trigger.attributes("aria-controls")).toBe(content.attributes("id"));
    expect(trigger.text()).toBe("运行限制");
    expect(content.attributes("aria-hidden")).toBe("true");
    expect(content.attributes("inert")).toBeDefined();

    await trigger.trigger("click");
    expect(trigger.attributes("aria-expanded")).toBe("true");
    expect(content.attributes("aria-hidden")).toBe("false");
    expect(content.attributes("inert")).toBeUndefined();
    wrapper.unmount();
  });

  it("remains expandable in a readonly fieldset while keeping its form controls disabled", async () => {
    const wrapper = mount({
      components: { SettingsDisclosure },
      template: `
        <fieldset disabled>
          <SettingsDisclosure title="Webhook 配置">
            <input aria-label="Webhook Key" value="saved-key" />
          </SettingsDisclosure>
        </fieldset>
      `,
    });
    const disclosure = wrapper.getComponent(SettingsDisclosure);
    const trigger = disclosure.get('[role="button"]');
    const input = disclosure.get('[aria-label="Webhook Key"]');

    expect(input.element.matches(":disabled")).toBe(true);
    await trigger.trigger("click");
    expect(trigger.attributes("aria-expanded")).toBe("true");
    expect(input.element.matches(":disabled")).toBe(true);
    await trigger.trigger("keydown", { key: "Enter" });
    expect(trigger.attributes("aria-expanded")).toBe("false");
    await trigger.trigger("keydown", { key: " " });
    expect(trigger.attributes("aria-expanded")).toBe("true");
    expect(input.element.matches(":disabled")).toBe(true);
    wrapper.unmount();
  });

  it("allows multiple sections to stay open independently and retains slot drafts", async () => {
    const wrapper = mount({
      components: { SettingsDisclosure },
      template: `
        <SettingsDisclosure title="浏览器">
          <input aria-label="浏览器超时" value="10000" />
        </SettingsDisclosure>
        <SettingsDisclosure title="运行限制">
          <input aria-label="技能上限" value="20" />
        </SettingsDisclosure>
      `,
    });
    const [browser, runtime] = wrapper.findAllComponents(SettingsDisclosure);
    const browserButton = browser.get('[role="button"]');
    const runtimeButton = runtime.get('[role="button"]');

    await browserButton.trigger("click");
    await browser.get('[aria-label="浏览器超时"]').setValue("12000");
    await runtimeButton.trigger("click");
    expect(browserButton.attributes("aria-expanded")).toBe("true");
    expect(runtimeButton.attributes("aria-expanded")).toBe("true");

    await browserButton.trigger("click");
    expect(browserButton.attributes("aria-expanded")).toBe("false");
    expect(runtimeButton.attributes("aria-expanded")).toBe("true");
    await browserButton.trigger("click");
    expect(browser.get('[aria-label="浏览器超时"]').element.value).toBe("12000");
    wrapper.unmount();
  });

  it("follows a dynamic external open state while emitting user changes", async () => {
    const wrapper = mount(SettingsDisclosure, {
      props: { title: "MCP JSON", expanded: false },
    });
    const trigger = wrapper.get('[role="button"]');

    await wrapper.setProps({ expanded: true });
    expect(trigger.attributes("aria-expanded")).toBe("true");
    await trigger.trigger("click");
    expect(trigger.attributes("aria-expanded")).toBe("true");
    expect(wrapper.emitted("update:expanded")).toEqual([[false]]);
    await wrapper.setProps({ expanded: false });
    expect(trigger.attributes("aria-expanded")).toBe("false");
    wrapper.unmount();
  });
});
