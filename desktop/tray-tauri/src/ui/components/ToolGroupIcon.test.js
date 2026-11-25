/** @vitest-environment jsdom */
import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import { nodeIconNames } from "@shared-icons/ui/node-registry.js";

vi.mock("./UiIcon.vue", () => ({
  default: {
    props: ["name", "size"],
    template: '<i class="icon-stub" :data-name="name"></i>',
  },
}));

import ToolGroupIcon from "./ToolGroupIcon.vue";

describe("web tool group icons", () => {
  it.each([["web_search", "search"], ["web_fetch", "file"]])("maps %s to an available icon", (group, icon) => {
    const wrapper = mount(ToolGroupIcon, { props: { name: group } });
    expect(wrapper.get(".icon-stub").attributes("data-name")).toBe(icon);
    expect(nodeIconNames).toContain(icon);
  });
});
