import { reactive } from "vue";
import {
  extractToolCallsFromEvent,
  toolCallParts,
  toolDisplayName,
  toolIndexFromEvent,
  resolveToolArgumentsFromData,
  USER_INFORMATION_TOOL,
} from "../utils/toolCalls.js";
import {
  forgetToolBlock,
  markToolBlockActive,
  resolveToolBlockId,
} from "./toolStream.js";
import {
  configureStreamReveal,
  flushReveal,
  markRevealStreaming,
  resetStreamReveal,
  scheduleReveal,
} from "./streamReveal.js";

let idSeq = 0;

export const transcriptStore = reactive({
  entries: [],
  lastSeq: 0,
  assistantBuffer: "",
  reasoningBuffer: "",
  showReasoning: false,
  toolFoldVerbose: false,
});

configureStreamReveal({
  getSourceText(kind) {
    if (kind === "assistant") return transcriptStore.assistantBuffer;
    if (kind === "reasoning") return transcriptStore.reasoningBuffer;
    return "";
  },
  onRevealText(kind, text, _flushed) {
    upsertStreaming(kind, text);
  },
});

export function hasStreamingKind(kind) {
  return transcriptStore.entries.some((e) => e.streaming && e.kind === kind);
}

export function hasStreamingTextContent() {
  return hasStreamingKind("assistant") || hasStreamingKind("reasoning");
}

export function noteSeq(seq) {
  if (seq > transcriptStore.lastSeq) transcriptStore.lastSeq = seq;
}

export function addUser(text, images = []) {
  abortStreaming();
  transcriptStore.entries.push({
    id: ++idSeq,
    kind: "user",
    text,
    images: Array.isArray(images) ? images.filter(Boolean) : [],
  });
}

export function addDeferredUser(text, userName = "", sideEffectSeq = 0) {
  abortStreaming();
  transcriptStore.entries.push({
    id: ++idSeq,
    kind: "user_deferred",
    text,
    userName: String(userName || "").trim(),
    sideEffectSeq: Number(sideEffectSeq) || 0,
    sideEffectApplied: false,
    sideEffectStale: false,
  });
}

function sideEffectSeqFromData(data) {
  const raw = data?.side_effect_seq;
  const n = Number(raw);
  return Number.isFinite(n) && n > 0 ? n : 0;
}

export function markSideEffectsApplied(seqs) {
  const set = new Set((seqs || []).map((s) => Number(s)).filter((n) => n > 0));
  if (!set.size) return;
  for (const e of transcriptStore.entries) {
    const seq = e.sideEffectSeq || sideEffectSeqFromData(e.data);
    if (seq > 0 && set.has(seq)) {
      e.sideEffectApplied = true;
      e.sideEffectStale = false;
    }
  }
}

export function markSideEffectsStale(seqs) {
  const set = new Set((seqs || []).map((s) => Number(s)).filter((n) => n > 0));
  if (!set.size) return;
  for (const e of transcriptStore.entries) {
    const seq = e.sideEffectSeq || sideEffectSeqFromData(e.data);
    if (seq > 0 && set.has(seq) && !e.sideEffectApplied) {
      e.sideEffectStale = true;
    }
  }
}

export function addSystem(text) {
  transcriptStore.entries.push({ id: ++idSeq, kind: "system", text });
}

export function appendAssistant(delta) {
  if (!delta) return;
  finalizeReasoning();
  transcriptStore.assistantBuffer += delta;
  if (!hasStreamingKind("assistant")) upsertStreaming("assistant", "");
  markRevealStreaming("assistant", true);
  scheduleReveal();
}

export function appendReasoning(delta) {
  if (!delta) return;
  finalizeAssistant();
  transcriptStore.reasoningBuffer += delta;
  if (!transcriptStore.showReasoning) return;
  if (!hasStreamingKind("reasoning")) upsertStreaming("reasoning", "");
  markRevealStreaming("reasoning", true);
  scheduleReveal();
}

