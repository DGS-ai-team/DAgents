import { describe, expect, it } from "vitest";
import {
  childAgentItems,
  formatChildAgentStatus,
  isChildAgentActive,
  sortChildAgentItems,
} from "./childAgent.js";

describe("childAgent", () => {
  it("isChildAgentActive treats terminal statuses as inactive", () => {
    expect(isChildAgentActive("active")).toBe(true);
    expect(isChildAgentActive("creating")).toBe(true);
    expect(isChildAgentActive("completed")).toBe(false);
    expect(isChildAgentActive("cancelled")).toBe(false);
  });

  it("formatChildAgentStatus returns Chinese labels", () => {
    expect(formatChildAgentStatus("active")).toBe("运行中");
    expect(formatChildAgentStatus("cancelled")).toBe("已取消");
  });

  it("sortChildAgentItems puts active items first", () => {
    const sorted = sortChildAgentItems([
      { child_session_id: "a", status: "completed", created_at: "2026-01-02T00:00:00Z" },
      { child_session_id: "b", status: "active", created_at: "2026-01-01T00:00:00Z" },
    ]);
    expect(sorted[0].child_session_id).toBe("b");
  });

  it("childAgentItems reads items array", () => {
    expect(childAgentItems({ items: [{ child_session_id: "x" }] })).toHaveLength(1);
    expect(childAgentItems(null)).toEqual([]);
  });
});
