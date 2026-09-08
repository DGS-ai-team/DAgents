/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import FeedbackSettings from "./FeedbackSettings.vue";
import * as api from "../../api/node.js";

vi.mock("../../api/node.js", () => ({
  listFeedback: vi.fn(),
  getFeedbackTarget: vi.fn(),
  getFeedback: vi.fn(),
  createFeedback: vi.fn(),
  syncFeedback: vi.fn(),
}));

const target = { configured: true, enabled: true, url: "http://manage.test", node_id: "node-a" };
const feedback = { client_feedback_id: "stable-id", title: "标题", body: "正文", delivery_status: "delivered" };

function render() {
  return mount(FeedbackSettings, { global: { stubs: { SettingsPageHeader: true } } });
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  api.listFeedback.mockResolvedValue({ items: [] });
  api.getFeedbackTarget.mockResolvedValue(target);
  api.getFeedback.mockRejectedValue(new Error("not found"));
});

describe("FeedbackSettings submission durability", () => {
  it("persists the generated client id before a lost response and recovers the authoritative row", async () => {
    let draftAtRequest = "";
    api.createFeedback.mockImplementation(async () => {
      draftAtRequest = localStorage.getItem("dagents.feedback.draft");
      throw new Error("network timeout");
    });
    api.getFeedback.mockResolvedValue(feedback);
    const wrapper = render();
    await flushPromises();
    await wrapper.find('input[placeholder="用一句话概括"]').setValue("标题");
    await wrapper.find("textarea").setValue("正文");
    await wrapper.find('button.btn--primary').trigger("click");
    await flushPromises();
    expect(draftAtRequest).toContain('"client_feedback_id":"');
    expect(api.getFeedback).toHaveBeenCalledWith(expect.any(String));
    expect(wrapper.text()).toContain("反馈已保存");
    expect(wrapper.text()).not.toContain("提交失败");
  });

  it("does not report a successful server submission as a storage failure", async () => {
    api.createFeedback.mockResolvedValue(feedback);
    const originalSetItem = Storage.prototype.setItem;
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("blocked"); });
    const wrapper = render();
    await flushPromises();
    await wrapper.find('input[placeholder="用一句话概括"]').setValue("标题");
    await wrapper.find("textarea").setValue("正文");
    await wrapper.find('button.btn--primary').trigger("click");
    await flushPromises();
    expect(api.createFeedback).toHaveBeenCalledTimes(1);
    expect(wrapper.text()).toContain("已送达当前 Manage");
    expect(wrapper.text()).not.toContain("无法保存草稿");
    Storage.prototype.setItem = originalSetItem;
  });

  it("keeps an offline feedback as a draft and never posts it", async () => {
    api.getFeedbackTarget.mockResolvedValue({ configured: false, enabled: false, url: "", node_id: "" });
    const wrapper = render();
    await flushPromises();
    await wrapper.find('input[placeholder="用一句话概括"]').setValue("离线标题");
    await wrapper.find("textarea").setValue("离线正文");
    await wrapper.find('button.btn--primary').trigger("click");
    await flushPromises();
    expect(api.createFeedback).not.toHaveBeenCalled();
    expect(localStorage.getItem("dagents.feedback.draft")).toContain("离线标题");
    expect(wrapper.text()).toContain("仅保存在本机草稿");
  });
});
