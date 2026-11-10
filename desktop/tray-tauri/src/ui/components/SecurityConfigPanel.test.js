/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import SecurityConfigPanel from "./SecurityConfigPanel.vue";
import SettingsDisclosure from "./SettingsDisclosure.vue";
import * as api from "../api/node.js";

vi.mock("../api/node.js", () => ({ getSetupConfig: vi.fn(), patchSetupConfig: vi.fn() }));

const setupConfig = () => ({
  config_path: "node_settings.db",
  config_writable: true,
  tools: {
    bash_output_encoding: "utf-8",
    file_encoding: "utf-8",
    bash_compress_enabled: true,
    bash_compress_max_output_chars: 20000,
    bash_compress_max_stderr_chars: 10000,
    bash_compress_default_max_output_chars: 12000,
    bash_compress_default_max_stderr_chars: 16000,
  },
  hooks: { duplicate_tool_call_enabled: true, duplicate_tool_call_window_seconds: 8 },
});

beforeEach(() => {
  vi.clearAllMocks();
  api.getSetupConfig.mockResolvedValue(setupConfig());
  api.patchSetupConfig.mockResolvedValue(setupConfig());
});

describe("SecurityConfigPanel advanced controls", () => {
  it("keeps protection switches visible and preserves advanced values on save", async () => {
    const wrapper = mount(SecurityConfigPanel);
    await flushPromises();

    const advanced = wrapper.findAllComponents(SettingsDisclosure);
    expect(advanced).toHaveLength(3);
    expect(advanced.every((item) => item.get('[role="button"]').attributes("aria-expanded") === "false")).toBe(true);
    expect(wrapper.findAll('.settings-row > input[type="checkbox"]')).toHaveLength(2);

    await advanced[0].get('[role="button"]').trigger("click");
    await advanced[1].get('[role="button"]').trigger("click");
    await advanced[2].get('[role="button"]').trigger("click");
    const outputLimit = wrapper.get('input[type="number"][min="1"]');
    await outputLimit.setValue("24000");
    await wrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();

    expect(api.patchSetupConfig).toHaveBeenCalledWith(expect.objectContaining({
      tools: expect.objectContaining({
        bash_output_encoding: "utf-8",
        file_encoding: "utf-8",
        bash_compress_max_output_chars: 24000,
        bash_compress_max_stderr_chars: 10000,
      }),
      hooks: expect.objectContaining({ duplicate_tool_call_window_seconds: 8 }),
    }));
    wrapper.unmount();
  });

  it("rejects zero in the UI and provides an explicit reset to product defaults", async () => {
    const wrapper = mount(SecurityConfigPanel);
    await flushPromises();
    const inputs = wrapper.findAll('input[type="number"][min="1"]');
    await inputs[0].setValue("0");
    await wrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();
    expect(api.patchSetupConfig).not.toHaveBeenCalled();
    expect(wrapper.get('[role="alert"]').text()).toContain("大于 0 的整数");

    await inputs[0].setValue("1");
    const restoreButton = wrapper.get('button[aria-label="恢复默认上限"]');
    expect(restoreButton.attributes("title")).toBe("恢复默认上限");
    const renderedIcon = restoreButton.get("svg");
    expect(renderedIcon.attributes("viewBox")).toBeTruthy();
    expect(renderedIcon.find("path").exists()).toBe(true);
    await restoreButton.trigger("click");
    expect(inputs[0].element.value).toBe("12000");
    expect(inputs[1].element.value).toBe("16000");
    wrapper.unmount();
  });
});
