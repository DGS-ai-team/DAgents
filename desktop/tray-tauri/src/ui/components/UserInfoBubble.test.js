// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { mount } from "@vue/test-utils";
import UserInfoBubble from "./UserInfoBubble.vue";

vi.mock("./ToolGroupIcon.vue", () => ({ default: { template: "<span />" } }));

describe("UserInfoBubble", () => {
  it("submits its own free-text answer explicitly", async () => {
    const wrapper = mount(UserInfoBubble, {
      props: {
        data: { user_information_args: { question: "Which range?", options: [], required: true } },
      },
    });

    expect(wrapper.find("button").element.disabled).toBe(true);
    await wrapper.get("textarea[aria-label='补充回答']").setValue("This quarter");
    await wrapper.get("button").trigger("click");

    expect(wrapper.emitted("submit")).toEqual([["This quarter"]]);
  });

  it("submits the selected option from the card without borrowing composer text", async () => {
    const wrapper = mount(UserInfoBubble, {
      props: {
        data: {
          user_information_args: {
            question: "Which range?",
            options: [{ id: "quarter", label: "This quarter", value: "quarter" }],
          },
        },
      },
    });

    await wrapper.get("button").trigger("click");
    expect(wrapper.emitted("submit")).toEqual([[""]]);
  });
});
