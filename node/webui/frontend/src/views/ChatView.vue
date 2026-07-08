<script setup>
import { computed, onMounted, onUnmounted, ref, watch, nextTick } from "vue";
import { useRoute, useRouter } from "vue-router";
import * as api from "../api/node.js";
import { connectStream } from "../sse/stream.js";
import MainChatPanel from "../components/MainChatPanel.vue";
import DiagnosticsPanel from "../components/sidebar/DiagnosticsPanel.vue";
import SessionPanel from "../components/SessionPanel.vue";
import ContextPanel from "../components/ContextPanel.vue";
import HelpPanel from "../components/HelpPanel.vue";
import ChildrenPanel from "../components/ChildrenPanel.vue";
import {
  sessionStore,
  persistSessionId,
  ensureSession,
  beginSubmit,
  beginImplicitTurn,
  finishTurn,
  markTurnContent,
  shouldAcceptDone,
  isStaleEvent,
  isDuplicateEvent,
  markEventApplied,
  resetEventTracking,
} from "../stores/session.js";
import {
  transcriptStore,
  noteSeq,
  addUser,
  addDeferredUser,
  markSideEffectsApplied,
  markSideEffectsStale,
  addSystem,
  appendAssistant,
  appendReasoning,
  finalizeAssistant,
  finalizeReasoning,
  upsertToolCallFromSSE,
  applyToolResult,
  clearTranscript,
  applyRoundUsage,
  resumeReasoningReveal,
  finalizePartialToolCalls,
} from "../stores/transcript.js";
import {
  hitlStore,
  enqueueHitl,
  getHitlAt,
  dequeueHitlAt,
  peekHitl,
  clearHitl,
  isA2ARelay,
  a2aRelaySuffix,
  a2aApprovedSummary,
  extractToolApprovals,
  buildApprovalResume,
  buildApprovalOneResume,
  extractUserInfo,
  buildUserInfoResume,
  buildUserInfoResumeFromSelection,
  enqueueHitlRequired,
  shouldSkipChildRuntimeDisplay,
} from "../stores/hitl.js";
import { consumeStartupURL, hydrateSession } from "../stores/hydrate.js";
import { chromeStore, setUsageFromSSE, resetUsageStrip } from "../stores/chrome.js";
import {
  startStatus,
  finishStatus,
  finishWaitingStatuses,
  hasStatus,
  resetStatusLines,
} from "../stores/statusLines.js";
import { resetToolStream } from "../stores/toolStream.js";
import {
  onChildCreated,
  onChildFinished,
  resetRemoteWorkers,
  resetPeerInvokeInflight,
  setChildAwaitingApproval,
  noteToolCallForWorkers,
  noteToolResultForWorkers,
  syncChildAgentsFromApi,
} from "../stores/remoteWorkers.js";
import { runSlashCommand } from "../utils/commands.js";
import { approvalItemDisplayName, sessionDisplayTitle } from "../utils/format.js";

const router = useRouter();
const route = useRoute();

const hitlSelected = ref(0);
const cancelling = ref(false);
const streamHandle = ref(null);
const apiBase = typeof window !== "undefined" ? window.location.origin : "";
const sessionPanelRef = ref(null);
const chatPanelRef = ref(null);

const entries = computed(() => transcriptStore.entries);
const hasUserInfoHitl = computed(() => peekHitl()?.kind === "user_information");
const canSend = computed(() => {
  if (hitlStore.busy) return false;
  if (hasUserInfoHitl.value) return true;
  return !sessionStore.awaitingTurn && !peekHitl()?.kind;
});
const sending = computed(() => sessionStore.awaitingTurn);
const thinkingSupported = computed(() => !!chromeStore.llmSettings?.thinking_supported);
const pendingApprovals = computed(() =>
  hitlStore.queue.filter((h) => h.kind === "approval").reduce((n, h) => n + extractToolApprovals(h.data).length, 0),
);

const PANEL_SETTINGS_ROUTES = {
  skills: "/settings/skills",
  triggers: "/settings/triggers",
  policy: "/settings/security",
  update: "/settings/about",
  status: "/settings/general",
};

function syncRouteSession(sessionId) {
  const id = String(sessionId || "").trim();
  if (!id) return;
  if (route.params.sessionId === id) return;
  router.replace({ name: "chat", params: { sessionId: id } });
}

