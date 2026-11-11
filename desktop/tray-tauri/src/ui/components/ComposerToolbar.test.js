/** @vitest-environment jsdom */
import { describe, expect, it } from "vitest";
import { mount } from "@vue/test-utils";
import { nextTick } from "vue";
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

  it("keeps the trigger compact and shows each profile model above its config name", async () => {
    const wrapper = mount(ComposerToolbar, {
      props: {
        llmSettings: { model: "deepseek-chat", active_profile: "production-deepseek", profiles: ["production-deepseek", "backup-qwen"] },
        profileCatalog: [
          { id: "production-deepseek", model: "deepseek-chat" },
          { id: "backup-qwen", model: "qwen-plus" },
        ],
        desktopCompact: true,
      },
    });
    expect(wrapper.find(".composer-toolbar__trigger-label > span:first-child").text()).toBe("deepseek-chat");
    expect(wrapper.find(".composer-toolbar__profile-label").exists()).toBe(false);
    expect(wrapper.find(".composer-toolbar__trigger").attributes("title")).toContain("配置: production-deepseek");
    await wrapper.find(".composer-toolbar__trigger").trigger("click");
    const options = wrapper.findAll('[role="option"]');
    expect(options.map(option => option.find(".composer-toolbar__option-model").text())).toEqual(["deepseek-chat", "qwen-plus"]);
    expect(options.map(option => option.find(".composer-toolbar__option-profile").text())).toEqual(["production-deepseek", "backup-qwen"]);
    expect(options[0].attributes("aria-selected")).toBe("true");
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

  it("opens the model settings action with a single profile outside the options listbox", async () => {
    const wrapper = mount(ComposerToolbar, {
      props: { llmSettings: { model: "model-a", active_profile: "profile-a", profiles: ["profile-a"] }, desktopCompact: true },
    });

    await wrapper.find('.composer-toolbar__trigger').trigger('click');
    expect(wrapper.find('[role="listbox"] [role="option"]').exists()).toBe(true);
    const settingsAction = wrapper.find('.composer-toolbar__settings-action');
    expect(settingsAction.text()).toBe('模型设置…');
    expect(wrapper.find('[role="listbox"]').attributes('aria-label')).toBe('选择模型配置');
    expect(settingsAction.element.closest('[role="listbox"]')).toBeNull();
    await settingsAction.trigger('click');
    expect(wrapper.emitted('open-model-settings')).toEqual([['profile-a']]);
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it("exposes the same keyboard-focused profile picker for an error recovery action", async () => {
    const wrapper = mount(ComposerToolbar, {
      props: { llmSettings: { model: "model-a", active_profile: "profile-a", profiles: ["profile-a", "profile-b"] }, desktopCompact: true },
      attachTo: document.body,
    });

    expect(wrapper.vm.openPicker()).toBe(true);
    await nextTick();
    expect(wrapper.find('[role="listbox"]').exists()).toBe(true);
    expect(wrapper.find('[aria-selected="true"]').element).toBe(document.activeElement);
    wrapper.unmount();
  });

  it("falls back to the active Agent profile when model settings omit active_profile", async () => {
    const wrapper = mount(ComposerToolbar, {
      props: {
        llmSettings: { model: "model-a", profiles: ["profile-a"] },
        settingsProfileId: "profile-a",
        desktopCompact: true,
      },
    });

    await wrapper.find('.composer-toolbar__trigger').trigger('click');
    await wrapper.find('.composer-toolbar__settings-action').trigger('click');
    expect(wrapper.emitted('open-model-settings')).toEqual([['profile-a']]);
    wrapper.unmount();
  });
});
