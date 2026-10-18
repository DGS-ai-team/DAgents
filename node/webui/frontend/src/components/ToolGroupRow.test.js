// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import { mount } from "@vue/test-utils";
import ToolGroupRow from "./ToolGroupRow.vue";
import { buildStream } from "../composables/useStream.js";
import { applyToolJobsSnapshot } from "../stores/toolJobs.js";
import { resetTurnState, applyTurnState } from "../stores/turnState.js";

function toolStep(id, phase, status = null) {
  const callEntry = {
    data: { tool_name: "bash_run", tool_call_id: id, arguments: '{"command":"true"}' },
  };
  const resultEntry = status
    ? { data: { tool_name: "bash_run", tool_call_id: id, status, content: "result" } }
    : null;
  return {
    key: id,
    callEntry,
    resultEntry,
    executionHint: phase === "hint" ? "failed" : phase === "active" ? "active" : phase,
  };
}

describe("ToolGroupRow terminal tool results", () => {
  beforeEach(() => {
    applyToolJobsSnapshot({ running: 0, running_call_ids: [] });
    resetTurnState();
  });

  it("shows the last failed bash result without stale running UI or controls", async () => {
    const steps = buildStream([
      {
        id: 1,
        kind: "tool_call",
        blockId: "read-first",
        data: {
          tool_name: "read_file",
          tool_call_id: "read-first",
          arguments: '{"path":"README.md"}',
        },
      },
      {
        id: 2,
        kind: "tool_result",
        blockId: "read-first",
        data: {
          tool_name: "read_file",
          tool_call_id: "read-first",
          status: "succeeded",
          content: "README content",
        },
      },
      {
        id: 3,
        kind: "tool_call",
        blockId: "bash-final",
        data: {
          tool_name: "bash_run",
          tool_call_id: "bash-final",
          arguments: '{"command":"exit 1"}',
        },
      },
      {
        id: 4,
        kind: "tool_result",
        blockId: "bash-final",
        data: {
          tool_name: "bash_run",
          tool_call_id: "bash-final",
          status: "failed",
          content: "[BASH_RESULT] status=FAILED\nexit_code=1",
        },
      },
    ]);
    expect(steps).toHaveLength(2);
    steps[1].executionHint = "active";
    applyToolJobsSnapshot({ running: 1, running_call_ids: ["bash-final"] });

    const wrapper = mount(ToolGroupRow, { props: { steps } });
    expect(wrapper.text()).not.toContain("项执行中");
    expect(wrapper.find(".tool-group-row__active").exists()).toBe(false);

    // Expand the completed batch to inspect the failed final command row.
    await wrapper.get(".tool-group-row__head").trigger("click");
    expect(wrapper.findAll(".tool-summary-row__status").map((node) => node.text())).toContain("执行失败");
    expect(wrapper.find(".tool-exec-spinner").exists()).toBe(false);
    expect(wrapper.find(".tool-summary-row__actions").exists()).toBe(false);
    wrapper.unmount();
  });

  it("uses authoritative failed execution before a result arrives despite stale running IDs", async () => {
    applyTurnState({
      authority: "turn_coordinator",
      phase: "tool_executing",
      turn_id: "turn-1",
      generation: 1,
      lifecycle_seq: 10,
      tool_executions: [{ id: "exec-bash", tool_call_id: "bash-pending-result", status: "failed" }],
    });
    applyToolJobsSnapshot({ running: 1, running_call_ids: ["bash-pending-result"] });

    const steps = buildStream([{
      id: 1,
      kind: "tool_call",
      blockId: "bash-pending-result",
      data: {
        tool_name: "bash_run",
        tool_call_id: "bash-pending-result",
        arguments: '{"command":"exit 1"}',
      },
    }]);
    expect(steps).toHaveLength(1);
    expect(steps[0].resultEntry).toBeNull();
    expect(steps[0].executionHint).toBe("failed");

    const wrapper = mount(ToolGroupRow, { props: { steps } });
    expect(wrapper.text()).not.toContain("项执行中");
    expect(wrapper.find(".tool-group-row__active").exists()).toBe(false);
    await wrapper.get(".tool-group-row__head").trigger("click");
    expect(wrapper.find(".tool-summary-row__status").text()).toBe("执行失败");
    expect(wrapper.find(".tool-exec-spinner").exists()).toBe(false);
    expect(wrapper.find(".tool-summary-row__actions").exists()).toBe(false);
    wrapper.unmount();
  });

  it("labels successful and failed results separately and marks only success with a green check", async () => {
    const wrapper = mount(ToolGroupRow, {
      props: { steps: [toolStep("ok", "completed", "succeeded"), toolStep("bad", "failed", "failed")] },
    });

    expect(wrapper.find(".tool-group-row__status").text()).toBe("1 项成功 · 1 项失败");
    await wrapper.get(".tool-group-row__head").trigger("click");
    const rows = wrapper.findAll(".tool-summary-row");
    expect(rows[0].find(".tool-summary-row__status-icon--success").exists()).toBe(true);
    expect(rows[1].find(".tool-summary-row__status-icon--danger").exists()).toBe(true);
    expect(rows[0].find(".tool-summary-row__status-icon--success path").attributes("d")).toContain("5 12");
    expect(rows[1].find(".tool-summary-row__status-icon--danger path").attributes("d")).toContain("6 6 12 12");
    expect(rows[1].find(".tool-summary-row__status").text()).toBe("执行失败");
    wrapper.unmount();
  });

  it("keeps the all-success group label", () => {
    const wrapper = mount(ToolGroupRow, {
      props: { steps: [toolStep("one", "completed", "succeeded"), toolStep("two", "completed", "succeeded")] },
    });
    expect(wrapper.find(".tool-group-row__status").text()).toBe("2 项已完成");
    expect(wrapper.find(".tool-group-row__status-icon--success").exists()).toBe(true);
    expect(wrapper.find(".tool-group-row__status-icon--success path").attributes("d")).toContain("5 12");
    wrapper.unmount();
  });

  it("counts terminal failure hints even before a result entry arrives", async () => {
    const wrapper = mount(ToolGroupRow, { props: { steps: [toolStep("hint-fail", "hint")] } });
    expect(wrapper.find(".tool-group-row__status").text()).toBe("1 项失败");
    await wrapper.get(".tool-group-row__head").trigger("click");
    expect(wrapper.find(".tool-summary-row__status").text()).toBe("执行失败");
    expect(wrapper.find(".tool-summary-row__status-icon--danger").exists()).toBe(true);
    wrapper.unmount();
  });

  it("shows pending approval without calling it active and keeps a neighboring failure visible", async () => {
    const pending = { ...toolStep("pending", "pending"), executionHint: "pending" };
    const wrapper = mount(ToolGroupRow, {
      props: { steps: [pending, toolStep("failed", "failed", "failed")] },
    });
    expect(wrapper.find(".tool-group-row__status").text()).toBe("1 项待批准 · 1 项失败");
    expect(wrapper.find(".brand-activity--icon-only").exists()).toBe(false);
    await wrapper.get(".tool-group-row__head").trigger("click");
    expect(wrapper.findAll(".tool-summary-row__status").map((node) => node.text())).toContain("待批准");
    expect(wrapper.findAll(".tool-summary-row__status").map((node) => node.text())).toContain("执行失败");
    wrapper.unmount();
  });

  it("keeps queued background work active instead of counting it as success", () => {
    const wrapper = mount(ToolGroupRow, {
      props: { steps: [toolStep("queued", "background", "queued")] },
    });
    expect(wrapper.find(".tool-group-row__status").text()).toBe("1 项后台执行中");
    expect(wrapper.find(".brand-activity--icon-only").exists()).toBe(true);
    expect(wrapper.find(".tool-group-row__status-icon--success").exists()).toBe(false);
    wrapper.unmount();
  });

  it("shows unknown idle state without a success mark", () => {
    const wrapper = mount(ToolGroupRow, { props: { steps: [{ key: "unknown" }] } });
    expect(wrapper.find(".tool-group-row__status").text()).toBe("1 项状态未知");
    expect(wrapper.find(".tool-group-row__status-icon--success").exists()).toBe(false);
    expect(wrapper.find(".tool-group-row__status-icon--muted").exists()).toBe(true);
    wrapper.unmount();
  });

  it.each([
    ["cancelled", "已终止", "warning", "1 项取消"],
    ["denied", "已拒绝", "danger", "1 项拒绝"],
    ["interrupted", "已中断", "warning", "1 项中断"],
  ])("does not mark %s as successful", async (status, expectedStatus, tone, groupStatus) => {
    const step = status === "interrupted"
      ? { ...toolStep(status, "interrupted"), callEntry: { data: { tool_name: "bash_run", interrupted: true } } }
      : toolStep(status, "completed", status);
    const wrapper = mount(ToolGroupRow, { props: { steps: [step] } });
    expect(wrapper.find(".tool-group-row__status").text()).toBe(groupStatus);
    expect(wrapper.find(".tool-group-row__status-icon--success").exists()).toBe(false);
    await wrapper.get(".tool-group-row__head").trigger("click");
    expect(wrapper.find(".tool-summary-row__status").text()).toBe(expectedStatus);
    expect(wrapper.find(`.tool-summary-row__status-icon--${tone}`).exists()).toBe(true);
    wrapper.unmount();
  });

  it("keeps an active indicator and exposes failures while another tool is running", () => {
    const wrapper = mount(ToolGroupRow, {
      props: { steps: [toolStep("still-running", "active"), toolStep("already-failed", "failed", "failed")] },
    });
    expect(wrapper.find(".tool-group-row__status").text()).toBe("1 项执行中 · 1 项失败");
    expect(wrapper.find(".brand-activity--icon-only").exists()).toBe(true);
    expect(wrapper.find(".tool-group-row__active .tool-exec-spinner").exists()).toBe(true);
    wrapper.unmount();
  });
});