function syncReasoningDisplay(llm) {
  if (!llm?.thinking_supported) return;
  const t = String(llm.thinking || "").trim().toLowerCase();
  const enable = t && !["disabled", "off", "false", "0"].includes(t);
  const wasHidden = !transcriptStore.showReasoning;
  if (enable) transcriptStore.showReasoning = true;
  if (enable && wasHidden) resumeReasoningReveal();
}

function restartStream() {
  streamHandle.value?.close();
  streamHandle.value = connectStream({
    getSessionId: () => sessionStore.sessionId,
    onStatus: (s) => {
      chromeStore.sseStatus = s;
    },
    onEvent: handleEvent,
  });
}

async function activateSessionStream() {
  const prev = sessionStore.sessionId;
  await hydrateSession();
  if (sessionStore.sessionId !== prev || !streamHandle.value) {
    restartStream();
  }
  await syncChildAgentsFromApi();
  await nextTick();
  chatPanelRef.value?.scrollToLastAssistant?.();
}

async function refreshMeta() {
  try {
    const [health, info, llm] = await Promise.all([api.getHealth(), api.getAgentInfo(), api.getLLMSettings()]);
    chromeStore.agentInfo = { ...health, ...info };
    chromeStore.llmSettings = llm;
    syncReasoningDisplay(llm);
  } catch (e) {
    sessionStore.error = e.message;
  }
}

async function refreshContextTokens() {
  if (!sessionStore.sessionId) return;
  try {
    const ctx = await api.getSessionContext(sessionStore.sessionId);
    chromeStore.contextTokens = Number(ctx.messages_total_tokens ?? -1);
  } catch {
    /* keep last */
  }
}

function handleEvent(ev) {
  noteSeq(ev.seq);
  if (isStaleEvent(ev.seq) || isDuplicateEvent(ev.seq)) return;
  if (shouldSkipChildRuntimeDisplay(ev.type, ev.data)) return;

  switch (ev.type) {
    case "assistant":
      markTurnContent();
      finishWaitingStatuses();
      appendAssistant(String(ev.data.content || ""));
      break;
    case "reasoning":
      markTurnContent();
      finishWaitingStatuses({ beforeReasoning: true });
      if (!hasStatus("thinking")) startStatus("thinking");
      appendReasoning(String(ev.data.content || ""));
      break;
    case "tool_call":
      markTurnContent();
      finishWaitingStatuses();
      noteToolCallForWorkers(ev.data);
      upsertToolCallFromSSE(ev.data);
      break;
    case "tool_result":
      markTurnContent();
      finishWaitingStatuses();
      noteToolResultForWorkers(ev.data);
      applyToolResult(ev.data);
      break;
    case "usage":
      setUsageFromSSE(ev.data);
      applyRoundUsage(ev.data);
      break;
    case "error":
      markTurnContent();
      finishWaitingStatuses();
      finalizePartialToolCalls({ interrupted: true });
      addSystem(`error: ${ev.data.message || "unknown"}`);
      if (sessionStore.awaitingTurn) finishTurn();
      break;
    case "done":
      finalizeAssistant();
      finalizeReasoning();
      finishWaitingStatuses();
      finalizePartialToolCalls({ interrupted: true });
      if (shouldAcceptDone(ev.seq)) {
        finishTurn();
        sessionStore.statusLine = `回合结束 (${ev.data.finish_reason || "stop"})`;
        resetToolStream();
        resetPeerInvokeInflight();
        syncChildAgentsFromApi();
      }
      refreshContextTokens();
      break;
    case "hitl_required":
      finalizeAssistant();
      finalizeReasoning();
      finishWaitingStatuses();
      {
        const { approval } = enqueueHitlRequired(ev.data);
        if (approval?.child_session_id) setChildAwaitingApproval(approval.child_session_id, true);
      }
      if (isA2ARelay(ev.data) && sessionStore.awaitingTurn) finishTurn();
      chromeStore.hitlQueueLen = hitlStore.queue.length;
      break;
    case "approval_required":
      finalizeAssistant();
      finalizeReasoning();
      finishWaitingStatuses();
      enqueueHitl({ kind: "approval", data: ev.data });
      if (ev.data?.child_session_id) setChildAwaitingApproval(ev.data.child_session_id, true);
      if (isA2ARelay(ev.data) && sessionStore.awaitingTurn) finishTurn();
      chromeStore.hitlQueueLen = hitlStore.queue.length;
      hitlStore.busy = false;
      break;
    case "user_information_required":
      finalizeAssistant();
      finalizeReasoning();
      finishWaitingStatuses();
      enqueueHitl({ kind: "user_information", data: ev.data });
      if (isA2ARelay(ev.data) && sessionStore.awaitingTurn) finishTurn();
      chromeStore.hitlQueueLen = hitlStore.queue.length;
      hitlStore.busy = false;
      break;
    case "temporary_agent_created":
      onChildCreated(ev.data);
      addSystem(formatChildLifecycle(ev.type, ev.data));
      break;
    case "temporary_agent_completed":
    case "temporary_agent_cancelled":
      onChildFinished(ev.data?.child_session_id);
      addSystem(formatChildLifecycle(ev.type, ev.data));
      break;
    case "context_compression_blocking":
    case "context_compression_silent":
      handleCompressionEvent(ev.type, ev.data);
      refreshContextTokens();
      break;
    case "side_effect_turn_start":
      beginImplicitTurn();
      sessionStore.statusLine = "处理旁路回调…";
      break;
    case "user_message_deferred":
      addDeferredUser(
        String(ev.data.content || ""),
        String(ev.data.user_name || ""),
        Number(ev.data.side_effect_seq) || 0,
      );
      break;
    case "side_effect_applied":
      markSideEffectsApplied(Array.isArray(ev.data.seqs) ? ev.data.seqs : []);
      break;
    case "side_effects_cleared":
      markSideEffectsStale(Array.isArray(ev.data.seqs) ? ev.data.seqs : []);
      break;
    default:
      break;
  }
  markEventApplied(ev.seq);
}

