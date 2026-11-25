import { extractToolApprovals } from "../stores/hitl.js";
import { toolCallIdFromEntry } from "../utils/toolIdentity.js";
import { resolveToolStepPhase } from "../utils/toolUserLabel.js";

function shouldSkipEntry(entry) {
  if (!entry) return true;
  // 思考内容仅流式时展示动态指示器，历史/落盘 reasoning 一律隐藏
  if (entry.kind === "reasoning") return !entry.streaming;
  if (
    (entry.kind === "assistant" || entry.kind === "user") &&
    !entry.text?.trim() &&
    !entry.streaming &&
    !(entry.kind === "user" && Array.isArray(entry.file_refs) && entry.file_refs.length)
  ) return true;
  return false;
}

function entryBlockId(entry) {
  return String(entry?.blockId || entry?.data?.tool_call_id || entry?.data?.id || "").trim();
}

function entryItemId(entry) {
  return String(entry?.item_id || entry?.id || "").trim();
}

function isSkippableBetweenTools(entry) {
  if (!entry) return true;
  if (entry.kind === "system") return true;
  // Summary-pagination markers are visual separators only. They must not
  // break the call/result association of the loaded tool rows around them.
  if (entry.kind === "tool_group_more") return true;
  // 多工具并行时，hydrate 顺序常为 callA, callB, resultA, resultB；
  // 扫描匹配 result 时允许越过其它 tool_call。
  if (entry.kind === "tool_call") return true;
  return shouldSkipEntry(entry);
}

function buildToolResultMatches(entries) {
  const matches = new Map();
  let calls = [];
  let results = new Map();

  const flushSegment = () => {
    for (const call of calls) {
      const blockId = entryBlockId(call.entry);
      const itemId = entryItemId(call.entry);
      const resultIdx = (itemId ? results.get(itemId) : undefined) ?? (blockId ? results.get(blockId) : undefined);
      if (resultIdx !== undefined && resultIdx > call.index) {
        matches.set(call.index, resultIdx);
      }
    }
    calls = [];
    results = new Map();
  };

  for (let index = 0; index < entries.length; index += 1) {
    const entry = entries[index];
    if (!entry) continue;
    if (!isSkippableBetweenTools(entry) && entry.kind !== "tool_result") {
      flushSegment();
      continue;
    }
    if (entry.kind === "tool_call") {
      calls.push({ entry, index });
    } else if (entry.kind === "tool_result") {
      const blockId = entryBlockId(entry);
      const itemId = String(entry?.data?.tool_call_item_id || "").trim();
      const resultKey = itemId || blockId;
      if (resultKey && !results.has(resultKey)) results.set(resultKey, index);
    }
  }
  flushSegment();
  return matches;
}

function awaitingApprovalCallIds(hitlQueue = []) {
  const ids = new Set();
  if (!Array.isArray(hitlQueue)) return ids;
  for (const hitl of hitlQueue) {
    if (hitl?.kind !== "approval") continue;
    for (const it of extractToolApprovals(hitl.data)) {
      const id = String(it?.callId || "").trim();
      if (id) ids.add(id);
    }
  }
  return ids;
}

/**
 * 将 transcript、HITL 和 coordinator 执行状态收敛为单一展示 phase。
 * 后端同批免审批工具并行执行：未完成的 final tool_call 一律 running；
 * 仅当 call 在 HITL 队列中时展示为 pending，并由审批卡独占呈现。
 */
