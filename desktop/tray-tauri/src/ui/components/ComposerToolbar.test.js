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
    expect(status.find('.composer-toolbar__status-text').text()).toBe("工具执行中");
    expect(status.find('.visually-hidden').text()).toBe("工具执行中");
    expect(status.attributes("title")).toBe("工具执行中");
    expect(wrapper.element.textContent.indexOf('工具执行中')).toBeLessThan(
      wrapper.element.textContent.indexOf("deepseek-chat"),
    );
    wrapper.unmount();
  });

  it("keeps an empty status slot while idle so neighbouring controls stay in place", async () => {
    const wrapper = mount(ComposerToolbar, {
      props: { llmSettings: { model: "deepseek-chat", profiles: [] }, desktopCompact: true },
    });

    const slot = wrapper.find('[role="status"]').element;
    expect(wrapper.find('[role="status"]').text()).toBe('');
    await wrapper.setProps({ statusLabel: '正在等待回复' });
    expect(wrapper.find('[role="status"]').element).toBe(slot);
    await wrapper.setProps({ statusLabel: '正在生成回复' });
    expect(wrapper.find('.composer-toolbar__status-text').text()).toBe('正在生成回复');
    await wrapper.setProps({ statusLabel: '' });
    expect(wrapper.find('[role="status"]').element).toBe(slot);
    expect(wrapper.find('[role="status"]').text()).toBe('');
    expect(wrapper.text()).toContain("deepseek-chat");
    wrapper.unmount();
  });

  it("keeps the model button and chevron when generation disables switching, and closes an open menu", async () => {
    const wrapper = mount(ComposerToolbar, {
      props: { llmSettings: { model: 'model-a', active_profile: 'a', profiles: ['a', 'b'] }, desktopCompact: true },
    });
    const button = wrapper.find('.composer-toolbar__trigger').element;
    const chevron = wrapper.find('.composer-toolbar__chevron').element;
    await wrapper.find('.composer-toolbar__trigger').trigger('click');
    const option = wrapper.findAll('[role="option"]')[1];
    expect(wrapper.find('[role="listbox"]').exists()).toBe(true);
    await wrapper.setProps({ disabled: true, statusLabel: '正在等待回复' });
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false);
    expect(wrapper.find('.composer-toolbar__trigger').element).toBe(button);
    expect(wrapper.find('.composer-toolbar__chevron').element).toBe(chevron);
    expect(button.disabled).toBe(true);
    // An already queued option click must not switch a now-busy Agent.
    await option.trigger('click');
    expect(wrapper.emitted('switch-profile')).toBeUndefined();
    await wrapper.setProps({ disabled: false, statusLabel: '' });
    await wrapper.find('.composer-toolbar__trigger').trigger('click');
    await wrapper.findAll('[role="option"]')[1].trigger('click');
    expect(wrapper.emitted('switch-profile')).toEqual([['b']]);
    wrapper.unmount();
  });
});