function handleCompressionEvent(type, data) {
  const phase = String(data.phase || "");
  const mode = type === "context_compression_blocking" ? "blocking" : "silent";
  if (mode === "blocking") {
    if (phase === "start") startStatus("compression_blocking");
    else if (phase === "end") {
      finishStatus("compression_blocking");
      addSystem(formatCompressionDetail(mode, data));
    }
    return;
  }
  if (phase === "start") {
    addSystem(`[compression] silent · start · target ${data.compressed_message_count || "?"}`);
  } else if (phase === "end") {
    addSystem(formatCompressionDetail(mode, data));
  }
}

function formatChildLifecycle(type, data) {
  const id = String(data.child_session_id || "").slice(0, 16);
  const purpose = String(data.purpose || "").trim();
  if (type === "temporary_agent_created") return `临时 Agent 已创建 · ${purpose || id}`;
  if (type === "temporary_agent_cancelled") return `临时 Agent 已取消 · ${id}`;
  return `临时 Agent 已结束 · ${id} · ${data.status || "completed"}`;
}

function formatCompressionDetail(mode, data) {
  const status = data.status || "done";
  const count = data.compressed_message_count || 0;
  if (status === "applied") {
    let line = `[compression] ${mode} applied — replaced ${count} messages`;
    const prompt = data.prompt_tokens;
    const completion = data.completion_tokens;
    if (prompt != null && completion != null) {
      const rate = data.token_reduction_rate != null ? `, −${Math.round(Number(data.token_reduction_rate) * 100)}%` : "";
      line += ` (prompt ${prompt}→completion ${completion}${rate})`;
    }
    return line;
  }
  if (status === "failed") return `[compression] ${mode} failed — keeping original context`;
  if (status === "stale") return `[compression] ${mode} stale — discarded`;
  if (status === "invalid") return `[compression] ${mode} invalid — discarded`;
  return `[compression] ${mode} finished (${status})`;
}

