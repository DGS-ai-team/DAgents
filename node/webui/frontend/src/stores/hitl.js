import { reactive } from "vue";

export const hitlStore = reactive({
  queue: [],
  busy: false,
  /** 正在 resume 的队列项 index；-1 表示无。 */
  busyIndex: -1,
});

export function enqueueHitl(item) {
  if (item.kind === "approval") {
    const key = approvalQueueKey(item.data);
    hitlStore.queue = hitlStore.queue.filter(
      (q) => q.kind !== "approval" || approvalQueueKey(q.data) !== key,
    );
  }
  hitlStore.queue.push(item);
}

export function dequeueHitlAt(index) {
  if (index < 0 || index >= hitlStore.queue.length) return null;
  return hitlStore.queue.splice(index, 1)[0] || null;
}

export function getHitlAt(index) {
  if (index < 0 || index >= hitlStore.queue.length) return null;
  return hitlStore.queue[index] || null;
}

export function peekHitl() {
  return getHitlAt(0);
}

export function clearHitl() {
  hitlStore.queue = [];
  hitlStore.busy = false;
  hitlStore.busyIndex = -1;
}

/** 队列去重 / resume 路由用的稳定 key。 */
export function approvalQueueKey(data) {
  const child = String(data?.child_agent_id || "").trim();
  if (child) return `child:${child}`;
  const id = String(data?.approval_id || data?.hitl_id || "").trim();
  if (id) return `parent:${id}`;
  const calls = extractToolApprovals(data);
  const callIds = calls
    .map((c) => c.callId)
    .filter(Boolean)
    .sort()
    .join(",");
  if (callIds) return `calls:${callIds}`;
  return "parent:anonymous";
}

import { normalizeToolCallItem } from "../utils/toolCalls.js";

function attachApprovalRouting(data, resume) {
  const rv = { ...resume };
  if (data?.child_agent_id) rv.child_agent_id = data.child_agent_id;
  if (data?.approval_id) rv.approval_id = data.approval_id;
  return rv;
}

export function extractToolApprovals(data) {
  const calls = data?.approval_args?.tool_calls;
  if (!Array.isArray(calls)) return [];
  return calls
    .map((m) => {
      const norm = normalizeToolCallItem(m);
      const dup = m?.duplicate_meta && typeof m.duplicate_meta === "object" ? m.duplicate_meta : null;
      return {
        callId: norm.id,
        name: norm.name,
        rawArgs: norm.rawArguments,
        arguments: norm.arguments,
        reason: String(m?.approval_reason || ""),
        risk: String(m?.risk_level || "").toLowerCase(),
        duplicateWindowSec: dup ? Number(dup.window_seconds || dup.window_sec || 0) || 0 : 0,
        duplicatePreview: dup ? String(dup.result_preview || "").trim() : "",
      };
    })
    .filter((it) => it.callId);
}

export function buildApprovalSelectionResume(data, approvedByCallId) {
  const items = extractToolApprovals(data);
  if (!items.length) {
    return attachApprovalRouting(data, { type: "reject" });
  }
  const approved = [];
  const rejected = [];
  for (const it of items) {
    if (approvedByCallId[it.callId]) approved.push(it.callId);
    else rejected.push(it.callId);
  }
  return attachApprovalRouting(data, { type: "selection", approved, rejected });
}

/** 单条批准：仅执行所选工具，其余拒绝。单条拒绝：整批全部拒绝（不自动批准 siblings）。 */
export function buildApprovalOneResume(data, callId, approve) {
  const items = extractToolApprovals(data);
  if (!items.length) {
    return attachApprovalRouting(data, { type: "reject" });
  }
  if (approve) {
    const approved = items.filter((it) => it.callId === callId).map((it) => it.callId);
    const rejected = items.filter((it) => it.callId !== callId).map((it) => it.callId);
    return attachApprovalRouting(data, { type: "selection", approved, rejected });
  }
  const rejected = items.map((it) => it.callId);
  return attachApprovalRouting(data, { type: "selection", approved: [], rejected });
}

export function buildApprovalResume(data, { approveAll, approved = [], rejected = [] }) {
  const items = extractToolApprovals(data);
  const rv = { type: "selection", approved: [...approved], rejected: [...rejected] };
  if (approveAll === true) {
    rv.approved = items.map((it) => it.callId);
    rv.rejected = [];
  } else if (approveAll === false) {
    rv.approved = [];
    rv.rejected = items.map((it) => it.callId);
  }
  return attachApprovalRouting(data, rv);
}

