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

function namedStep(id, name, phase, purpose = "", status = null) {
  const callEntry = {
    data: {
      tool_name: name,
      tool_call_id: id,
      arguments: JSON.stringify({ call_purpose: purpose }),
    },
  };
  return {
    key: id,
    callEntry,
    resultEntry: status ? { data: { tool_name: name, tool_call_id: id, status } } : null,
    executionHint: phase === "completed" ? "settled" : phase,
  };
}

describe("ToolGroupRow terminal tool results", () => {
  beforeEach(() => {
    applyToolJobsSnapshot({ running: 0, running_call_ids: [] });
    resetTurnState();
  });

  it("selects the newest foreground tool and its icon over background and pending steps", () => {
    const wrapper = mount(ToolGroupRow, {
      props: {
        steps: [
          namedStep("bg", "bash_run", "background", "后台命令", "queued"),
          namedStep("pending", "browser_navigate", "pending", "待审批"),
          namedStep("run-old", "terminal_read", "running", "旧执行"),
          namedStep("run-new", "read_file", "generating", "读取配置"),
        ],
      },
    });
    expect(wrapper.find(".tool-group-row__title").text()).toBe("read_file(读取配置)");
    expect(wrapper.find(".tool-group-row").classes()).toContain("tool-group-row--fs");
    expect(wrapper.find(".tool-group-row__title--active").exists()).toBe(true);
    expect(wrapper.find(".tool-group-row__status").text()).toBe("2 项执行中 · 1 项后台执行中 · 1 项待批准");
    wrapper.unmount();
  });

  it("selects pending approval before background work, then the newest background step", async () => {
    const steps = [
      namedStep("pending-old", "browser_navigate", "pending", "旧待批"),
      namedStep("bg-old", "bash_run", "background", "早期后台", "queued"),
      namedStep("bg-new", "terminal_read", "background", "最新后台", "queued"),
    ];
    const wrapper = mount(ToolGroupRow, { props: { steps } });
    expect(wrapper.find(".tool-group-row__title").text()).toBe("browser_navigate(旧待批)");
    expect(wrapper.find(".tool-group-row__title--active").exists()).toBe(false);
    await wrapper.setProps({ steps: steps.slice(1) });
    expect(wrapper.find(".tool-group-row__title").text()).toBe("terminal_read(最新后台)");
    expect(wrapper.find(".tool-group-row__title--active").exists()).toBe(false);
    wrapper.unmount();
  });

  it("keeps settled results ahead of stale active hints and falls back to the last step", () => {
    const settled = namedStep("done", "bash_run", "active", "已完成", "succeeded");
    const wrapper = mount(ToolGroupRow, {
      props: { steps: [namedStep("earlier", "read_file", "completed", "先前"), settled] },
    });
    expect(wrapper.find(".tool-group-row__title").text()).toBe("bash(已完成)");
    expect(wrapper.find(".tool-group-row__title--active").exists()).toBe(false);
    expect(wrapper.find(".tool-group-row__status").text()).toBe("2 项已完成");
    wrapper.unmount();
  });

  it("slides purpose independently for the same name, replaces the whole title for a new name, and preserves expansion on rapid updates", async () => {
    const wrapper = mount(ToolGroupRow, {
      props: { steps: [namedStep("one", "read_file", "running", "读取一")] },
      global: { stubs: { Transition: true } },
    });
    const head = wrapper.get(".tool-group-row__head");
    await head.trigger("click");
    const titleNode = wrapper.get(".tool-group-row__title-name").element;
    const purposeNode = wrapper.get(".tool-group-row__title-purpose").element;
    await wrapper.setProps({ steps: [namedStep("one", "read_file", "running", "读取二")] });
    expect(wrapper.get(".tool-group-row__title-name").element).toBe(titleNode);
    expect(wrapper.get(".tool-group-row__title-purpose").element).not.toBe(purposeNode);
    expect(wrapper.find(".tool-group-row__title").text()).toContain("读取二");
    await wrapper.setProps({ steps: [namedStep("one", "bash_run", "running", "执行命令")] });
    expect(wrapper.find(".tool-group-row__title").text()).toContain("bash(执行命令)");
    expect(wrapper.get(".tool-group-row__title-name").element).not.toBe(titleNode);
    await wrapper.setProps({ steps: [namedStep("one", "read_file", "running", "读取三")] });
    expect(wrapper.get(".tool-group-row__head").attributes("aria-expanded")).toBe("true");
    expect(wrapper.findAll(".tool-summary-row")).toHaveLength(1);
    wrapper.unmount();
  });

  it("handles missing purpose and keeps long nested summaries within the title region", () => {
    const longPurpose = `检查${"非常长的目录结构/".repeat(30)}`;
    const empty = namedStep("empty", "read_file", "completed");
    const wrapper = mount(ToolGroupRow, {
      props: {
        steps: [
          empty,
          namedStep("long", "mcp__docs__read", "completed", longPurpose),
        ],
      },
    });
    expect(wrapper.find(".tool-group-row__title").text()).toMatch(/^mcp__docs__read\(检查/);
    expect(wrapper.find(".tool-group-row__title-purpose").exists()).toBe(true);
    expect(wrapper.find(".tool-group-row__title").attributes("title")).toBe(wrapper.find(".tool-group-row__title").text());
    expect(wrapper.find(".tool-group-row__title--active").exists()).toBe(false);
    wrapper.setProps({ steps: [empty] });
    return wrapper.vm.$nextTick().then(() => {
      expect(wrapper.find(".tool-group-row__title").text()).toBe("read_file");
      expect(wrapper.find(".tool-group-row__title-purpose").exists()).toBe(false);
      wrapper.unmount();
    });
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
    expect(wrapper.find(".tool-group-row__activity-dot").exists()).toBe(false);
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
    expect(wrapper.find(".tool-group-row__activity-dot").exists()).toBe(true);
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
    expect(wrapper.find(".tool-group-row__activity-dot").exists()).toBe(true);
    expect(wrapper.find(".tool-group-row__active .tool-exec-spinner").exists()).toBe(true);
    wrapper.unmount();
  });
});