async function submitHitlApproval(approveAll, hitlIndex = 0) {
  const item = getHitlAt(hitlIndex);
  if (!item || item.kind !== "approval") return;
  hitlStore.busy = true;
  hitlStore.busyIndex = hitlIndex;
  const resume = buildApprovalResume(item.data, { approveAll });
  try {
    await api.submitResume(sessionStore.sessionId, resume);
    if (isA2ARelay(item.data)) {
      const suffix = a2aRelaySuffix(item.data);
      extractToolApprovals(item.data).forEach((it) => {
        const approved = approveAll !== false && resume.approved?.includes(it.callId);
        addSystem(`${approvalItemDisplayName(it)}${suffix} · ${a2aApprovedSummary(item.data, approved)}`);
      });
    }
    dequeueHitlAt(hitlIndex);
    if (item.data?.child_session_id) setChildAwaitingApproval(item.data.child_session_id, false);
    chromeStore.hitlQueueLen = hitlStore.queue.length;
    hitlStore.busy = false;
    hitlStore.busyIndex = -1;
    beginSubmit();
    if (!sessionStore.turnContentSeen) startStatus("prefilling");
  } catch (e) {
    sessionStore.error = e.message;
    hitlStore.busy = false;
    hitlStore.busyIndex = -1;
  }
}

async function submitHitlOne(payload, approve) {
  const hitlIndex = typeof payload === "object" ? payload.index : 0;
  const callId = typeof payload === "object" ? payload.callId : payload;
  const item = getHitlAt(hitlIndex);
  if (!item || item.kind !== "approval") return;
  hitlStore.busy = true;
  hitlStore.busyIndex = hitlIndex;
  const resume = buildApprovalOneResume(item.data, callId, approve);
  try {
    await api.submitResume(sessionStore.sessionId, resume);
    dequeueHitlAt(hitlIndex);
    if (item.data?.child_session_id) setChildAwaitingApproval(item.data.child_session_id, false);
    chromeStore.hitlQueueLen = hitlStore.queue.length;
    hitlStore.busy = false;
    hitlStore.busyIndex = -1;
    beginSubmit();
    if (!sessionStore.turnContentSeen) startStatus("prefilling");
  } catch (e) {
    sessionStore.error = e.message;
    hitlStore.busy = false;
    hitlStore.busyIndex = -1;
  }
}

async function submitHitlUserInfo(hitlIndex, text) {
  const item = getHitlAt(hitlIndex);
  if (!item || item.kind !== "user_information") return;
  const req = extractUserInfo(item.data);
  let resume;
  if (req.options.length) {
    const opt = req.options[hitlSelected.value] || req.options[0];
    resume = req.allowMultiple
      ? buildUserInfoResumeFromSelection(item.data, [opt.id])
      : buildUserInfoResumeFromSelection(item.data, [opt.id]);
  } else {
    resume = buildUserInfoResume(item.data, text);
  }
  hitlStore.busy = true;
  hitlStore.busyIndex = hitlIndex;
  try {
    await api.submitResume(sessionStore.sessionId, resume);
    dequeueHitlAt(hitlIndex);
    if (item.data?.child_session_id) setChildAwaitingApproval(item.data.child_session_id, false);
    chromeStore.hitlQueueLen = hitlStore.queue.length;
    hitlStore.busy = false;
    hitlStore.busyIndex = -1;
    hitlSelected.value = 0;
    beginSubmit();
    if (!sessionStore.turnContentSeen) startStatus("prefilling");
  } catch (e) {
    sessionStore.error = e.message;
    hitlStore.busy = false;
    hitlStore.busyIndex = -1;
  }
}

async function onSendMessage(payload) {
  sessionStore.error = "";
  const text = typeof payload === "string" ? payload : String(payload?.text || "").trim();
  const contentParts = typeof payload === "string" ? null : payload?.contentParts;
  const images = typeof payload === "string" ? [] : payload?.images || [];

  if (text.startsWith("/")) {
    await handleCommand(text);
    return;
  }

  const hitl = peekHitl();
  if (hitl?.kind === "user_information") {
    await submitHitlUserInfo(0, text);
    return;
  }

  if (sessionStore.awaitingTurn) {
    sessionStore.error = "上一回合尚未结束";
    return;
  }

  await activateSessionStream();
  clearHitl();
  addUser(text, images);
  beginSubmit();
  try {
    await api.submitMessage(sessionStore.sessionId, text, contentParts);
    sessionStore.statusLine = "等待 Agent 回复…";
    if (!sessionStore.turnContentSeen) startStatus("prefilling");
  } catch (e) {
    finishStatus("prefilling");
    finishTurn();
    sessionStore.error = e.message;
  }
}