export function resumeReasoningReveal() {
  if (!transcriptStore.reasoningBuffer || !transcriptStore.showReasoning) return;
  markRevealStreaming("reasoning", true);
  scheduleReveal();
}

export function finalizeAssistant() {
  flushReveal("assistant");
  const text = transcriptStore.assistantBuffer;
  const usage = pendingUsageSuffix;
  removeStreaming("assistant");
  transcriptStore.assistantBuffer = "";
  pendingUsageSuffix = "";
  if (!text) return;
  transcriptStore.entries.push({
    id: ++idSeq,
    kind: "assistant",
    text,
    usage,
  });
}

export function finalizeReasoning() {
  flushReveal("reasoning");
  const text = transcriptStore.reasoningBuffer;
  removeStreaming("reasoning");
  transcriptStore.reasoningBuffer = "";
  if (!text) return;
  transcriptStore.entries.push({
    id: ++idSeq,
    kind: "reasoning",
    text,
  });
}

export function addAssistantFinal(text, usage = "") {
  removeStreaming("assistant");
  transcriptStore.assistantBuffer = "";
  pendingUsageSuffix = "";
  if (!text?.trim()) return;
  transcriptStore.entries.push({
    id: ++idSeq,
    kind: "assistant",
    text,
    usage,
  });
}

let pendingUsageSuffix = "";

function abortStreaming() {
  resetStreamReveal();
  removeStreaming("assistant");
  removeStreaming("reasoning");
  transcriptStore.assistantBuffer = "";
  transcriptStore.reasoningBuffer = "";
  pendingUsageSuffix = "";
}

export function applyRoundUsage(data) {
  const suffix = formatInlineUsage(parseUsageRound(data));
  if (!suffix) return;
  if (transcriptStore.assistantBuffer) {
    pendingUsageSuffix = suffix;
    return;
  }
  for (let i = transcriptStore.entries.length - 1; i >= 0; i--) {
    const e = transcriptStore.entries[i];
    if (e.kind === "assistant" && !e.streaming) {
      e.usage = suffix;
      return;
    }
  }
}

export function upsertToolCallFromSSE(data) {
  finalizeAssistant();
  finalizeReasoning();
  const partial = !!data?.partial;
  const toolIndex = toolIndexFromEvent(data);
  if (partial && toolIndex < 0 && !extractToolCallsFromEvent(data).some((c) => c.id)) {
    return;
  }
  for (const call of extractToolCallsFromEvent(data)) {
    if (call.name === USER_INFORMATION_TOOL) continue;
    const { blockId, migrateFrom } = resolveToolBlockId(call.id, toolIndex, partial);
    clearPartialToolIndex(toolIndex, partial);
    if (!blockId) continue;
    const parts = toolCallParts(call.name, call.arguments, {
      streaming: partial,
      rawArguments: call.rawArguments,
    });
    upsertToolCallEntry(blockId, migrateFrom, {
      data: {
        ...data,
        tool_name: call.name,
        name: call.name,
        tool_call_id: call.id || blockId,
        id: call.id || blockId,
        arguments: parts.arguments,
        raw_arguments: call.rawArguments,
        summary: parts.summary,
      },
      partial,
      summary: parts.summary,
      codePreview: parts.codePreview,
      call,
    });
    if (!partial) markToolBlockActive(blockId);
  }
}

/** turn 结束/取消时清除仍标记为 partial 的 tool_call，避免僵死「生成中」。 */
export function finalizePartialToolCalls({ interrupted = false } = {}) {
  for (const entry of transcriptStore.entries) {
    if (entry.kind !== "tool_call" || !entry.partial) continue;
    entry.partial = false;
    if (interrupted) {
      entry.data = { ...entry.data, interrupted: true };
    }
  }
}

