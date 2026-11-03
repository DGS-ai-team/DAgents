/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import LinuxChannelsSettings from "./LinuxChannelsSettings.vue";
import * as api from "../../api/node.js";

const { route, routerReplace } = vi.hoisted(() => ({ route: { query: {} }, routerReplace: vi.fn() }));
vi.mock("vue-router", () => ({ useRoute: () => route, useRouter: () => ({ replace: routerReplace }) }));
vi.mock("../../api/node.js", () => ({
  listLinuxChannels: vi.fn(),
  listLinuxCredentials: vi.fn(),
  createLinuxChannel: vi.fn(),
  createLinuxCredential: vi.fn(),
}));

const savedCredential = { credential_id: "credential-a", display_name: "Fixture key", auth_type: "private_key" };

beforeEach(() => {
  vi.clearAllMocks();
  route.query = {};
  api.listLinuxChannels.mockResolvedValue({ channels: [] });
  api.listLinuxCredentials.mockResolvedValue({ credentials: [savedCredential] });
  api.createLinuxChannel.mockResolvedValue({ channel_id: "channel-a" });
});

describe("Linux channel return flow", () => {
  it("creates a global SSH channel and returns to the originating agent resource section", async () => {
    route.query = { create: "1", returnAgentId: "agent-a", returnSection: "resources" };
    const wrapper = mount(LinuxChannelsSettings, {
      global: {
        stubs: {
          SettingsPageHeader: true,
          UiIcon: true,
          UiSelect: {
            props: ["modelValue", "options"],
            emits: ["update:modelValue", "change"],
            template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value); $emit(\'change\', $event.target.value)"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>',
          },
        },
      },
    });
    await flushPromises();

    expect(wrapper.get("#channel-form-title").text()).toBe("新增 SSH 通道");
    await wrapper.get('input[placeholder="例如：生产服务器"]').setValue("Test server");
    await wrapper.get('input[placeholder="例如：10.0.0.21"]').setValue("10.0.0.21");
    await wrapper.get('input[placeholder="例如：deploy"]').setValue("deploy");
    await wrapper.get(".linux-settings__channel-editor select").setValue("credential-a");
    await wrapper.get(".linux-settings__modal-actions .btn--primary").trigger("click");
    await flushPromises();

    expect(api.createLinuxChannel).toHaveBeenCalledWith(expect.objectContaining({
      host: "10.0.0.21",
      username: "deploy",
      credential_id: "credential-a",
      host_key_policy: "known_hosts",
    }));
    expect(routerReplace).toHaveBeenCalledWith({
      name: "settings-agent-detail",
      params: { agentId: "agent-a" },
      query: { section: "resources" },
    });
    wrapper.unmount();
  });
});
