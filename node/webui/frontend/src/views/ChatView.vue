<script setup>
import { computed, onMounted, onUnmounted, onActivated, ref, watch, nextTick } from "vue";
import { useRoute, useRouter } from "vue-router";
import * as api from "../api/node.js";
import { connectStream } from "../sse/stream.js";
import MainChatPanel from "../components/MainChatPanel.vue";
import AgentPanel from "../components/AgentPanel.vue";
import AgentCreateModal from "../components/AgentCreateModal.vue";
import AgentEmptyState from "../components/AgentEmptyState.vue";
import ChildrenPanel from "../components/ChildrenPanel.vue";
import ActivityPanel from "../components/ActivityPanel.vue";
import {
  sessionStore,
  persistAgentId,
  ensureAgent,
  beginSubmit,
  beginImplicitTurn,
  finishTurn,
  markTurnContent,
  shouldAcceptDone,
  isStaleEvent,
  isDuplicateEvent,
  markEventApplied,
  shouldAckSSEEvent,
  resetEventTracking,
} from "../stores/session.js";
import {
  transcriptStore,
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
  setShowReasoning,
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
import {
  startDesktopFocusHeartbeat,
  stopDesktopFocusHeartbeat,
  pulseDesktopFocus,
} from "../stores/desktopFocus.js";
import { COMPOSER_DRAFT_KEY } from "../utils/helpCommands.js";
import {
  formatChildLifecycle,
  formatCompressionDetail,
  formatCompressionStart,
} from "../utils/activityFormat.js";
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
import { approvalItemDisplayName, agentDisplayTitle, agentRecordId } from "../utils/format.js";

const router = useRouter();
const route = useRoute();

const hitlSelected = ref(0);
const cancelling = ref(false);
const streamHandle = ref(null);
const agentPanelRef = ref(null);
const showAgentCreateModal = ref(false);
const createModalTemplateId = ref("");
const agentListCount = ref(null);
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
const showNoAgentWelcome = computed(
  () => !sessionStore.sessionId && agentListCount.value === 0
);

const PANEL_SETTINGS_ROUTES = {
  help: "/settings/about",
  context: "/settings/context",
  skills: "/settings/skills",
  triggers: "/settings/triggers",
  policy: "/settings/security",
  update: "/settings/about",
  status: "/settings/general",
};

function syncRouteAgent(agentId) {
  const id = String(agentId || "").trim();
  if (route.params.agentId === id) return;
  router.replace({ name: "agents", params: id ? { agentId: id } : {} });
}

function syncReasoningDisplay(llm) {
  if (!llm?.thinking_supported) return;
  const t = String(llm.thinking || "").trim().toLowerCase();
  const enable = t && !["disabled", "off", "false", "0"].includes(t);
  if (enable) setShowReasoning(true);
}

function restartStream() {
  streamHandle.value?.close();
  if (!sessionStore.sessionId) {
    streamHandle.value = null;
    return;
  }
  streamHandle.value = connectStream({
    getAgentId: () => sessionStore.sessionId,
    onStatus: (s) => {
      chromeStore.sseStatus = s;
    },
    onEvent: handleEvent,
  });
}

async function activateAgentStream() {
  if (!sessionStore.sessionId) {
    clearTranscript();
    clearHitl();
    finishTurn();
    streamHandle.value?.close();
    streamHandle.value = null;
    chromeStore.sseStatus = "idle";
    return;
  }
  const prev = sessionStore.sessionId;
  await hydrateSession();
  if (sessionStore.sessionId !== prev || !streamHandle.value) {
    restartStream();
  }
  await syncChildAgentsFromApi();
  await nextTick();
  chatPanelRef.value?.scrollToTail?.();
}

async function refreshAfterPageRestore() {
  resetStatusLines();
  resetToolStream();
  resetRemoteWorkers();
  resetUsageStrip();
  resetEventTracking();
  await activateAgentStream();
  refreshContextTokens();
  agentPanelRef.value?.refresh?.();
}

function onPageShow(event) {
  if (event?.persisted) {
    void refreshAfterPageRestore();
  }
}

async function refreshMeta() {
  try {
    const boot = await api.getUIBootstrap();
    chromeStore.agentInfo = { ...(boot.health || {}), ...(boot.info || {}) };
    chromeStore.llmSettings = boot.llm || null;
    syncReasoningDisplay(chromeStore.llmSettings);
  } catch (e) {
    // 回退到旧的并行请求（兼容旧 Node）
    try {
      const [health, info, llm] = await Promise.all([api.getHealth(), api.getAgentInfo(), api.getLLMSettings()]);
      chromeStore.agentInfo = { ...health, ...info };
      chromeStore.llmSettings = llm;
      syncReasoningDisplay(llm);
    } catch (e2) {
      sessionStore.error = e2.message || e.message;
    }
  }
}

async function refreshContextTokens() {
  if (!sessionStore.sessionId) return;
  try {
    const ctx = await api.getAgentContext(sessionStore.sessionId);
    chromeStore.contextTokens = Number(ctx.messages_total_tokens ?? -1);
  } catch {
    /* keep last */
  }
}

function handleEvent(ev) {
  if (isStaleEvent(ev.seq) || isDuplicateEvent(ev.seq)) return;
  const skipRender = shouldSkipChildRuntimeDisplay(ev.type, ev.data);

  if (!skipRender) {
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
      break;
    case "approval_required":
      finalizeAssistant();
      finalizeReasoning();
      finishWaitingStatuses();
      enqueueHitl({ kind: "approval", data: ev.data });
      if (ev.data?.child_session_id) setChildAwaitingApproval(ev.data.child_session_id, true);
      if (isA2ARelay(ev.data) && sessionStore.awaitingTurn) finishTurn();
      hitlStore.busy = false;
      break;
    case "user_information_required":
      finalizeAssistant();
      finalizeReasoning();
      finishWaitingStatuses();
      enqueueHitl({ kind: "user_information", data: ev.data });
      if (isA2ARelay(ev.data) && sessionStore.awaitingTurn) finishTurn();
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
  }

  markEventApplied(ev.seq, { ack: !skipRender && shouldAckSSEEvent(ev.type, ev.data) });
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
    addSystem(formatCompressionStart(mode, data));
  } else if (phase === "end") {
    addSystem(formatCompressionDetail(mode, data));
  }
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

  await activateAgentStream();
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
    await api.clearContext(await ensureAgent());
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
    const out = await api.compressContext(await ensureAgent());
    addSystem(`压缩: ${out.status || "done"}`);
    refreshContextTokens();
    return;
  }
  if (res.action === "new") {
    openCreateWizard();
    return;
  }
  if (res.action === "switch") {
    if (!res.arg) {
      sessionStore.error = "用法: /switch <agent_id>";
      return;
    }
    await switchAgent(res.arg);
    return;
  }
  if (res.action === "reasoning") {
    setShowReasoning(["on", "true", "1"].includes(String(res.arg).toLowerCase()));
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

async function openCreateWizard(templateId = "") {
  createModalTemplateId.value = String(templateId || "").trim();
  showAgentCreateModal.value = true;
}

function onAgentsUpdated(list) {
  agentListCount.value = Array.isArray(list) ? list.length : 0;
}

function onCreateModalClose() {
  showAgentCreateModal.value = false;
  createModalTemplateId.value = "";
}

async function onAgentCreated(created) {
  const id = agentRecordId(created);
  if (!id) return;
  persistAgentId(id);
  clearTranscript();
  resetUsageStrip();
  resetStatusLines();
  resetToolStream();
  resetRemoteWorkers();
  resetEventTracking();
  clearHitl();
  finishTurn();
  restartStream();
  syncRouteAgent(id);
  pulseDesktopFocus();
  agentPanelRef.value?.refresh?.();
  try {
    await hydrateSession();
  } catch (e) {
    sessionStore.error = e.message;
  }
  await nextTick();
  chatPanelRef.value?.scrollToTail?.();
}

async function switchAgent(id) {
  persistAgentId(id);
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
  refreshContextTokens();
  await syncChildAgentsFromApi();
  syncRouteAgent(id);
  pulseDesktopFocus();
  agentPanelRef.value?.refresh?.();
  await nextTick();
  chatPanelRef.value?.scrollToTail?.();
}

async function deleteAgentById(payload) {
  const aid = String(typeof payload === "string" ? payload : payload?.id || "").trim();
  if (!aid) {
    sessionStore.error = "无法删除：Agent ID 无效";
    return;
  }
  const agent = typeof payload === "object" && payload?.agent ? payload.agent : { agent_id: aid };
  const label = agentDisplayTitle(agent);
  if (!window.confirm(`确定删除 Agent「${label}」？\n\n将停止该实例并归档记录，不可恢复。`)) return;
  const deletingCurrent = sessionStore.sessionId === aid;
  sessionStore.error = "";
  agentPanelRef.value?.setDeleting?.(aid);
  try {
    await api.deleteAgent(aid);
    streamHandle.value?.close();
    streamHandle.value = null;

    if (deletingCurrent) {
      finishTurn();
      hitlStore.busy = false;
      clearTranscript();
      clearHitl();
      resetStatusLines();
      resetToolStream();
      resetRemoteWorkers();
      resetEventTracking();
      persistAgentId("");

      const res = await api.listAgents();
      const remaining = (res.agents || []).filter((row) => agentRecordId(row) !== aid);
      if (remaining.length > 0) {
        await switchAgent(agentRecordId(remaining[0]));
        } else {
        syncRouteAgent("");
        chromeStore.sseStatus = "idle";
        agentListCount.value = 0;
      }
    } else {
      await agentPanelRef.value?.refresh?.();
    }
  } catch (e) {
    sessionStore.error = e.message;
    if (deletingCurrent && sessionStore.sessionId === aid) {
      await activateAgentStream();
    }
  } finally {
    agentPanelRef.value?.setDeleting?.("");
  }
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

async function switchLLMProfile(id) {
  const profileId = String(id || "").trim();
  if (!profileId) return;
  if (profileId === chromeStore.llmSettings?.active_profile) return;
  sessionStore.error = "";
  try {
    chromeStore.llmSettings = await api.patchLLMSettings({ active_profile: profileId });
    syncReasoningDisplay(chromeStore.llmSettings);
    try {
      chromeStore.agentInfo = await api.getAgentInfo();
    } catch {
      /* agent info refresh best-effort */
    }
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
    if (!sessionStore.sessionId) {
      sessionStore.error = "请先创建或选择一个 Agent";
      return;
    }
    try {
      await ensureAgent();
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
  if (name === "agents" || name === "sessions") {
    agentPanelRef.value?.refresh?.();
    return;
  }
  if (name === "activity" || name === "changes" || name === "children") {
    chromeStore.panel = name === "changes" ? "activity" : name;
    return;
  }
  if (!sessionStore.sessionId) {
    sessionStore.error = "请先创建或选择一个 Agent";
    return;
  }
  try {
    await ensureAgent();
    chromeStore.panel = name;
  } catch (e) {
    sessionStore.error = e.message;
    chromeStore.panel = null;
  }
}

function closePanel() {
  chromeStore.panel = null;
}

function consumeComposerDraft() {
  try {
    const cmd = sessionStorage.getItem(COMPOSER_DRAFT_KEY);
    if (!cmd) return;
    sessionStorage.removeItem(COMPOSER_DRAFT_KEY);
    nextTick(() => chatPanelRef.value?.setDraft?.(cmd));
  } catch {
    /* ignore */
  }
}


async function bootstrapAgentFromRoute() {
  const fromRoute = String(route.params.agentId || "").trim();
  if (fromRoute) {
    persistAgentId(fromRoute);
    return;
  }
  consumeStartupURL();
  if (sessionStore.sessionId) {
    // 校验持久化 id 是否仍存在
    try {
      await api.getAgent(sessionStore.sessionId);
      syncRouteAgent(sessionStore.sessionId);
    } catch {
      persistAgentId("");
    }
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
  await bootstrapAgentFromRoute();
  await refreshMeta();
  await activateAgentStream();
  refreshContextTokens();
  consumeComposerDraft();
  startDesktopFocusHeartbeat(() => sessionStore.sessionId);
  window.addEventListener("keydown", onKeydown);
  window.addEventListener("pageshow", onPageShow);
});

onActivated(() => {
  if (sessionStore.sessionId && !streamHandle.value) {
    void activateAgentStream();
  }
  consumeComposerDraft();
  startDesktopFocusHeartbeat(() => sessionStore.sessionId);
});

watch(
  () => route.params.agentId,
  async (id, prev) => {
    const aid = String(id || "").trim();
    if (!aid) {
      if (sessionStore.sessionId) {
        // 允许停留在 /agents 空态
        return;
      }
      return;
    }
    if (aid === sessionStore.sessionId || aid === prev) return;
    await switchAgent(aid);
  },
);

onUnmounted(() => {
  stopDesktopFocusHeartbeat();
  streamHandle.value?.close();
  window.removeEventListener("keydown", onKeydown);
  window.removeEventListener("pageshow", onPageShow);
});
</script>

<template>
  <div
    class="app__body app__body--chat-v61"
    :class="{ 'app__body--with-activity': chromeStore.panel === 'activity' }"
  >
    <aside class="app__col app__col--sessions">
      <AgentPanel
        ref="agentPanelRef"
        @switch="switchAgent"
        @create="openCreateWizard()"
        @created="onAgentCreated"
        @delete="deleteAgentById"
        @agents-updated="onAgentsUpdated"
      />
    </aside>

    <div class="app__main-col">
      <div v-if="sessionStore.error" class="chat-error-banner">{{ sessionStore.error }}</div>
      <AgentEmptyState
        v-if="showNoAgentWelcome"
        @create="openCreateWizard()"
        @pick-template="openCreateWizard"
      />
      <div v-else-if="!sessionStore.sessionId" class="chat-empty-agent">
        <p>选择左侧 Agent，或点击 + 从模板新建。</p>
      </div>

      <MainChatPanel
        v-else
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
        @toggle-thinking="toggleThinkingMode"
        @cycle-effort="cycleThinkingEffort"
        @switch-profile="switchLLMProfile"
        @approve-all="(idx) => submitHitlApproval(true, idx)"
        @reject-all="(idx) => submitHitlApproval(false, idx)"
        @approve-one="(payload) => submitHitlOne(payload, true)"
        @reject-one="(payload) => submitHitlOne(payload, false)"
        @user-info-submit="(idx) => submitHitlUserInfo(idx, '')"
        @user-info-selected="(v) => { hitlSelected = v; }"
      />

      <div v-if="chromeStore.panel === 'children'" class="panel-overlay" @click.self="closePanel">
        <ChildrenPanel @close="closePanel" />
      </div>
    </div>

    <aside v-if="chromeStore.panel === 'activity'" class="app__col app__col--activity">
      <ActivityPanel @close="closePanel" />
    </aside>

    <AgentCreateModal
      :open="showAgentCreateModal"
      :initial-template-id="createModalTemplateId"
      @close="onCreateModalClose"
      @created="onAgentCreated"
    />
  </div>
</template>