function upsertToolCallEntry(blockId, migrateFrom, payload) {
  if (migrateFrom && migrateFrom !== blockId) {
    removeToolCallByBlockId(migrateFrom);
    forgetToolBlock(migrateFrom);
  }
  const idx = transcriptStore.entries.findIndex((e) => e.kind === "tool_call" && e.blockId === blockId);
  const prev = idx >= 0 ? transcriptStore.entries[idx] : null;
  const row = {
    id: prev?.id ?? ++idSeq,
    kind: "tool_call",
    blockId,
    sideEffectSeq: payload.data?.deferred ? sideEffectSeqFromData(payload.data) : prev?.sideEffectSeq || 0,
    sideEffectApplied: prev?.sideEffectApplied || false,
    sideEffectStale: prev?.sideEffectStale || false,
    ...payload,
    startedAt: prev?.startedAt ?? Date.now(),
  };
  if (idx >= 0) transcriptStore.entries[idx] = row;
  else {
    transcriptStore.entries.push(row);
    markToolBlockActive(blockId);
  }
}

function removeToolCallByBlockId(blockId) {
  const idx = transcriptStore.entries.findIndex((e) => e.kind === "tool_call" && e.blockId === blockId);
  if (idx >= 0) transcriptStore.entries.splice(idx, 1);
}

export function applyToolResult(data) {
  finalizeAssistant();
  finalizeReasoning();
  const callId = String(data?.tool_call_id || data?.id || "").trim();
  const idx = transcriptStore.entries.findIndex(
    (e) =>
      (e.kind === "tool_call" || e.kind === "tool_result") &&
      (e.blockId === callId || e.data?.tool_call_id === callId || e.data?.id === callId),
  );
  const elapsed =
    data?.duration_seconds != null
      ? Number(data.duration_seconds)
      : idx >= 0 && transcriptStore.entries[idx].startedAt
        ? (Date.now() - transcriptStore.entries[idx].startedAt) / 1000
        : null;
  const row = {
    id: idx >= 0 ? transcriptStore.entries[idx].id : ++idSeq,
    kind: "tool_result",
    blockId: callId || `tool-${idSeq}`,
    partial: false,
    sideEffectSeq: data?.deferred ? sideEffectSeqFromData(data) : transcriptStore.entries[idx]?.sideEffectSeq || 0,
    sideEffectApplied: transcriptStore.entries[idx]?.sideEffectApplied || false,
    sideEffectStale: transcriptStore.entries[idx]?.sideEffectStale || false,
    data: { ...data, duration_seconds: elapsed },
    startedAt: idx >= 0 ? transcriptStore.entries[idx].startedAt : undefined,
  };
  if (idx >= 0 && transcriptStore.entries[idx].kind === "tool_call") {
    const prev = transcriptStore.entries[idx];
    const args = prev.data?.arguments || resolveToolArgumentsFromData(prev.data);
    const toolName = String(data.tool_name || prev.data?.tool_name || prev.data?.name || "tool").trim();
    const summary = toolDisplayName(toolName, args);
    row.data = {
      ...row.data,
      arguments: args,
      raw_arguments: prev.data?.raw_arguments,
      summary,
    };
    row.summary = summary;
  } else {
    const args = resolveToolArgumentsFromData(data);
    const toolName = String(data?.tool_name || data?.name || "tool").trim();
    if (Object.keys(args).length) {
      const summary = toolDisplayName(toolName, args);
      row.data = { ...row.data, arguments: args, summary };
      row.summary = summary;
    }
  }
  if (idx >= 0) transcriptStore.entries[idx] = row;
  else transcriptStore.entries.push(row);
  if (callId) forgetToolBlock(callId);
}

/** @deprecated use upsertToolCallFromSSE */
export function addToolCall(data) {
  upsertToolCallFromSSE(data);
}

export function addToolResult(data) {
  applyToolResult(data);
}

export function clearTranscript() {
  transcriptStore.entries = [];
  abortStreaming();
}

