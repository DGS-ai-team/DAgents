// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { mount } from "@vue/test-utils";
import ToolGroupRow from "./ToolGroupRow.vue";
import * as nodeApi from "../api/node.js";
import ToolExecBubble from "./ToolExecBubble.vue";

function step(id, name, purpose, status = null) {
  const callEntry = {
    kind: "tool_call",
    data: { tool_name: name, tool_call_id: id, arguments: JSON.stringify(purpose ? { call_purpose: purpose } : {}) },
  };
  const resultEntry = status
    ? { kind: "tool_result", data: { tool_name: name, tool_call_id: id, status, content: "result" } }
    : null;
  const phases = {
    succeeded: "completed",
    completed: "completed",
    denied: "rejected",
    rejected: "rejected",
    failed: "failed",
    error: "failed",
    cancelled: "cancelled",
    canceled: "cancelled",
    timed_out: "cancelled",
    queued: "background",
  };
  return { key: id, callEntry, resultEntry, phase: phases[status] || (status ? "completed" : "running") };
}

describe("ToolGroupRow", () => {
  it("keeps an active run in one stable parent row with no collapsed child activity rows", () => {
    const wrapper = mount(ToolGroupRow, { props: { streamItemKey: "tool-group-first", conversationOrdinal: 4, steps: [step("first", "bash_run", "检查公式")] } });
    const parentKey = wrapper.attributes("class");
    expect(wrapper.get(".tool-group-row__title").text()).toBe("检查公式");
    expect(wrapper.get(".tool-group-row__status").text()).toBe("执行中");
    expect(wrapper.find(".tool-group-row__active").exists()).toBe(false);
    expect(wrapper.find(".tool-group-row__steps").exists()).toBe(false);
    expect(wrapper.findAll(".tool-summary-row")).toHaveLength(0);
    expect(wrapper.get(".tool-group-row__head").attributes("aria-expanded")).toBe("false");
    expect(wrapper.get(".tool-group-row").attributes("data-stream-item-key")).toBe("tool-group-first");
    expect(wrapper.get(".tool-group-row").attributes("data-conversation-ordinal")).toBe("4");
    expect(parentKey).toContain("tool-group-row");
    wrapper.unmount();
  });

  it("shows tool-friendly fallback when call_purpose is missing and supports temporary-agent purpose", async () => {
    const wrapper = mount(ToolGroupRow, {
      props: { steps: [step("no-purpose", "read_file", "")] },
    });
    expect(wrapper.get(".tool-group-row__title").text()).toBe("读取文件");
    await wrapper.setProps({ steps: [step("temp", "create_temporary_agent", "整理接口文档")] });
    expect(wrapper.get(".tool-group-row__title").text()).toBe("整理接口文档");
    wrapper.unmount();
  });

  it("keeps child expansion across run-to-complete updates and parent collapse/reopen", async () => {
    const running = step("stable-first", "bash_run", "检查服务状态");
    const wrapper = mount(ToolGroupRow, { props: { steps: [running] } });
    await wrapper.get(".tool-group-row__head").trigger("click");
    const child = wrapper.get(".tool-summary-row__head");
    expect(child.text()).toContain("检查服务状态");
    expect(wrapper.find(".tool-summary-row__detail").exists()).toBe(false);
    expect(wrapper.get(".tool-summary-row").classes()).toContain("tool-summary-row--embedded");
    await child.trigger("click");
    expect(wrapper.get(".tool-summary-row__head").attributes("aria-expanded")).toBe("true");
    const result = { kind: "tool_result", data: { tool_name: "bash_run", tool_call_id: "stable-first", status: "succeeded", content: "done" } };
    await wrapper.setProps({ steps: [{ ...running, resultEntry: result, phase: "completed" }] });
    expect(wrapper.get(".tool-group-row__head").attributes("aria-expanded")).toBe("true");
    expect(wrapper.get(".tool-summary-row__head").attributes("aria-expanded")).toBe("true");
    expect(wrapper.get(".tool-summary-row__status").text()).toBe("已完成");
    await wrapper.get(".tool-group-row__head").trigger("click");
    expect(wrapper.find(".tool-group-row__steps").exists()).toBe(false);
    expect(wrapper.findAll(".tool-summary-row")).toHaveLength(0);
    await wrapper.get(".tool-group-row__head").trigger("click");
    expect(wrapper.get(".tool-summary-row__head").attributes("aria-expanded")).toBe("true");
    expect(wrapper.find(".tool-group-row__steps").exists()).toBe(true);
    wrapper.unmount();
  });

  it("uses a completion headline only for all-success groups and keeps mixed failure visible", () => {
    const success = step("ok", "read_file", "读取配置", "succeeded");
    const failed = step("bad", "bash_run", "检查服务", "failed");
    const allDone = mount(ToolGroupRow, { props: { steps: [success, step("ok-2", "bash_run", "更新结果", "completed")] } });
    expect(allDone.get(".tool-group-row__title").text()).toBe("已完成 2 项操作");
    expect(allDone.find(".tool-group-row__status").exists()).toBe(false);
    allDone.unmount();

    const mixed = mount(ToolGroupRow, { props: { steps: [success, failed] } });
    expect(mixed.get(".tool-group-row__title").text()).toBe("检查服务");
    expect(mixed.get(".tool-group-row__status").text()).toContain("失败");
    expect(mixed.get(".tool-group-row__head").attributes("aria-label")).toContain("检查服务");
    mixed.unmount();
  });

  it("does not report an adapter success as completed when coordinator says cancelled", async () => {
    const cancelled = {
      ...step("cancelled-write", "write_file", "更新审批测试文档", "succeeded"),
      phase: "cancelled",
    };
    const wrapper = mount(ToolGroupRow, { props: { steps: [cancelled] } });

    expect(wrapper.get(".tool-group-row__title").text()).toBe("更新审批测试文档");
    expect(wrapper.get(".tool-group-row__title").text()).not.toContain("已完成");
    expect(wrapper.get(".tool-group-row__status").text()).toBe("1项取消");
    expect(wrapper.get(".tool-group-row__head").attributes("aria-label")).toContain("1 项取消");
    await wrapper.get(".tool-group-row__head").trigger("click");
    expect(wrapper.get(".tool-summary-row__status").text()).toBe("已终止");
    wrapper.unmount();
  });

  it("summarizes mixed approval results without replacing the current tool purpose", () => {
    const success = step("created", "write_file", "创建核对清单", "succeeded");
    const denied = step("refused", "write_file", "创建问题清单", "denied");
    const wrapper = mount(ToolGroupRow, { props: { steps: [success, denied] } });

    expect(wrapper.get(".tool-group-row__title").text()).toBe("创建问题清单");
    expect(wrapper.get(".tool-group-row__status").text()).toBe("1完成·1未批准");
    expect(wrapper.get(".tool-group-row__status").attributes("title")).toBe("1 项完成 · 1 项未批准");
    expect(wrapper.get(".tool-group-row__head").attributes("aria-label")).toContain("1 项未批准");
    wrapper.unmount();
  });

  it("shows one compact status without wrapping and keeps full counts in the accessible label", () => {
    const running = mount(ToolGroupRow, {
      props: { steps: [step("live", "bash_run", "运行检查"), step("done", "read_file", "读取文件", "succeeded")] },
    });
    expect(running.get(".tool-group-row__status").text()).toBe("执行中");
    expect(running.get(".tool-group-row__status").attributes("title")).toContain("1 项完成");
    expect(running.get(".tool-group-row__head").attributes("aria-label")).toContain("1 项执行中");
    running.unmount();

    const pending = step("pending", "bash_run", "待确认的操作");
    pending.phase = "pending";
    const pendingGroup = mount(ToolGroupRow, { props: { steps: [pending] } });
    expect(pendingGroup.get(".tool-group-row__status").text()).toBe("待确认");
    pendingGroup.unmount();

    const failed = mount(ToolGroupRow, {
      props: { steps: [step("bad", "bash_run", "检查公式", "failed"), step("cancel", "read_file", "读取规则", "cancelled")] },
    });
    expect(failed.get(".tool-group-row__status").text()).toBe("1项失败");
    expect(failed.get(".tool-group-row__status").attributes("title")).toContain("1 项取消");
    failed.unmount();
  });

  it("retains each tool result status in the expanded list", async () => {
    const wrapper = mount(ToolGroupRow, {
      props: { steps: [step("one", "read_file", "读取配置", "succeeded"), step("two", "bash_run", "运行检查", "failed")] },
    });
    await wrapper.get(".tool-group-row__head").trigger("click");
    const rows = wrapper.findAll(".tool-summary-row");
    expect(rows.map((row) => row.find(".tool-summary-row__status").text())).toEqual(["已完成", "执行失败"]);
    expect(rows.map((row) => row.find(".tool-summary-row__text").text())).toEqual(["读取配置", "运行检查"]);
    wrapper.unmount();
  });

  it("loads full tool details only after the user expands a summarized step", async () => {
    const request = vi.spyOn(nodeApi, "getConversationRoundToolStep").mockResolvedValue({
      steps: [{
        tool_call_id: "lazy-call",
        call_entry: { kind: "tool_call", id: "tool-call/lazy-call", detail_item_id: "call-record", data: { tool_call_id: "lazy-call", tool_name: "read_file", arguments: { path: "secret.txt" } } },
        result_entry: { kind: "tool_result", id: "result-lazy-call", data: { tool_call_id: "lazy-call", content: "full result" } },
      }],
    });
    const summarized = step("lazy-call", "read_file", "查看说明");
    summarized.callEntry.details_available = true;
    summarized.callEntry.detail_item_id = "call-record";
    summarized.callEntry.round_id = "round-lazy";
    summarized.callEntry.round_revision_id = "revision-lazy";
    const wrapper = mount(ToolGroupRow, { props: { agentId: "agent-lazy", steps: [summarized] } });

    expect(request).not.toHaveBeenCalled();
    await wrapper.get(".tool-group-row__head").trigger("click");
    await wrapper.get(".tool-summary-row__head").trigger("click");
    await vi.waitFor(() => expect(request).toHaveBeenCalledWith("agent-lazy", "agent-lazy", "round-lazy", "lazy-call", { revisionId: "revision-lazy", sourceItemId: "call-record" }));
    expect(wrapper.findComponent(ToolExecBubble).props("resultEntry").data.content).toBe("full result");
    await wrapper.get(".tool-group-row__head").trigger("click");
    expect(wrapper.find(".tool-exec-bubble").exists()).toBe(false);
    await wrapper.get(".tool-group-row__head").trigger("click");
    expect(wrapper.get(".tool-summary-row__head").attributes("aria-expanded")).toBe("true");
    await vi.waitFor(() => expect(request).toHaveBeenCalledTimes(2));
    expect(wrapper.findComponent(ToolExecBubble).props("resultEntry").data.content).toBe("full result");
    wrapper.unmount();
    request.mockRestore();
  });

  it("reloads expanded tool details when the committed result revision arrives", async () => {
    const request = vi.spyOn(nodeApi, "getConversationRoundToolStep")
      .mockResolvedValueOnce({ steps: [{
        tool_call_id: "live-call",
        call_entry: { kind: "tool_call", id: "tool-call/live-call", item_version: 1, detail_item_id: "call-record", data: { tool_call_id: "live-call", tool_name: "read_file", arguments: { path: "probe.txt" } } },
      }] })
      .mockResolvedValueOnce({ steps: [{
        tool_call_id: "live-call",
        call_entry: { kind: "tool_call", id: "tool-call/live-call", item_version: 1, detail_item_id: "call-record", data: { tool_call_id: "live-call", tool_name: "read_file", arguments: { path: "probe.txt" } } },
        result_entry: { kind: "tool_result", id: "result-live-call", item_version: 2, data: { tool_call_id: "live-call", content: "committed result" } },
      }] });
    const running = step("live-call", "read_file", "读取测试文件");
    Object.assign(running.callEntry, { id: "tool-call/live-call", item_version: 1, detail_item_id: "call-record", round_id: "round-live", round_revision_id: "revision-live", details_available: true });
    const wrapper = mount(ToolGroupRow, { props: { agentId: "agent-live", steps: [running] } });
    await wrapper.get(".tool-group-row__head").trigger("click");
    await wrapper.get(".tool-summary-row__head").trigger("click");
    await vi.waitFor(() => expect(request).toHaveBeenCalledTimes(1));

    const completed = {
      ...running,
      phase: "completed",
      resultEntry: { id: "result-live-call", item_id: "result-live-call", item_version: 2, detail_item_id: "result-record", round_id: "round-live", round_revision_id: "revision-live", details_available: true, kind: "tool_result", data: { tool_call_id: "live-call", content_bytes: 18 } },
    };
    await wrapper.setProps({ steps: [completed] });
    await vi.waitFor(() => expect(request).toHaveBeenCalledTimes(2));
    await vi.waitFor(() => expect(wrapper.findComponent(ToolExecBubble).props("resultEntry").data.content).toBe("committed result"));
    wrapper.unmount();
    request.mockRestore();
  });
});
