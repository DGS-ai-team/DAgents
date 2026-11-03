/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import SecurityConfigPanel from "./SecurityConfigPanel.vue";
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

    const advanced = wrapper.findAll("details");
    expect(advanced).toHaveLength(3);
    expect(advanced.every((item) => !item.element.open)).toBe(true);
    expect(wrapper.findAll('.settings-toggle input[type="checkbox"]')).toHaveLength(2);

    await advanced[0].get("summary").trigger("click");
    await advanced[1].get("summary").trigger("click");
    await advanced[2].get("summary").trigger("click");
    const outputLimit = wrapper.get('input[type="number"][min="0"]');
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
});
