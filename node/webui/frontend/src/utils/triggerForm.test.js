import { describe, expect, it } from "vitest";
import {
  buildConditionFromForm,
  buildCreatePayload,
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

  it("builds weekly schedule with optional cmd", () => {
    const condition = buildConditionFromForm({
      scheduleKind: "weekly",
      hour: 9,
      minute: 30,
      weekday: 3,
      cmd: "test -f /tmp/ok",
    });
    expect(condition).toEqual({
      schedule: { kind: "weekly", hour: 9, minute: 30, weekday: 3 },
      cmd: "test -f /tmp/ok",
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
      scheduleKind: "daily",
      hour: 8,
      minute: 0,
    });
    expect(payload).toEqual({
      name: "日报",
      task_template: "汇总今日工作",
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

  it("converts datetime-local to unix seconds", () => {
    const ts = datetimeLocalToUnix("2026-07-08T09:30");
    expect(ts).toBeGreaterThan(0);
  });
});