export function extractUserInfo(data) {
  const args = data?.user_information_args;
  const src = args && typeof args === "object" ? args : data || {};
  const options = [];
  if (Array.isArray(src.options)) {
    for (const raw of src.options) {
      if (!raw || typeof raw !== "object") continue;
      const id = String(raw.id || "").trim();
      const label = String(raw.label || "").trim();
      if (!id || !label) continue;
      options.push({
        id,
        label,
        value: String(raw.value || label).trim() || label,
      });
    }
  }
  return {
    toolCallId: String(src.tool_call_id || data?.tool_call_id || "").trim(),
    question: String(src.question || data?.content || data?.message || "请提供信息").trim(),
    options,
    allowMultiple: !!src.allow_multiple,
    placeholder: String(src.placeholder || ""),
    required: src.required !== false,
  };
}

export function buildUserInfoResume(data, answer, selectedOptions = []) {
  const req = extractUserInfo(data);
  const rv = {
    type: "user_information",
    tool_call_id: req.toolCallId || data?.tool_call_id,
    answer: String(answer || "").trim(),
    selected_options: [...selectedOptions],
    cancelled: false,
  };
  if (data?.child_agent_id) rv.child_agent_id = data.child_agent_id;
  return rv;
}

/**
 * 把 UI 当前选中态解析为 option id 列表。
 * - 单选：selected 为 options 下标（Number）
 * - 多选：selected 为 option id 数组
 * 不在此静默回退到首项；由调用方决定 required 空选如何处理。
 */
export function resolveUserInfoSelectionIds(req, selected) {
  const options = Array.isArray(req?.options) ? req.options : [];
  if (!options.length) return [];
  if (req.allowMultiple) {
    if (!Array.isArray(selected)) return [];
    const allowed = new Set(options.map((o) => String(o.id)));
    return selected
      .map((id) => String(id || "").trim())
      .filter((id) => id && allowed.has(id));
  }
  // 单选只接受数字下标；[] / 对象等不能靠 Number([])===0 误解析成首项
  if (typeof selected !== "number" || !Number.isInteger(selected)) return [];
  if (selected < 0 || selected >= options.length) return [];
  const id = String(options[selected]?.id || "").trim();
  return id ? [id] : [];
}

export function buildUserInfoResumeFromSelection(data, selectedIds) {
  const req = extractUserInfo(data);
  const selected = new Set(selectedIds.map((id) => String(id).trim()).filter(Boolean));
  // answer 用 value（提交值），与 option id 区分；缺省时 extractUserInfo 已回落到 label
  const answers = req.options
    .filter((o) => selected.has(o.id))
    .map((o) => o.value || o.label);
  return buildUserInfoResume(data, answers.join(", "), [...selected].sort());
}

/**
 * 组装 user_information resume。
 * - 底部输入框有非空文本：以自由文本为准（忽略气泡已选选项），避免「发消息打断」却落成默认选项。
 * - 无文本且有选项：走选中态；单选空选可回落首项（与气泡默认高亮一致）。
 * - 无文本且无选项：自由文本路径（可能为空）。
 *
 * @returns {{ ok: true, resume: object } | { ok: false, error: string }}
 */
export function buildUserInfoSubmitResume(data, { text = "", selected = [] } = {}) {
  const req = extractUserInfo(data);
  const typed = String(text || "").trim();
  if (typed) {
    return { ok: true, resume: buildUserInfoResume(data, typed) };
  }
  if (req.options.length) {
    let selectedIds = resolveUserInfoSelectionIds(req, selected);
    if (!selectedIds.length && !req.allowMultiple) {
      selectedIds = resolveUserInfoSelectionIds(req, 0);
    }
    if (!selectedIds.length && req.required) {
      return { ok: false, error: "请先选择选项再提交" };
    }
    return { ok: true, resume: buildUserInfoResumeFromSelection(data, selectedIds) };
  }
  if (req.required) {
    return { ok: false, error: "请先输入回答再提交" };
  }
  return { ok: true, resume: buildUserInfoResume(data, "") };
}

export function extractMemoryConflict(data) {
  const meta = data?.memory_conflict_meta && typeof data.memory_conflict_meta === "object"
    ? data.memory_conflict_meta
    : {};
  const args = data?.user_information_args && typeof data.user_information_args === "object"
    ? data.user_information_args
    : {};
  return {
    toolCallId: String(data?.id || data?.tool_call_id || "").trim(),
    question: String(args.question || data?.content || meta.conflict_description || "检测到长期记忆冲突，请选择保留方式。").trim(),
    existing: String(meta.existing || "").trim(),
    newInformation: String(meta.new_information || "").trim(),
    mergedBoth: String(meta.merged_both || "").trim(),
    conflictDescription: String(meta.conflict_description || "").trim(),
  };
}

