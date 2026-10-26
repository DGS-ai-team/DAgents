import { describe, expect, it } from "vitest";
import {
  buildConditionFromForm,
  buildCreatePayload,
  buildUpdatePayload,
  buildTriggerPatch,
  datetimeLocalToUnix,
  parseConditionToForm,
  triggerToForm,
  validateTriggerForm,
} from "./triggerForm.js";

describe("triggerForm", () => {
  it("parses interval condition", () => {
    expect(parseConditionToForm({ interval_seconds: 120 })).toMatchObject({
      scheduleKind: "interval",
      intervalSeconds: 120,
    });
  });

  it("does not emit legacy shell checks", () => {
    const condition = buildConditionFromForm({
      scheduleKind: "weekly",
      hour: 9,
      minute: 30,
      weekday: 3,
      cmd: "test -f /tmp/ok",
    });
    expect(condition).toEqual({
      schedule: { kind: "weekly", hour: 9, minute: 30, weekday: 3 },
    });
  });

  it("validates required fields", () => {
    expect(validateTriggerForm({ name: "", taskTemplate: "x", scheduleKind: "interval", intervalSeconds: 60 })).toBe(
      "请填写名称",
    );
    expect(validateTriggerForm({ name: "a", taskTemplate: "", scheduleKind: "interval", intervalSeconds: 60 })).toBe(
      "请填写任务模板",
    );
  });

  it("builds create payload", () => {
    const payload = buildCreatePayload({
      name: "日报",
      taskTemplate: "汇总今日工作",
      targetAgentId: "agent-1",
      sessionTargetMode: "fixed",
      scheduleKind: "daily",
      hour: 8,
      minute: 0,
    });
    expect(payload).toEqual({
      name: "日报",
      task_template: "汇总今日工作",
      target_agent_id: "agent-1",
      session_target_mode: "fixed",
      enabled: true,
      condition: { schedule: { kind: "daily", hour: 8, minute: 0 } },
    });
  });

  it("round-trips trigger definition into form", () => {
    const form = triggerToForm({
      name: "心跳",
      enabled: false,
      task_template: "ping",
      condition: { interval_seconds: 300 },
    });
    expect(form.name).toBe("心跳");
    expect(form.enabled).toBe(false);
    expect(form.taskTemplate).toBe("ping");
    expect(form.scheduleKind).toBe("interval");
    expect(form.intervalSeconds).toBe(300);
  });

  it("keeps schedule changes in the update payload", () => {
    const previous = { name: "日报", taskTemplate: "汇总", targetAgentId: "a", sessionTargetMode: "fixed", scheduleKind: "daily", hour: 8, minute: 0 };
    const next = { ...previous, scheduleKind: "interval", intervalSeconds: 900 };
    expect(buildUpdatePayload(next).condition).toEqual({ interval_seconds: 900 });
    expect(buildUpdatePayload(next).condition).not.toEqual(buildUpdatePayload(previous).condition);
  });

  it("builds minimal trigger patches and clears an old session binding", () => {
    const previous = { name: "日报", taskTemplate: "汇总", targetAgentId: "a", sessionTargetMode: "fixed", scheduleKind: "daily", hour: 8, minute: 0, enabled: true };
    expect(buildTriggerPatch(previous, { ...previous, name: "日报2" })).toEqual({ name: "日报2" });
    expect(buildTriggerPatch(previous, { ...previous, scheduleKind: "interval", intervalSeconds: 900 })).toMatchObject({ condition: { interval_seconds: 900 } });
    expect(buildTriggerPatch(previous, { ...previous, targetAgentId: "b" })).toMatchObject({ target_agent_id: "b", target_session_id: "" });
    expect(buildTriggerPatch(previous, { ...previous })).toEqual({});
  });

  it("sends disabled state atomically when creating", () => {
    expect(buildCreatePayload({ name: "x", taskTemplate: "y", targetAgentId: "a", enabled: false, scheduleKind: "interval", intervalSeconds: 60 }).enabled).toBe(false);
  });

  it("converts datetime-local to unix seconds", () => {
    const ts = datetimeLocalToUnix("2026-07-08T09:30");
    expect(ts).toBeGreaterThan(0);
  });
});