async function handleCommand(cmd) {
  const res = await runSlashCommand(cmd, { toolFoldVerbose: transcriptStore.toolFoldVerbose });
  if (res.system) {
    addSystem(res.system);
    return;
  }
  if (res.error) {
    sessionStore.error = res.error;
    return;
  }
  if (res.action === "cancel") {
    await cancelTurn();
    return;
  }
  if (res.action === "clear") {
    await api.clearContext(await ensureSession());
    await hydrateSession();
    restartStream();
    resetStatusLines();
    resetToolStream();
    resetUsageStrip();
    chromeStore.contextTokens = 0;
    addSystem("已清空对话上下文");
    return;
  }
  if (res.action === "compress") {
    const out = await api.compressContext(await ensureSession());
    addSystem(`压缩: ${out.status || "done"}`);
    refreshContextTokens();
    return;
  }
  if (res.action === "new") {
    await createNewSession();
    return;
  }
  if (res.action === "switch") {
    if (!res.arg) {
      sessionStore.error = "用法: /switch <session_id>";
      return;
    }
    await switchSession(res.arg);
    return;
  }
  if (res.action === "reasoning") {
    transcriptStore.showReasoning = ["on", "true", "1"].includes(String(res.arg).toLowerCase());
    addSystem(`reasoning 显示: ${transcriptStore.showReasoning ? "开启" : "关闭"}`);
    return;
  }
  if (res.action === "thinking") {
    await handleThinkingCommand(res.arg);
    return;
  }
  if (res.action === "tools_verbose") {
    transcriptStore.toolFoldVerbose = !!res.on;
    addSystem(`tool 输出: ${transcriptStore.toolFoldVerbose ? "详细" : "折叠"}`);
    return;
  }
  if (res.action === "upload") {
    await handleUploadCommand(res.upload);
    return;
  }
  if (res.panel) {
    await openPanel(res.panel, res.arg);
  }
}

async function handleUploadCommand(spec) {
  if (!spec || spec.error) {
    sessionStore.error = spec?.error || "upload 参数无效";
    return;
  }
  const { kind, path, id, version, name, platform, publish } = spec;
  try {
    let out;
    if (kind === "skill" || kind === "skills") {
      out = await api.uploadSkillToManage({ path, skillId: id, version, name, publish });
    } else if (["externaltool", "externaltools", "tool", "tools"].includes(kind)) {
      out = await api.uploadExternalToolToManage({ path, toolId: id, version, name, platform, publish });
    } else if (kind === "plugin" || kind === "plugins") {
      out = await api.uploadPluginToManage({ path, pluginId: id, version, name, platform, publish });
    } else {
      sessionStore.error = `未知 upload 类型: ${kind}`;
      return;
    }
    const note = out.message ? ` — ${out.message}` : "";
    addSystem(`已上传 ${out.kind} ${out.id}@${out.version}（${out.status}${note}）`);
  } catch (err) {
    sessionStore.error = err.message;
  }
}

async function createNewSession() {
  const created = await api.createSession("");
  persistSessionId(created.session_id);
  clearTranscript();
  resetUsageStrip();
  resetStatusLines();
  resetToolStream();
  resetRemoteWorkers();
  resetEventTracking();
  clearHitl();
  restartStream();
  addSystem(`新 session: ${created.session_id}`);
  syncRouteSession(created.session_id);
  sessionPanelRef.value?.refresh?.();
}

async function switchSession(id) {
  const prev = sessionStore.sessionId;
  persistSessionId(id);
  resetStatusLines();
  resetToolStream();
  resetRemoteWorkers();
  resetUsageStrip();
  try {
    await hydrateSession();
  } catch (e) {
    sessionStore.error = e.message;
    clearTranscript();
    clearHitl();
    resetEventTracking();
    finishTurn();
    return;
  }
  restartStream();
  if (id !== prev) {
    addSystem(`已切换 session: ${sessionStore.sessionId}`);
  }
  refreshContextTokens();
  await syncChildAgentsFromApi();
  syncRouteSession(id);
  sessionPanelRef.value?.refresh?.();
  await nextTick();
  chatPanelRef.value?.scrollToLastAssistant?.();
}

