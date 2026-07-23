import { describe, expect, it, beforeEach } from "vitest";
import {
  canControlBashTool,
  canBackgroundBashTool,
  bashControlMode,
  toolCallIdFromEntry,
  toolJobsStripText,
  applyToolJobsSnapshot,
  toolJobsStore,
  parseBashResultStatus,
  isBashBackgroundRunning,
} from "./toolJobs.js";

describe("toolJobs", () => {
  beforeEach(() => {
    applyToolJobsSnapshot({ running: 0, background: 0, running_call_ids: [], background_call_ids: [] });
  });

  it("formats statusline strip", () => {
    applyToolJobsSnapshot({ running: 2, background: 1 });
    expect(toolJobsStripText()).toBe("2 执行中 · 1 后台");
    applyToolJobsSnapshot({ running: 0, background: 3 });
    expect(toolJobsStripText()).toBe("3 后台");
    applyToolJobsSnapshot({});
    expect(toolJobsStripText()).toBe("");
    expect(toolJobsStore.running).toBe(0);
  });

  it("only exposes bash controls while sync-running or background-running", () => {
    const call = { data: { tool_name: "bash_run", tool_call_id: "c1" } };
    expect(canControlBashTool({ callEntry: call, resultEntry: null })).toBe(false);

    applyToolJobsSnapshot({ running: 1, running_call_ids: ["c1"] });
    expect(bashControlMode({ callEntry: call, resultEntry: null })).toBe("running");
    expect(canBackgroundBashTool({ callEntry: call, resultEntry: null })).toBe(true);

    applyToolJobsSnapshot({ running: 0, background: 1, background_call_ids: ["c1"] });
    expect(bashControlMode({ callEntry: call, resultEntry: null })).toBe("background");
    expect(canBackgroundBashTool({ callEntry: call, resultEntry: null })).toBe(false);

    expect(
      canControlBashTool({
        callEntry: { data: { tool_name: "read_file", tool_call_id: "c2" } },
        resultEntry: null,
      }),
    ).toBe(false);
    expect(
      canControlBashTool({
        callEntry: { data: { tool_name: "bash_run", tool_call_id: "c3" }, partial: true },
        resultEntry: null,
      }),
    ).toBe(false);
  });

  it("keeps cancel for RUNNING bash after auto-background via call id list", () => {
    const call = { data: { tool_name: "bash_run", tool_call_id: "c9" } };
    const result = {
      data: {
        tool_name: "bash_run",
        tool_call_id: "c9",
        content: "[BASH_RESULT] status=RUNNING job_id=j1\nshell_type=bash",
      },
    };
    expect(parseBashResultStatus(result.data.content)).toBe("RUNNING");
    expect(isBashBackgroundRunning(result)).toBe(true);
    expect(bashControlMode({ callEntry: call, resultEntry: result })).toBe(null);
    applyToolJobsSnapshot({ background: 1, background_call_ids: ["c9"] });
    expect(bashControlMode({ callEntry: call, resultEntry: result })).toBe("background");
    expect(canBackgroundBashTool({ callEntry: call, resultEntry: result })).toBe(false);
  });

  it("reads tool call id from entry", () => {
    expect(toolCallIdFromEntry({ blockId: "b1", data: { tool_call_id: "c1" } })).toBe("c1");
    expect(toolCallIdFromEntry({ blockId: "b1" })).toBe("b1");
  });
});
