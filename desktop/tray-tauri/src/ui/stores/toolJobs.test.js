import { describe, expect, it, beforeEach } from "vitest";
import {
  canControlBashTool,
  bashControlMode,
  toolCallIdFromEntry,
  toolJobsStripText,
  applyToolJobsSnapshot,
  applyToolExecutions,
  toolJobsStore,
} from "./toolJobs.js";

describe("toolJobs", () => {
  beforeEach(() => {
    applyToolJobsSnapshot({ running: 0, running_call_ids: [] });
  });

  it("formats the synchronous execution statusline", () => {
    applyToolJobsSnapshot({ running: 2 });
    expect(toolJobsStripText()).toBe("2 执行中");
    applyToolJobsSnapshot({});
    expect(toolJobsStripText()).toBe("");
    expect(toolJobsStore.running).toBe(0);
  });

  it("only exposes the cancel control for a running bash call", () => {
    const call = { data: { tool_name: "bash_run", tool_call_id: "c1" } };
    expect(canControlBashTool({ callEntry: call, resultEntry: null })).toBe(false);

    applyToolJobsSnapshot({ running: 1, running_call_ids: ["c1"] });
    expect(bashControlMode({ callEntry: call, resultEntry: null })).toBe("running");

    applyToolJobsSnapshot({ running: 0, running_call_ids: [] });
    expect(bashControlMode({ callEntry: call, resultEntry: null })).toBe(null);
    expect(
      canControlBashTool({
        callEntry: { data: { tool_name: "read_file", tool_call_id: "c2" } },
        resultEntry: null,
      }),
    ).toBe(false);
  });

  it("reads tool call id from an entry", () => {
    expect(toolCallIdFromEntry({ blockId: "b1", data: { tool_call_id: "c1" } })).toBe("c1");
    expect(toolCallIdFromEntry({ blockId: "b1" })).toBe("b1");
  });

  it("derives cancellable executions from the authoritative turn projection", () => {
    applyToolExecutions([
      { id: "exec-1", tool_call_id: "c1", status: "running" },
      { id: "exec-2", tool_call_id: "c2", status: "completed" },
    ]);
    expect(toolJobsStore.running).toBe(1);
    expect(toolJobsStore.runningCallIds).toEqual(["c1"]);
  });
});
