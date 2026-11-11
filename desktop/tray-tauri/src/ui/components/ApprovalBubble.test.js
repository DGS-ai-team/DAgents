/** @vitest-environment jsdom */
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import ApprovalBubble from "./ApprovalBubble.vue";

function approvalData({ approvalId = "request-a", calls = [] } = {}) {
  return {
    approval_id: approvalId,
    approval_args: {
      tool_calls: calls.map((call, index) => ({
        id: call.id || `call-${index + 1}`,
        type: "function",
        function: {
          name: call.name || "read_file",
          arguments: JSON.stringify(call.args || { call_purpose: `检查 ${index + 1}`, path: `C:\\very-long-folder\\${index + 1}.txt` }),
        },
        approval_reason: call.reason || "需要确认读取目标文件",
        duplicate_meta: call.duplicate ? { window_seconds: 30, result_preview: "上次读取摘要" } : undefined,
        risk_level: call.risk,
      })),
    },
  };
}

describe("ApprovalBubble", () => {
  it("keeps expanding independent from selection and shows actual parameters only when expanded", async () => {
    const wrapper = mount(ApprovalBubble, {
      props: {
        requestKey: "request-a",
        data: approvalData({ calls: [{}, {}] }),
      },
    });
    const rows = wrapper.findAll(".approval-item");

    expect(wrapper.find(".approval-actions__approve").text()).toBe("批准所选 0 项");
    expect(wrapper.find(".approval-actions__approve").element.disabled).toBe(true);
    expect(wrapper.text()).toContain("未选中的操作本次不执行");
    expect(wrapper.text()).not.toContain("C:\\very-long-folder");

    await rows[0].find(".approval-item__toggle").trigger("click");
    expect(rows[0].find(".approval-item__toggle").attributes("aria-expanded")).toBe("true");
    expect(rows[0].find("input[type=checkbox]").element.checked).toBe(false);
    expect(rows[0].text()).toContain("需要确认读取目标文件");
    expect(rows[0].text()).toContain("C:\\very-long-folder\\1.txt");

    await rows[1].find("input[type=checkbox]").setValue(true);
    expect(wrapper.find(".approval-actions__approve").text()).toBe("批准所选 1 项");
    expect(rows[0].find("input[type=checkbox]").element.checked).toBe(false);
    expect(rows[1].find(".approval-item__toggle").attributes("aria-expanded")).toBe("false");

    await wrapper.find(".approval-actions__approve").trigger("click");
    expect(wrapper.emitted("approve-selection")).toEqual([[ ["call-2"] ]]);
  });

  it("retains selection for retry after a failed resume, then resets for a new request", async () => {
    const initial = approvalData({ calls: [{}, {}] });
    const wrapper = mount(ApprovalBubble, {
      props: { requestKey: "request-a", data: initial },
    });
    const rows = wrapper.findAll(".approval-item");
    await rows[0].find("input[type=checkbox]").setValue(true);
    await rows[1].find(".approval-item__toggle").trigger("click");

    await wrapper.setProps({ busy: true, data: { ...initial, message: "刷新中的状态" } });
    expect(wrapper.findAll(".approval-item")[0].find("input[type=checkbox]").element.checked).toBe(true);
    expect(wrapper.findAll(".approval-item")[1].find(".approval-item__toggle").attributes("aria-expanded")).toBe("true");
    await wrapper.find(".approval-actions__approve").trigger("click");
    expect(wrapper.emitted("approve-selection")).toBeUndefined();

    // Failed resume leaves the same request queued. When busy clears, selection is still available to retry.
    await wrapper.setProps({ busy: false, data: initial });
    expect(wrapper.findAll(".approval-item")[0].find("input[type=checkbox]").element.checked).toBe(true);
    expect(wrapper.findAll(".approval-item")[1].find(".approval-item__toggle").attributes("aria-expanded")).toBe("true");
    await wrapper.find(".approval-actions__approve").trigger("click");
    expect(wrapper.emitted("approve-selection")).toEqual([[ ["call-1"] ]]);

    await wrapper.setProps({ requestKey: "request-b", data: approvalData({ approvalId: "request-b", calls: [{}, {}] }), busy: false });
    expect(wrapper.findAll(".approval-item")[0].find("input[type=checkbox]").element.checked).toBe(false);
    expect(wrapper.findAll(".approval-item")[1].find(".approval-item__toggle").attributes("aria-expanded")).toBe("false");
  });

  it("uses call_purpose or purpose, falls back to a friendly name, and emits the single selected id", async () => {
    const wrapper = mount(ApprovalBubble, {
      props: {
        requestKey: "request-single",
        data: approvalData({ approvalId: "request-single", calls: [
          { id: "purpose-call", name: "create_temporary_agent", args: { purpose: "整理接口文档" } },
        ] }),
      },
    });
    expect(wrapper.find(".approval-item__summary").text()).toBe("整理接口文档");
    await wrapper.find(".approval-actions__approve").trigger("click");
    expect(wrapper.emitted("approve-selection")).toEqual([[ ["purpose-call"] ]]);

    await wrapper.setProps({
      requestKey: "request-friendly",
      data: approvalData({ approvalId: "request-friendly", calls: [
        { id: "plain-call", name: "read_file", args: { path: "C:\\long\\folder\\document.txt" } },
      ] }),
    });
    expect(wrapper.find(".approval-item__summary").text()).toBe("读取文件");
  });

  it("shows repeat and approval details and rejects the whole batch once", async () => {
    const wrapper = mount(ApprovalBubble, {
      props: {
        requestKey: "request-repeat",
        data: approvalData({ calls: [{ duplicate: true }] }),
      },
    });
    await wrapper.find(".approval-item__toggle").trigger("click");
    expect(wrapper.text()).toContain("重复调用");
    expect(wrapper.text()).toContain("上次读取摘要");
    expect(wrapper.text()).toContain("审批原因");
    await wrapper.find(".approval-actions__reject").trigger("click");
    expect(wrapper.emitted("reject-all")).toHaveLength(1);
  });
});
