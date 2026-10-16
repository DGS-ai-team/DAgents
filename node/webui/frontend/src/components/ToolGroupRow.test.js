// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import { mount } from "@vue/test-utils";
import ToolGroupRow from "./ToolGroupRow.vue";
import { buildStream } from "../composables/useStream.js";
import { applyToolJobsSnapshot } from "../stores/toolJobs.js";
import { resetTurnState, applyTurnState } from "../stores/turnState.js";

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
});
