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
  const child = String(data?.child_session_id || "").trim();
  if (child) return `child:${child}`;
  const a2aTask = String(data?.a2a_task_id || "").trim();
  if (a2aTask) return `a2a:${a2aTask}`;
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

export function isA2ARelay(data) {
  return !!data?.a2a_relay;
}

export function a2aPeerLabel(data) {
  const name = String(data?.a2a_peer_agent_name || "").trim();
  if (name) return name;
  return String(data?.a2a_peer_agent_id || "").trim();
}

export function a2aRelaySuffix(data) {
  const label = a2aPeerLabel(data);
  return label ? ` from ${label}` : " from 对端 Agent";
}

export function a2aApprovedSummary(data, approved) {
  if (!approved) return "已拒绝";
  const label = a2aPeerLabel(data) || "对端 Agent";
  return `已审批，由${label}执行`;
}

import { normalizeToolCallItem } from "../utils/toolCalls.js";

function attachApprovalRouting(data, resume) {
  const rv = { ...resume };
  if (data?.child_session_id) rv.child_session_id = data.child_session_id;
  if (data?.approval_id) rv.approval_id = data.approval_id;
  if (data?.a2a_task_id) rv.a2a_task_id = data.a2a_task_id;
  return rv;
}

export function extractToolApprovals(data) {
  const calls = data?.approval_args?.tool_calls;
  if (!Array.isArray(calls)) return [];
  return calls
    .map((m) => {
      const norm = normalizeToolCallItem(m);
      return {
        callId: norm.id,
        name: norm.name,
        rawArgs: norm.rawArguments,
        arguments: norm.arguments,
        reason: String(m?.approval_reason || ""),
        risk: String(m?.risk_level || ""),
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
  if (data?.child_session_id) rv.child_session_id = data.child_session_id;
  return rv;
}

export function buildUserInfoResumeFromSelection(data, selectedIds) {
  const req = extractUserInfo(data);
  const selected = new Set(selectedIds.map((id) => String(id).trim()).filter(Boolean));
  const labels = req.options.filter((o) => selected.has(o.id)).map((o) => o.label);
  return buildUserInfoResume(data, labels.join(", "), [...selected].sort());
}

const HITL_TYPE_USER_INFORMATION = "user_information";
const HITL_TYPE_EXECUTE_TOOL = "execute_tool";

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

function hitlRoutingFieldsFromBatch(batch) {
  if (!batch || typeof batch !== "object") return {};
  const out = {};
  const childId = String(batch.child_session_id || "").trim();
  if (childId) out.child_session_id = childId;
  const scope = String(batch.hitl_scope || "").trim();
  if (scope) out.hitl_scope = scope;
  const purpose = String(batch.child_purpose || "").trim();
  if (purpose) out.child_purpose = purpose;
  const a2aTask = String(batch.a2a_task_id || "").trim();
  if (a2aTask) out.a2a_task_id = a2aTask;
  if (batch.a2a_relay) out.a2a_relay = true;
  const peerName = String(batch.a2a_peer_agent_name || "").trim();
  if (peerName) out.a2a_peer_agent_name = peerName;
  const peerId = String(batch.a2a_peer_agent_id || "").trim();
  if (peerId) out.a2a_peer_agent_id = peerId;
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
  const executeItems = [];
  for (const item of hitlItemsFromData(data?.items)) {
    const hitlType = String(item.hitl_type || "").trim();
    if (hitlType === HITL_TYPE_USER_INFORMATION) {
      userInfos.push({ ...userInformationDataFromHITLItem(item), ...routing });
    } else if (hitlType === HITL_TYPE_EXECUTE_TOOL) {
      executeItems.push(item);
    }
  }
  return {
    userInfos,
    approval: approvalDataFromHITLBatch(data, executeItems),
  };
}

/** 将 hydrate pending_a2a_relay 展开并入队（F-H4）。 */
export function enqueueA2ARelayPending(relay) {
  if (!relay?.event_type || !relay?.data) return;
  const data = { ...relay.data };
  if (relay.a2a_task_id && !data.a2a_task_id) data.a2a_task_id = relay.a2a_task_id;
  if (relay.a2a_relay && !data.a2a_relay) data.a2a_relay = true;
  switch (String(relay.event_type).trim()) {
    case "approval_required":
      enqueueHitl({ kind: "approval", data });
      break;
    case "user_information_required":
      enqueueHitl({ kind: "user_information", data });
      break;
    default:
      break;
  }
}

/** 将 hitl_required / hydrate pending_hitl 展开并入队。 */
export function enqueueHitlRequired(data) {
  if (!data?.items?.length) {
    return { userInfos: [], approval: null };
  }
  const { userInfos, approval } = expandHitlRequired(data);
  for (const ui of userInfos) {
    enqueueHitl({ kind: "user_information", data: ui });
  }
  if (approval) {
    enqueueHitl({ kind: "approval", data: approval });
  }
  hitlStore.busy = false;
  return { userInfos, approval };
}

/** 对齐 Go hitl.ShouldSkipChildRuntimeDisplay：隐藏子 Agent turn 的运行时 SSE。 */
export function shouldSkipChildRuntimeDisplay(eventType, data) {
  const childId = String(data?.child_session_id || "").trim();
  if (!childId) return false;
  switch (eventType) {
    case "approval_required":
    case "hitl_required":
    case "temporary_agent_created":
    case "temporary_agent_completed":
    case "temporary_agent_cancelled":
      return false;
    default:
      return true;
  }
}
