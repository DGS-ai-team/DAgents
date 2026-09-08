import { describe, expect, it } from "vitest";
import { GOAL_TEMPLATES, goalBudgetRemaining, goalReasonLabel, goalStatusLabel, goalStatusReason, goalNextWakeLabel } from "./goalPresentation.js";

describe("goal presentation", () => {
  it("provides the three supported creation templates", () => {
    expect(Object.keys(GOAL_TEMPLATES)).toEqual(["observe", "batch", "followup"]);
  });
  it("does not show used tokens as remaining budget", () => {
    expect(goalBudgetRemaining({ token_budget: 100, tokens_used: 35 })).toBe(65);
    expect(goalStatusReason({ status_reason: "等待审批" })).toBe("等待审批");
  });
  it("does not show stale approval on terminal goals", () => {
    expect(goalReasonLabel({ status: "completed", status_reason: "approval_required" })).toBe("");
    expect(goalReasonLabel({ status: "stopped", status_reason: "approval_required" })).toBe("");
    expect(goalStatusLabel({ status: "waiting", status_reason: "approval_required" })).toBe("等待审批");
    expect(goalStatusLabel({ status: "waiting", status_reason: "next_wake" })).toBe("等待下次唤醒");
    expect(goalNextWakeLabel({ status: "completed", next_wake_at: "2030-01-01T00:00:00Z" })).toBe("不会再运行");
    expect(goalNextWakeLabel({ status: "stopped", next_wake_at: "2030-01-01T00:00:00Z" })).toBe("不会再运行");
  });
});
