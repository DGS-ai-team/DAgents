/** 触发器表单 ↔ API condition 互转。 */

export const TRIGGER_SCHEDULE_OPTIONS = [
  { value: "interval", label: "固定间隔" },
  { value: "once", label: "单次" },
  { value: "daily", label: "每天" },
  { value: "weekly", label: "每周" },
  { value: "monthly", label: "每月" },
];

export const WEEKDAY_OPTIONS = [
  { value: 0, label: "周日" },
  { value: 1, label: "周一" },
  { value: 2, label: "周二" },
  { value: 3, label: "周三" },
  { value: 4, label: "周四" },
  { value: 5, label: "周五" },
  { value: 6, label: "周六" },
];

function intFromAny(v) {
  const n = Number(v);
  return Number.isFinite(n) ? Math.floor(n) : 0;
}

function floatFromAny(v) {
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}

function pad2(n) {
  return String(n).padStart(2, "0");
}

/** @returns {ReturnType<typeof defaultTriggerForm>} */
export function defaultTriggerForm() {
  const now = new Date();
  now.setMinutes(now.getMinutes() + 5, 0, 0);
  return {
    name: "",
    enabled: true,
    scheduleKind: "interval",
    intervalSeconds: 3600,
    fireAtLocal: datetimeLocalFromDate(now),
    hour: 9,
    minute: 0,
    weekday: 1,
    day: 1,
    cmd: "",
    taskTemplate: "",
  };
}

export function datetimeLocalFromDate(d) {
  if (!(d instanceof Date) || Number.isNaN(d.getTime())) return "";
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}T${pad2(d.getHours())}:${pad2(d.getMinutes())}`;
}

export function unixToDatetimeLocal(ts) {
  const n = floatFromAny(ts);
  if (n <= 0) return "";
  return datetimeLocalFromDate(new Date(n * 1000));
}

export function datetimeLocalToUnix(value) {
  const s = String(value || "").trim();
  if (!s) return 0;
  const ms = new Date(s).getTime();
  return Number.isFinite(ms) ? Math.floor(ms / 1000) : 0;
}

export function isCalendarScheduleKind(kind) {
  return kind === "daily" || kind === "weekly" || kind === "monthly";
}

/** 从 API trigger 或 condition 解析为表单字段。 */
export function triggerToForm(trigger) {
  const form = defaultTriggerForm();
  if (!trigger || typeof trigger !== "object") return form;

  form.name = String(trigger.name || "");
  form.enabled = trigger.enabled !== false;
  form.taskTemplate = String(trigger.task_template || "");

  const parsed = parseConditionToForm(trigger.condition);
  return { ...form, ...parsed };
}

/** 仅解析 condition → 调度相关字段。 */
export function parseConditionToForm(condition) {
  const base = {
    scheduleKind: "interval",
    intervalSeconds: 3600,
    fireAtLocal: defaultTriggerForm().fireAtLocal,
    hour: 9,
    minute: 0,
    weekday: 1,
    day: 1,
    cmd: "",
  };
  if (!condition || typeof condition !== "object") return base;

  const interval = intFromAny(condition.interval_seconds);
  if (interval > 0) {
    return { ...base, scheduleKind: "interval", intervalSeconds: interval, cmd: "" };
  }

  const fireAt = floatFromAny(condition.fire_at);
  if (fireAt > 0) {
    return {
      ...base,
      scheduleKind: "once",
      fireAtLocal: unixToDatetimeLocal(fireAt),
      cmd: "",
    };
  }

  const sched = condition.schedule;
  if (sched && typeof sched === "object" && Object.keys(sched).length > 0) {
    const kind = String(sched.kind || "daily").trim().toLowerCase();
    const scheduleKind = TRIGGER_SCHEDULE_OPTIONS.some((o) => o.value === kind) ? kind : "daily";
    return {
      ...base,
      scheduleKind,
      hour: intFromAny(sched.hour),
      minute: intFromAny(sched.minute),
      weekday: intFromAny(sched.weekday),
      day: intFromAny(sched.day) || 1,
      cmd: String(condition.cmd || "").trim(),
    };
  }

  return base;
}

/** 表单 → API condition。 */
export function buildConditionFromForm(form) {
  const kind = String(form?.scheduleKind || "interval");
  const condition = {};

  switch (kind) {
    case "interval": {
      condition.interval_seconds = intFromAny(form.intervalSeconds);
      break;
    }
    case "once": {
      condition.fire_at = datetimeLocalToUnix(form.fireAtLocal);
      break;
    }
    case "daily":
    case "weekly":
    case "monthly": {
      const schedule = {
        kind,
        hour: intFromAny(form.hour),
        minute: intFromAny(form.minute),
      };
      if (kind === "weekly") schedule.weekday = intFromAny(form.weekday);
      if (kind === "monthly") schedule.day = intFromAny(form.day);
      condition.schedule = schedule;
      const cmd = String(form.cmd || "").trim();
      if (cmd) condition.cmd = cmd;
      break;
    }
    default:
      break;
  }

  return condition;
}

/** @returns {string|null} */
export function validateTriggerForm(form) {
  const name = String(form?.name || "").trim();
  if (!name) return "请填写名称";

  const taskTemplate = String(form?.taskTemplate || "").trim();
  if (!taskTemplate) return "请填写任务模板";

  const kind = String(form?.scheduleKind || "interval");
  if (kind === "interval") {
    const sec = intFromAny(form.intervalSeconds);
    if (sec < 1) return "间隔须至少 1 秒";
    return null;
  }
  if (kind === "once") {
    const ts = datetimeLocalToUnix(form.fireAtLocal);
    if (ts <= 0) return "请选择有效的执行时间";
    return null;
  }
  if (isCalendarScheduleKind(kind)) {
    const hour = intFromAny(form.hour);
    const minute = intFromAny(form.minute);
    if (hour < 0 || hour > 23) return "小时须在 0–23 之间";
    if (minute < 0 || minute > 59) return "分钟须在 0–59 之间";
    if (kind === "weekly") {
      const weekday = intFromAny(form.weekday);
      if (weekday < 0 || weekday > 6) return "请选择星期";
    }
    if (kind === "monthly") {
      const day = intFromAny(form.day);
      if (day === 0 || day < -31 || day > 31) return "日期须为 1–31，或负数表示倒数（如 -1=最后一天）";
    }
    return null;
  }
  return "未知调度类型";
}

/** 表单 → POST /v1/triggers body。 */
export function buildCreatePayload(form) {
  return {
    name: String(form.name || "").trim(),
    task_template: String(form.taskTemplate || "").trim(),
    condition: buildConditionFromForm(form),
  };
}

/** 表单 → PATCH /v1/triggers/{id} body。 */
export function buildUpdatePayload(form) {
  return {
    name: String(form.name || "").trim(),
    task_template: String(form.taskTemplate || "").trim(),
    condition: buildConditionFromForm(form),
    enabled: !!form.enabled,
  };
}
