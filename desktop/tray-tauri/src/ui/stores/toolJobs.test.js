import { describe, expect, it, beforeEach } from "vitest";
import {
  toolCallIdFromEntry,
  applyToolJobsSnapshot,
  applyToolExecutions,
  toolJobsStore,
} from "./toolJobs.js";

describe("toolJobs", () => {
  beforeEach(() => {
    applyToolJobsSnapshot({ running_call_ids: [] });
  });

  it("reads tool call id from an entry", () => {
    expect(toolCallIdFromEntry({ blockId: "b1", data: { tool_call_id: "c1" } })).toBe("c1");
    expect(toolCallIdFromEntry({ blockId: "b1" })).toBe("b1");
  });

  it("derives running call IDs from the authoritative turn projection", () => {
    applyToolExecutions([
      { id: "exec-1", tool_call_id: "c1", status: "running" },
      { id: "exec-2", tool_call_id: "c2", status: "completed" },
    ]);
    expect(toolJobsStore.runningCallIds).toEqual(["c1"]);
  });
});
