/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import LinuxAgentPanel from "./LinuxAgentPanel.vue";
import * as api from "../api/node.js";

const { routerPush } = vi.hoisted(() => ({ routerPush: vi.fn() }));
vi.mock("vue-router", () => ({ useRouter: () => ({ push: routerPush }) }));
vi.mock("../api/node.js", () => ({
  listLinuxChannels: vi.fn(),
  getAgentLinuxChannels: vi.fn(),
  putAgentLinuxChannels: vi.fn(),
}));

beforeEach(() => {
  vi.clearAllMocks();
  api.listLinuxChannels.mockResolvedValue({ channels: [] });
  api.getAgentLinuxChannels.mockResolvedValue({ bindings: [] });
});

describe("Linux agent resource setup", () => {
  it("links to global channel creation while preserving the current agent target", async () => {
    const wrapper = mount(LinuxAgentPanel, { props: { agentId: "agent-a" } });
    await flushPromises();
    await wrapper.get(".linux-agent-panel__empty button").trigger("click");

    expect(routerPush).toHaveBeenCalledWith({
      path: "/settings/linux-channels",
      query: { returnAgentId: "agent-a", returnSection: "resources", create: "1" },
    });
    wrapper.unmount();
  });
});
