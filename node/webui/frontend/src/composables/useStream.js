import { extractToolApprovals } from "../stores/hitl.js";
import { toolCallIdFromEntry, toolJobsStore } from "../stores/toolJobs.js";
import { toolExecutionForCall, turnStateStore } from "../stores/turnState.js";

function shouldSkipEntry(entry) {
  if (!entry) return true;
  // 思考内容仅流式时展示动态指示器，历史/落盘 reasoning 一律隐藏
  if (entry.kind === "reasoning") return !entry.streaming;
  if ((entry.kind === "assistant" || entry.kind === "user") && !entry.text?.trim() && !entry.streaming) return true;
  // 兜底：过滤注入型 user（日期 / 异步回灌等），与后端 hydrate 跳过对齐
  if (entry.kind === "user") {
    const name = String(entry.name || entry.data?.name || "").trim();
    if (["date", "async_tool", "compression", "compression_sidecar", "tool_vision"].includes(name)) {
      return true;
    }
    if (String(entry.text || "").startsWith("当天日期为：")) return true;
  }
  return false;
}

function entryBlockId(entry) {
  return String(entry?.blockId || entry?.data?.tool_call_id || entry?.data?.id || "").trim();
}

function isSkippableBetweenTools(entry) {
  if (!entry) return true;
  if (entry.kind === "system") return true;
  // 多工具并行时，hydrate 顺序常为 callA, callB, resultA, resultB；
  // 扫描匹配 result 时允许越过其它 tool_call。
  if (entry.kind === "tool_call") return true;
  return shouldSkipEntry(entry);
}

function findMatchingToolResult(entries, startIdx, blockId) {
  const bid = String(blockId || "").trim();
  if (!bid) return -1;
  for (let j = startIdx + 1; j < entries.length; j += 1) {
    const next = entries[j];
    if (isSkippableBetweenTools(next)) continue;
    if (next.kind === "tool_result" && entryBlockId(next) === bid) {
      return j;
    }
    // 遇到其它非可跳过内容则停止（例如真正的 user/assistant 气泡）
    if (next.kind !== "tool_result") {
      return -1;
    }
  }
  return -1;
}

function buildToolResultMatches(entries) {
  const matches = new Map();
  let calls = [];
  let results = new Map();

  const flushSegment = () => {
    for (const call of calls) {
      const blockId = entryBlockId(call.entry);
      const resultIdx = blockId ? results.get(blockId) : undefined;
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
      if (blockId && !results.has(blockId)) results.set(blockId, index);
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
 * 为同批尚未出结果的 tool_step 标注 executionHint。
 * 后端同批免审批工具并行执行：未完成的 final tool_call 一律 active（执行中）。
 * 仅当该 call 已出现在 HITL 审批队列时标 pending（尚未开跑）。
 */
export function annotateToolExecutionHints(items, _jobs = toolJobsStore, hitlQueue = [], authority = turnStateStore) {
  if (!Array.isArray(items) || !items.length) return items;
  const unfinished = items.filter(
    (item) =>
      item?.kind === "tool_step" &&
      item.callEntry &&
      !item.resultEntry &&
      !item.callEntry.partial,
  );
  if (!unfinished.length) return items;

  const awaiting = awaitingApprovalCallIds(hitlQueue);
  for (const item of unfinished) {
    const id = toolCallIdFromEntry(item.callEntry);
    const execution = id
      ? (authority === turnStateStore ? toolExecutionForCall(id) : authority?.toolExecutions?.find((row) => row.toolCallId === id))
      : null;
    const status = String(execution?.status || "").trim().toLowerCase();
    if (["succeeded", "denied", "cancelled", "timed_out", "unknown"].includes(status)) {
      item.executionHint = status === "succeeded" ? "settled" : "failed";
    } else if (["running"].includes(status)) {
      item.executionHint = "active";
    } else if (awaiting.has(id) || ["proposed", "pending"].includes(status)) {
      item.executionHint = "pending";
    } else if (authority?.authority === "turn_coordinator") {
      // A final tool_call is an intent until the authoritative execution
      // projection says it has started. Do not label it as running merely
      // because the transcript has not received a result yet.
      item.executionHint = "pending";
    } else {
      // Legacy nodes have no execution projection; retain the old fallback.
      item.executionHint = "active";
    }
  }
  return items;
}

/** 合并同 blockId 的 tool_call + tool_result 为 tool_step（F-UI6）。 */
export function buildStream(entries, hitlQueue = [], jobs = toolJobsStore) {
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
      if (resultIdx >= 0) {
        mergedResultIndices.add(resultIdx);
        items.push({
          key: `tool-step-${blockId || entry.id}`,
          kind: "tool_step",
          callEntry: entry,
          resultEntry: entries[resultIdx],
        });
        continue;
      }
      items.push({
        key: `tool-call-${blockId || entry.id}`,
        kind: "tool_step",
        callEntry: entry,
        resultEntry: null,
      });
      continue;
    }

    if (entry.kind === "tool_result") {
      items.push({
        key: `tool-result-${entryBlockId(entry) || entry.id}`,
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

  const annotated = annotateToolExecutionHints(items, jobs, hitlQueue);
  // 待批工具由 ApprovalBubble 独占展示，避免 ToolSummaryRow「待执行」与审批卡双份。
  const awaiting = awaitingApprovalCallIds(hitlQueue);
  return annotated.filter(
    (item) => {
      if (item?.kind !== "tool_step" || item.executionHint !== "pending") return true;
      const id = toolCallIdFromEntry(item.callEntry);
      return !awaiting.has(id);
    },
  );
}