async function deleteSessionById(payload) {
  const sid = String(typeof payload === "string" ? payload : payload?.id || "").trim();
  if (!sid) {
    sessionStore.error = "无法删除：会话 ID 无效";
    return;
  }
  const session = typeof payload === "object" && payload?.session ? payload.session : { session_id: sid };
  const label = sessionDisplayTitle(session);
  if (!window.confirm(`确定删除会话「${label}」？\n\n将停止该会话并清除持久化记录，不可恢复。`)) return;
  sessionStore.error = "";
  sessionPanelRef.value?.setDeleting?.(sid);
  try {
    await api.deleteSession(sid);
    if (sessionStore.sessionId === sid) {
      finishTurn();
      hitlStore.busy = false;
      await createNewSession();
    } else {
      addSystem(`已删除 session: ${sid.slice(0, 16)}…`);
      sessionPanelRef.value?.refresh?.();
    }
  } catch (e) {
    sessionStore.error = e.message;
  } finally {
    sessionPanelRef.value?.setDeleting?.("");
  }
}

async function openContextPanel() {
  sessionStore.error = "";
  await ensureSession();
  chromeStore.panel = "context";
}

async function toggleThinkingMode() {
  if (!chromeStore.llmSettings?.thinking_supported) {
    sessionStore.error = "当前 provider 不支持 thinking 控制（需 deepseek 或 qwen）";
    return;
  }
  sessionStore.error = "";
  const t = String(chromeStore.llmSettings.thinking || "").toLowerCase();
  const enabled = !["disabled", "off"].includes(t);
  try {
    chromeStore.llmSettings = await api.patchLLMSettings({ thinking: enabled ? "disabled" : "enabled" });
    syncReasoningDisplay(chromeStore.llmSettings);
  } catch (e) {
    sessionStore.error = e.message;
  }
}

async function cycleThinkingEffort() {
  if (!chromeStore.llmSettings?.thinking_supported) return;
  sessionStore.error = "";
  const current = String(chromeStore.llmSettings.reasoning_effort || "high").toLowerCase();
  const next = current === "max" ? "high" : "max";
  try {
    chromeStore.llmSettings = await api.patchLLMSettings({ reasoning_effort: next });
  } catch (e) {
    sessionStore.error = e.message;
  }
}

async function handleThinkingCommand(arg) {
  const parts = String(arg || "").trim().split(/\s+/);
  const patch = {};
  if (!parts[0]) {
    addSystem(`thinking: ${chromeStore.llmSettings?.thinking || "-"}`);
    return;
  }
  if (["on", "enabled", "true", "1"].includes(parts[0])) patch.thinking = "enabled";
  else if (["off", "disabled", "false", "0"].includes(parts[0])) patch.thinking = "disabled";
  else if (parts[0] === "effort" && ["high", "max"].includes(parts[1])) patch.reasoning_effort = parts[1];
  else {
    sessionStore.error = "用法: /thinking on|off 或 /thinking effort high|max";
    return;
  }
  chromeStore.llmSettings = await api.patchLLMSettings(patch);
  addSystem(`thinking: ${chromeStore.llmSettings.thinking || "-"}`);
}

async function openPanel(name, arg) {
  const settingsPath = PANEL_SETTINGS_ROUTES[name];
  if (settingsPath) {
    await ensureSession();
    try {
      if (name === "skills") {
        if (arg?.startsWith("load ")) {
          await api.loadSkill(sessionStore.sessionId, arg.slice(5).trim());
        } else if (arg?.startsWith("unload ")) {
          await api.unloadSkill(sessionStore.sessionId, arg.slice(7).trim());
        }
      }
      await router.push(settingsPath);
    } catch (e) {
      sessionStore.error = e.message;
    }
    return;
  }
  if (name === "sessions") {
    sessionPanelRef.value?.refresh?.();
    return;
  }
  await ensureSession();
  try {
    chromeStore.panel = name;
  } catch (e) {
    sessionStore.error = e.message;
    chromeStore.panel = null;
  }
}

function closePanel() {
  chromeStore.panel = null;
}

function onHelpPick(cmd) {
  closePanel();
  chatPanelRef.value?.setDraft?.(cmd);
}