/** 从 hydrate API 快照灌入 transcript（F-H7）；替换当前 entries。 */
export function loadTranscriptFromHydrate(entries) {
  abortStreaming();
  transcriptStore.entries = [];
  if (!Array.isArray(entries)) return;
  for (const raw of entries) {
    if (!raw || typeof raw !== "object") continue;
    const kind = String(raw.kind || "").trim();
    if (!kind) continue;
    const row = {
      ...raw,
      id: ++idSeq,
      kind,
      partial: raw.partial === true,
      streaming: false,
    };
    if (kind === "user" && !Array.isArray(row.images)) {
      row.images = [];
    }
    transcriptStore.entries.push(row);
    if (kind === "tool_call" && row.blockId) {
      markToolBlockActive(row.blockId);
    }
  }
}

function upsertStreaming(kind, text) {
  const idx = transcriptStore.entries.findIndex((e) => e.streaming && e.kind === kind);
  const row = { id: idx >= 0 ? transcriptStore.entries[idx].id : ++idSeq, kind, text, streaming: true };
  if (idx >= 0) transcriptStore.entries[idx] = row;
  else transcriptStore.entries.push(row);
}

function removeStreaming(kind) {
  const idx = transcriptStore.entries.findIndex((e) => e.streaming && e.kind === kind);
  if (idx >= 0) transcriptStore.entries.splice(idx, 1);
}

function parseUsageRound(data) {
  return parseUsageFields({
    prompt_tokens: data.round_prompt_tokens,
    completion_tokens: data.round_completion_tokens,
    prompt_cache_hit_tokens: data.round_prompt_cache_hit_tokens,
    prompt_cached_tokens: data.round_prompt_cached_tokens,
    prompt_cache_hit_rate: data.round_prompt_cache_hit_rate,
    reasoning_tokens: data.round_reasoning_tokens,
    completion_tokens_details: data.round_completion_tokens_details,
  });
}

function parseUsageStrip(data) {
  return parseUsageFields(data);
}

function parseUsageFields(data) {
  const prompt = intVal(data.prompt_tokens);
  const completion = intVal(data.completion_tokens);
  if (prompt <= 0 && completion <= 0) return null;
  let hit = intVal(data.prompt_cache_hit_tokens);
  const cached = intVal(data.prompt_cached_tokens);
  if (hit <= 0 && cached > 0) hit = cached;
  let rate = -1;
  if (typeof data.prompt_cache_hit_rate === "number" && data.prompt_cache_hit_rate >= 0) {
    rate = data.prompt_cache_hit_rate;
  } else if (prompt > 0 && hit > 0) {
    rate = Math.min(1, hit / prompt);
  }
  let reasoning = intVal(data.reasoning_tokens);
  if (reasoning <= 0 && data.completion_tokens_details) {
    reasoning = intVal(data.completion_tokens_details.reasoning_tokens);
  }
  return { prompt, completion, hit, rate, reasoning };
}

function intVal(v) {
  const n = Number(v);
  return Number.isFinite(n) && n > 0 ? Math.floor(n) : 0;
}

function formatCompact(n) {
  if (n >= 10000) return `${(Math.round(n / 100) / 10).toFixed(1).replace(/\.0$/, "")}k`;
  return n.toLocaleString("en-US");
}

function formatInlineUsage(snap) {
  if (!snap) return "";
  let t = ` · ↑${formatCompact(snap.prompt)} ↓${formatCompact(snap.completion)}`;
  if (snap.reasoning > 0) t += ` · think ${formatCompact(snap.reasoning)}`;
  return t;
}

export function formatInputStripUsage(snap) {
  if (!snap) return "";
  let t = `↑${formatCompact(snap.prompt)} ↓${formatCompact(snap.completion)}`;
  if (snap.hit > 0) {
    t += snap.rate >= 0 ? ` · hit ${formatCompact(snap.hit)} (${Math.round(snap.rate * 100)}%)` : ` · hit ${formatCompact(snap.hit)}`;
  }
  if (snap.reasoning > 0) t += ` · think ${formatCompact(snap.reasoning)}`;
  return t;
}

export { parseUsageStrip, parseUsageRound };