export function buildMemoryConflictResume(data, decision, { cancelled = false } = {}) {
  const req = extractMemoryConflict(data);
  return {
    type: "memory_conflict",
    tool_call_id: req.toolCallId || data?.id,
    decision: cancelled ? "cancelled" : String(decision || "").trim(),
    cancelled: !!cancelled,
  };
}

const HITL_TYPE_USER_INFORMATION = "user_information";
const HITL_TYPE_EXECUTE_TOOL = "execute_tool";
const HITL_TYPE_MEMORY_CONFLICT = "memory_conflict";

function hitlItemsFromData(raw) {
  if (!Array.isArray(raw)) return [];
  return raw.filter((item) => item && typeof item === "object");
}

function userInformationDataFromHITLItem(item) {
  const data = { display_type: "normal_text" };
  const content = String(item?.content || "").trim();
  if (content) data.content = content;
  if (item?.user_information_args && typeof item.user_information_args === "object") {
    data.user_information_args = item.user_information_args;
  }
  return data;
}

function memoryConflictDataFromHITLItem(item) {
  return {
    display_type: "normal_text",
    id: item?.id,
    content: item?.content,
    memory_conflict_meta: item?.memory_conflict_meta,
    user_information_args: item?.user_information_args,
  };
}

function hitlRoutingFieldsFromBatch(batch) {
  if (!batch || typeof batch !== "object") return {};
  const out = {};
  const childId = String(batch.child_agent_id || "").trim();
  if (childId) out.child_agent_id = childId;
  const scope = String(batch.hitl_scope || "").trim();
  if (scope) out.hitl_scope = scope;
  const purpose = String(batch.child_purpose || "").trim();
  if (purpose) out.child_purpose = purpose;
  return out;
}

function approvalDataFromHITLBatch(batch, executeItems) {
  if (!executeItems.length) return null;
  const data = {
    approval_type: "execute_tool",
    approval_args: { tool_calls: executeItems },
    display_type: "normal_text",
    ...hitlRoutingFieldsFromBatch(batch),
  };
  const hitlId = String(batch?.hitl_id || "").trim();
  if (hitlId) data.approval_id = hitlId;
  const message = String(batch?.message || "").trim();
  if (message) data.message = message;
  return data;
}

/** 将 hitl_required 展开为 Client 可入队的 user_information / approval 队列项。 */
export function expandHitlRequired(data) {
  const routing = hitlRoutingFieldsFromBatch(data);
  const userInfos = [];
  const memoryConflicts = [];
  const executeItems = [];
  for (const item of hitlItemsFromData(data?.items)) {
    const hitlType = String(item.hitl_type || "").trim();
    if (hitlType === HITL_TYPE_MEMORY_CONFLICT) {
      memoryConflicts.push({ ...memoryConflictDataFromHITLItem(item), ...routing });
    } else if (hitlType === HITL_TYPE_USER_INFORMATION) {
      userInfos.push({ ...userInformationDataFromHITLItem(item), ...routing });
    } else if (hitlType === HITL_TYPE_EXECUTE_TOOL) {
      executeItems.push(item);
    }
  }
  return {
    userInfos,
    memoryConflicts,
    approval: approvalDataFromHITLBatch(data, executeItems),
  };
}

/** 将 hitl_required / hydrate pending_hitl 展开并入队。 */
export function enqueueHitlRequired(data) {
  if (!data?.items?.length) {
    return { userInfos: [], memoryConflicts: [], approval: null };
  }
  const { userInfos, memoryConflicts, approval } = expandHitlRequired(data);
  for (const ui of userInfos) {
    enqueueHitl({ kind: "user_information", data: ui });
  }
  for (const mc of memoryConflicts) {
    enqueueHitl({ kind: "memory_conflict", data: mc });
  }
  if (approval) {
    enqueueHitl({ kind: "approval", data: approval });
  }
  hitlStore.busy = false;
  return { userInfos, memoryConflicts, approval };
}

/** 对齐 Go hitl.ShouldSkipChildRuntimeDisplay：隐藏子 Agent turn 的运行时 SSE。 */
export function shouldSkipChildRuntimeDisplay(eventType, data) {
  const childId = String(data?.child_agent_id || "").trim();
  if (!childId) return false;
  switch (eventType) {
    case "hitl_required":
    case "temporary_agent_created":
    case "temporary_agent_progress":
    case "temporary_agent_completed":
    case "temporary_agent_cancelled":
      return false;
    default:
      return true;
  }
}