async function onSessionSwitch(sessionId) {
  closePanel();
  await switchSession(sessionId);
}

async function bootstrapSessionFromRoute() {
  const fromRoute = String(route.params.sessionId || "").trim();
  if (fromRoute) {
    persistSessionId(fromRoute);
    return;
  }
  consumeStartupURL();
  if (sessionStore.sessionId) {
    syncRouteSession(sessionStore.sessionId);
  }
}

async function cancelTurn() {
  if (!sessionStore.sessionId || cancelling.value || !sessionStore.awaitingTurn) return;
  cancelling.value = true;
  sessionStore.error = "";
  try {
    finishWaitingStatuses();
    finalizePartialToolCalls({ interrupted: true });
    await api.cancelTurn(sessionStore.sessionId);
    finishTurn();
    clearHitl();
    finalizeAssistant();
    finalizeReasoning();
    resetToolStream();
    sessionStore.statusLine = "已请求取消 turn";
    addSystem("turn 已取消");
  } catch (e) {
    sessionStore.error = e.message;
  } finally {
    cancelling.value = false;
  }
}

function onKeydown(e) {
  if (e.key === "Escape" && chromeStore.panel) {
    closePanel();
  }
}

onMounted(async () => {
  await bootstrapSessionFromRoute();
  await refreshMeta();
  await activateSessionStream();
  refreshContextTokens();
  window.addEventListener("keydown", onKeydown);
});

watch(
  () => route.params.sessionId,
  async (id, prev) => {
    const sid = String(id || "").trim();
    if (!sid || sid === sessionStore.sessionId || sid === prev) return;
    await switchSession(sid);
  },
);

onUnmounted(() => {
  streamHandle.value?.close();
  window.removeEventListener("keydown", onKeydown);
});

watch(
  () => sessionStore.sessionId,
  () => {
    chromeStore.hitlQueueLen = hitlStore.queue.length;
  },
);
</script>

<template>
  <div class="app__body app__body--chat-v61">
    <aside class="app__col app__col--sessions">
      <SessionPanel ref="sessionPanelRef" @switch="switchSession" @new="createNewSession" @delete="deleteSessionById" />
    </aside>

    <div class="app__main-col">
      <div v-if="pendingApprovals > 0" class="chat-hitl-sticky">
        {{ pendingApprovals }} 项待你确认 — 请在对话中批准或拒绝
      </div>
      <div v-if="sessionStore.error" class="chat-error-banner">{{ sessionStore.error }}</div>

      <MainChatPanel
        ref="chatPanelRef"
        :entries="entries"
        :hitl-queue="hitlStore.queue"
        :show-reasoning="transcriptStore.showReasoning"
        :tool-verbose="transcriptStore.toolFoldVerbose"
        :disabled="!canSend"
        :sending="sending"
        :cancelling="cancelling"
        :hitl-busy="hitlStore.busy"
        :hitl-busy-index="hitlStore.busyIndex"
        :thinking-supported="thinkingSupported"
        :llm-settings="chromeStore.llmSettings"
        @send="onSendMessage"
        @cancel="cancelTurn"
        @open-context="openContextPanel"
        @toggle-thinking="toggleThinkingMode"
        @cycle-effort="cycleThinkingEffort"
        @approve-all="(idx) => submitHitlApproval(true, idx)"
        @reject-all="(idx) => submitHitlApproval(false, idx)"
        @approve-one="(payload) => submitHitlOne(payload, true)"
        @reject-one="(payload) => submitHitlOne(payload, false)"
        @user-info-submit="(idx) => submitHitlUserInfo(idx, '')"
        @user-info-selected="(v) => { hitlSelected = v; }"
      />

      <div v-if="chromeStore.panel" class="panel-overlay" @click.self="closePanel">
        <ContextPanel v-if="chromeStore.panel === 'context'" @close="closePanel" />
        <HelpPanel v-else-if="chromeStore.panel === 'help'" @close="closePanel" @pick="onHelpPick" />
        <ChildrenPanel v-else-if="chromeStore.panel === 'children'" @close="closePanel" />
      </div>
    </div>

    <DiagnosticsPanel :api-base="apiBase" @open-context="openContextPanel" />
  </div>
</template>