export function annotateToolExecutionPhases(items, hitlQueue = [], authority = null) {
  if (!Array.isArray(items) || !items.length) return items;
  const toolSteps = items.filter((item) => item?.kind === "tool_step");
  if (!toolSteps.length) return items;

  const awaiting = awaitingApprovalCallIds(hitlQueue);
  const executionByCallId = new Map();
  for (const execution of Array.isArray(authority?.toolExecutions) ? authority.toolExecutions : []) {
    const id = String(execution?.toolCallId || "").trim();
    if (id && !executionByCallId.has(id)) executionByCallId.set(id, execution);
  }
  for (const item of toolSteps) {
    const id = toolCallIdFromEntry(item.callEntry) || toolCallIdFromEntry(item.resultEntry);
    const execution = id ? executionByCallId.get(id) : null;
    const status = String(execution?.status || "").trim().toLowerCase();
    const lifecycleStatus = ["succeeded", "completed", "failed", "error", "denied", "rejected", "cancelled", "canceled", "timed_out", "unknown"].includes(status)
      ? status
      : "";

    // Keep the coordinator's pending/running hint local to this projection.
    // UI consumers receive only the canonical phase, not two competing status
    // fields that each component has to reconcile again.
    let executionHint = null;
    if (!item.resultEntry && item.callEntry && !item.callEntry.partial) {
      if (["succeeded", "completed"].includes(status)) {
        executionHint = "settled";
      } else if (["failed", "error", "timed_out", "unknown"].includes(status)) {
        executionHint = "failed";
      } else if (["denied", "rejected"].includes(status)) {
        executionHint = "rejected";
      } else if (["cancelled", "canceled"].includes(status)) {
        executionHint = "cancelled";
      } else if (status === "running") {
        executionHint = "active";
      } else if (awaiting.has(id) || ["proposed", "pending"].includes(status)) {
        executionHint = "pending";
      } else if (authority?.authority === "turn_coordinator") {
        // A final call is an intent until execution starts. Do not display it
        // as running merely because its result has not reached the transcript.
        executionHint = "pending";
      }
    }

    item.phase = resolveToolStepPhase({
      callEntry: item.callEntry,
      resultEntry: item.resultEntry,
      lifecycleStatus,
      executionHint,
    });
  }
  return items;
}

/** 合并同 blockId 的 tool_call + tool_result 为 tool_step（F-UI6）。 */
export function buildStream(entries, hitlQueue = [], authority = null) {
  const items = [];
  const mergedResultIndices = new Set();
  const toolResultMatches = buildToolResultMatches(entries);

  for (let i = 0; i < entries.length; i += 1) {
    if (mergedResultIndices.has(i)) continue;
    const entry = entries[i];
    if (shouldSkipEntry(entry)) continue;

    if (entry.kind === "tool_call") {
      const blockId = entryBlockId(entry);
      const resultIdx = toolResultMatches.get(i) ?? -1;
      const itemId = entryItemId(entry) || blockId;
      if (resultIdx >= 0) {
        mergedResultIndices.add(resultIdx);
        items.push({
          key: `tool-step-${itemId}`,
          kind: "tool_step",
          callEntry: entry,
          resultEntry: entries[resultIdx],
        });
        continue;
      }
      items.push({
        key: `tool-step-${itemId}`,
        kind: "tool_step",
        callEntry: entry,
        resultEntry: null,
      });
      continue;
    }

    if (entry.kind === "tool_result") {
      items.push({
        key: `tool-result-${entryItemId(entry) || entryBlockId(entry)}`,
        kind: "tool_step",
        callEntry: null,
        resultEntry: entry,
      });
      continue;
    }

    items.push({ key: `e-${entry.id}`, kind: entry.kind, entry });
  }

  hitlQueue.forEach((hitl, idx) => {
    const hitlId = String(
      hitl?.data?.request_id || hitl?.data?.approval_id || hitl?.data?.id || "",
    ).trim();
    items.push({
      key: `hitl-${idx}-${hitl.kind}-${hitlId}`,
      kind: hitl.kind,
      hitl,
      hitlIndex: idx,
    });
  });

  const annotated = annotateToolExecutionPhases(items, hitlQueue, authority);
  // 待批工具由 ApprovalBubble 独占展示，避免 ToolSummaryRow「待执行」与审批卡双份。
  const awaiting = awaitingApprovalCallIds(hitlQueue);
  return annotated.filter(
    (item) => {
      if (item?.kind !== "tool_step" || item.phase !== "pending" || !item.callEntry || item.resultEntry) return true;
      const id = toolCallIdFromEntry(item.callEntry);
      return !awaiting.has(id);
    },
  );
}
