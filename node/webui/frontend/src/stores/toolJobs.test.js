import { describe, expect, it } from "vitest";
import {
  canControlBashTool,
  toolCallIdFromEntry,
  toolJobsStripText,
  applyToolJobsSnapshot,
  toolJobsStore,
} from "./toolJobs.js";

describe("toolJobs", () => {
  it("formats statusline strip", () => {
    applyToolJobsSnapshot({ running: 2, background: 1 });
    expect(toolJobsStripText()).toBe("2 执行中 · 1 后台");
    applyToolJobsSnapshot({ running: 0, background: 3 });
    expect(toolJobsStripText()).toBe("3 后台");
    applyToolJobsSnapshot({});
    expect(toolJobsStripText()).toBe("");
    expect(toolJobsStore.running).toBe(0);
  });

  it("only exposes bash controls for in-progress bash_run", () => {
    expect(
      canControlBashTool({
        callEntry: { data: { tool_name: "bash_run", tool_call_id: "c1" } },
        resultEntry: null,
      }),
    ).toBe(true);
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
    expect(
      canControlBashTool({
        callEntry: { data: { tool_name: "bash_run", tool_call_id: "c4" } },
        resultEntry: { data: { content: "done" } },
      }),
    ).toBe(false);
  });

  it("reads tool call id from entry", () => {
    expect(toolCallIdFromEntry({ blockId: "b1", data: { tool_call_id: "c1" } })).toBe("c1");
    expect(toolCallIdFromEntry({ blockId: "b1" })).toBe("b1");
  });
});
