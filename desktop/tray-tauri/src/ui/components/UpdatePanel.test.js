/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import UpdatePanel from "./UpdatePanel.vue";
import * as platformApi from "../api/platform.js";

vi.mock("../api/platform.js", () => ({
  getUpdateStatus: vi.fn(),
  applyAgentUpdate: vi.fn(),
}));

describe("UpdatePanel headers", () => {
  it.each([
    [true, false],
    [false, true],
  ])("embedded=%s controls header visibility", async (embedded, hasHeader) => {
    platformApi.getUpdateStatus.mockResolvedValue({ current_version: "1", latest_version: "1", upgrade_available: false });
    const wrapper = mount(UpdatePanel, { props: { embedded } });
    await flushPromises();
    expect(wrapper.find("header").exists()).toBe(hasHeader);
  });
});
