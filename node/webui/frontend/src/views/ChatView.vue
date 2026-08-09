<script setup>
import { computed, onMounted, onUnmounted, onActivated, ref, watch, nextTick } from "vue";
import { useRoute, useRouter } from "vue-router";
import * as api from "../api/node.js";
import { connectStream } from "../sse/stream.js";
import MainChatPanel from "../components/MainChatPanel.vue";
import NavRail from "../components/NavRail.vue";
import AgentCreateModal from "../components/AgentCreateModal.vue";
import AgentEmptyState from "../components/AgentEmptyState.vue";
import ChildrenPanel from "../components/ChildrenPanel.vue";
import {
  agentStore,
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
} from "../stores/agent.js";
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
  buildApprovalResume,
  buildApprovalOneResume,
  buildUserInfoSubmitResume,
  buildMemoryConflictResume,
  enqueueHitlRequired,
  shouldSkipChildRuntimeDisplay,
} from "../stores/hitl.js";
import { consumeStartupURL, hydrateAgent } from "../stores/hydrate.js";
import {
  startDesktopFocusHeartbeat,
  stopDesktopFocusHeartbeat,
  pulseDesktopFocus,
} from "../stores/desktopFocus.js";
import {
  refreshToolJobs,
  startToolJobsPolling,
  stopToolJobsPolling,
} from "../stores/toolJobs.js";
import { COMPOSER_DRAFT_KEY } from "../utils/helpCommands.js";
import {
  formatChildLifecycle,
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
  setChildAwaitingApproval,
  syncChildAgentsFromApi,
} from "../stores/remoteWorkers.js";
import { runSlashCommand } from "../utils/commands.js";
import { agentDisplayTitle, agentRecordId } from "../utils/format.js";

const router = useRouter();
const route = useRoute();

const hitlSelected = ref([]);
const cancelling = ref(false);
const streamHandle = ref(null);
const agentPanelRef = ref(null);
const showAgentCreateModal = ref(false);
const createModalTemplateId = ref("");
const agentListCount = ref(null);
const agentList = ref([]);
const currentAgentDisplayName = ref("");
const chatPanelRef = ref(null);
let agentNameSyncToken = 0;

const entries = computed(() => transcriptStore.entries);
const hitlKind = computed(() => peekHitl()?.kind || "");
const hasUserInfoHitl = computed(() => hitlKind.value === "user_information");
const canSend = computed(() => {
  if (hitlStore.busy) return false;
  if (hasUserInfoHitl.value) return true;
  if (hitlKind.value) return false;
  return !agentStore.awaitingTurn;
});
const sending = computed(() => agentStore.awaitingTurn);
const thinkingSupported = computed(() => !!chromeStore.llmSettings?.thinking_supported);
const showNoAgentWelcome = computed(
  () => !agentStore.agentId && agentListCount.value === 0
);
const currentAgentTitle = computed(() => {
  if (!String(agentStore.agentId || "").trim()) return "";
  return String(currentAgentDisplayName.value || "").trim() || "未命名 Agent";
});

const currentAgentRecord = computed(() => {
  const id = String(agentStore.agentId || "").trim();
  if (!id) return null;
  return agentList.value.find((a) => agentRecordId(a) === id) || null;
});

async function syncCurrentAgentDisplayName() {
  const id = String(agentStore.agentId || "").trim();
  const token = ++agentNameSyncToken;
  if (!id) {
    currentAgentDisplayName.value = "";
    return;
  }
  const fromList = agentList.value.find((a) => agentRecordId(a) === id);
  if (fromList) {
    const name = String(fromList.display_name || fromList.DisplayName || "").trim();
    if (token === agentNameSyncToken) {
      currentAgentDisplayName.value = name || "未命名 Agent";
    }
    return;
  }
  try {
    const agent = await api.getAgent(id);
    if (token !== agentNameSyncToken) return;
    const name = String(agent?.display_name || agent?.DisplayName || "").trim();
    currentAgentDisplayName.value = name || "未命名 Agent";
  } catch {
    if (token !== agentNameSyncToken) return;
    currentAgentDisplayName.value = "未命名 Agent";
  }
}

function syncRouteAgent(agentId) {
  const id = String(agentId || "").trim();
  if (route.params.agentId === id) return;
  router.replace({ name: "agents", params: id ? { agentId: id } : {} });
}

function syncReasoningDisplay(_llm) {
  // 思考内容不再在对话区展示；思考开关仅控制 LLM thinking 模式。
  setShowReasoning(false);
}

function restartStream() {
  streamHandle.value?.close();
  if (!agentStore.agentId) {
    streamHandle.value = null;
    return;
  }
  streamHandle.value = connectStream({
    getAgentId: () => agentStore.agentId,
    onStatus: (s) => {
      chromeStore.sseStatus = s;
    },
    onEvent: handleEvent,
  });
}

async function activateAgentStream() {
  if (!agentStore.agentId) {
    clearTranscript();
    clearHitl();
    finishTurn();
    streamHandle.value?.close();
    streamHandle.value = null;
    chromeStore.sseStatus = "idle";
    return;
  }
  const prev = agentStore.agentId;
  await hydrateAgent();
  if (agentStore.agentId !== prev || !streamHandle.value) {
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
  await refreshLLMSettings();
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
      agentStore.error = e2.message || e.message;
    }
  }
}

async function refreshContextTokens() {
  if (!agentStore.agentId) return;
  try {
    const ctx = await api.getAgentContext(agentStore.agentId);
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
      upsertToolCallFromSSE(ev.data);
      refreshToolJobs(agentStore.agentId);
      break;
    case "tool_result":
      markTurnContent();
      finishWaitingStatuses();
      applyToolResult(ev.data);
      refreshToolJobs(agentStore.agentId);
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
      if (agentStore.awaitingTurn) finishTurn();
      break;
    case "done":
      finalizeAssistant();
      finalizeReasoning();
      finishWaitingStatuses();
      finalizePartialToolCalls({ interrupted: true });
      refreshToolJobs(agentStore.agentId);
      if (shouldAcceptDone(ev.seq)) {
        finishTurn();
        resetToolStream();
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
        if (approval?.child_agent_id) setChildAwaitingApproval(approval.child_agent_id, true);
      }
      break;
    case "temporary_agent_created":
      onChildCreated(ev.data);
      addSystem(formatChildLifecycle(ev.type, ev.data));
      break;
    case "temporary_agent_completed":
    case "temporary_agent_cancelled":
      onChildFinished(ev.data?.child_agent_id);
      addSystem(formatChildLifecycle(ev.type, ev.data));
      break;
    case "context_compression_blocking":
    case "context_compression_silent":
      handleCompressionEvent(ev.type, ev.data);
      refreshContextTokens();
      break;
    case "side_effect_turn_start":
      beginImplicitTurn();
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
  if (phase === "start") {
    startStatus("compression");
    return;
  }
  if (phase === "end") {
    finishStatus("compression");
  }
}

async function submitHitlApproval(approveAll, hitlIndex = 0) {
  const item = getHitlAt(hitlIndex);
  if (!item || item.kind !== "approval") return;
  hitlStore.busy = true;
  hitlStore.busyIndex = hitlIndex;
  const resume = buildApprovalResume(item.data, { approveAll });
  try {
    await api.submitResume(agentStore.agentId, resume);
    dequeueHitlAt(hitlIndex);
    if (item.data?.child_agent_id) setChildAwaitingApproval(item.data.child_agent_id, false);
    hitlStore.busy = false;
    hitlStore.busyIndex = -1;
    beginSubmit();
    if (!agentStore.turnContentSeen) startStatus("prefilling");
  } catch (e) {
    agentStore.error = e.message;
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
    await api.submitResume(agentStore.agentId, resume);
    dequeueHitlAt(hitlIndex);
    if (item.data?.child_agent_id) setChildAwaitingApproval(item.data.child_agent_id, false);
    hitlStore.busy = false;
    hitlStore.busyIndex = -1;
    beginSubmit();
    if (!agentStore.turnContentSeen) startStatus("prefilling");
  } catch (e) {
    agentStore.error = e.message;
    hitlStore.busy = false;
    hitlStore.busyIndex = -1;
  }
}

async function submitHitlMemoryConflict(hitlIndex, decision, { cancelled = false } = {}) {
  const item = getHitlAt(hitlIndex);
  if (!item || item.kind !== "memory_conflict") return;
  hitlStore.busy = true;
  hitlStore.busyIndex = hitlIndex;
  const resume = buildMemoryConflictResume(item.data, decision, { cancelled });
  try {
    await api.submitResume(agentStore.agentId, resume);
    dequeueHitlAt(hitlIndex);
    hitlStore.busy = false;
    hitlStore.busyIndex = -1;
    beginSubmit();
    if (!agentStore.turnContentSeen) startStatus("prefilling");
  } catch (e) {
    agentStore.error = e.message;
    hitlStore.busy = false;
    hitlStore.busyIndex = -1;
  }
}

function onHitlUserInfoSelected(v) {
  // 必须在 <script> 里写 ref.value；模板中顶层 ref 已自动解包，写 .value 不会更新选中态。
  hitlSelected.value = Array.isArray(v) ? [...v] : Number(v);
}

async function submitHitlUserInfo(hitlIndex, text) {
  const item = getHitlAt(hitlIndex);
  if (!item || item.kind !== "user_information") return;
  const built = buildUserInfoSubmitResume(item.data, {
    text,
    selected: hitlSelected.value,
  });
  if (!built.ok) {
    agentStore.error = built.error;
    return;
  }
  const resume = built.resume;
  hitlStore.busy = true;
  hitlStore.busyIndex = hitlIndex;
  try {
    await api.submitResume(agentStore.agentId, resume);
    dequeueHitlAt(hitlIndex);
    if (item.data?.child_agent_id) setChildAwaitingApproval(item.data.child_agent_id, false);
    hitlStore.busy = false;
    hitlStore.busyIndex = -1;
    hitlSelected.value = [];
    beginSubmit();
    if (!agentStore.turnContentSeen) startStatus("prefilling");
  } catch (e) {
    agentStore.error = e.message;
    hitlStore.busy = false;
    hitlStore.busyIndex = -1;
  }
}

async function onSendMessage(payload) {
  agentStore.error = "";
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
  if (hitl?.kind === "memory_conflict") {
    agentStore.error = "请先处理长期记忆冲突确认";
    return;
  }

  if (agentStore.awaitingTurn) {
    agentStore.error = "上一回合尚未结束";
    return;
  }

  await activateAgentStream();
  clearHitl();
  addUser(text, images);
  beginSubmit();
  try {
    await api.submitMessage(agentStore.agentId, text, contentParts);
    if (!agentStore.turnContentSeen) startStatus("prefilling");
  } catch (e) {
    finishStatus("prefilling");
    finishTurn();
    agentStore.error = e.message;
  }
}

async function handleCommand(cmd) {
  const res = await runSlashCommand(cmd);
  if (res.error) {
    agentStore.error = res.error;
    return;
  }
  if (res.action === "clear") {
    await api.clearContext(await ensureAgent());
    await hydrateAgent();
    restartStream();
    resetStatusLines();
    resetToolStream();
    resetUsageStrip();
    chromeStore.contextTokens = 0;
    addSystem("已清空对话上下文");
    return;
  }
  if (res.action === "compress") {
    startStatus("compression");
    try {
      const out = await api.compressContext(await ensureAgent());
      if (out.status && out.status !== "applied" && out.status !== "done") {
        addSystem(`压缩: ${out.status}`);
      }
    } finally {
      finishStatus("compression");
    }
    refreshContextTokens();
    return;
  }
  if (res.action === "thinking") {
    await handleThinkingCommand(res.arg);
    return;
  }
  if (res.action === "upload") {
    await handleUploadCommand(res.upload);
    return;
  }
}

async function handleUploadCommand(spec) {
  if (!spec || spec.error) {
    agentStore.error = spec?.error || "upload 参数无效";
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
      agentStore.error = `未知 upload 类型: ${kind}`;
      return;
    }
    const note = out.message ? ` — ${out.message}` : "";
    addSystem(`已上传 ${out.kind} ${out.id}@${out.version}（${out.status}${note}）`);
  } catch (err) {
    agentStore.error = err.message;
  }
}

async function openCreateWizard(templateId = "") {
  createModalTemplateId.value = String(templateId || "").trim();
  showAgentCreateModal.value = true;
}

function onAgentsUpdated(list) {
  agentList.value = Array.isArray(list) ? list.slice() : [];
  agentListCount.value = agentList.value.length;
  void syncCurrentAgentDisplayName();
}

function onCreateModalClose() {
  showAgentCreateModal.value = false;
  createModalTemplateId.value = "";
}

async function onAgentCreated(created) {
  const id = agentRecordId(created);
  if (!id) return;
  const createdName = String(created?.display_name || created?.DisplayName || "").trim();
  if (createdName) currentAgentDisplayName.value = createdName;
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
    await hydrateAgent();
    await refreshLLMSettings();
  } catch (e) {
    agentStore.error = e.message;
  }
  await syncCurrentAgentDisplayName();
  await nextTick();
  chatPanelRef.value?.scrollToTail?.();
}

async function switchAgent(id) {
  persistAgentId(id);
  resetStatusLines();
  resetToolStream();
  resetRemoteWorkers();
  resetUsageStrip();
  void syncCurrentAgentDisplayName();
  try {
    await hydrateAgent();
    await refreshLLMSettings();
  } catch (e) {
    agentStore.error = e.message;
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
  await syncCurrentAgentDisplayName();
  await nextTick();
  chatPanelRef.value?.scrollToTail?.();
}

async function deleteAgentById(payload) {
  const aid = String(typeof payload === "string" ? payload : payload?.id || "").trim();
  if (!aid) {
    agentStore.error = "无法删除：Agent ID 无效";
    return;
  }
  const agent = typeof payload === "object" && payload?.agent ? payload.agent : { agent_id: aid };
  const label = agentDisplayTitle(agent);
  if (!window.confirm(`确定删除 Agent「${label}」？\n\n将停止该实例并归档记录，不可恢复。`)) return;
  const deletingCurrent = agentStore.agentId === aid;
  agentStore.error = "";
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
        agentList.value = [];
        agentListCount.value = 0;
        currentAgentDisplayName.value = "";
        // 最后一个 Agent 被删时 switchAgent 不会跑，需显式刷新侧栏列表
        await agentPanelRef.value?.refresh?.();
      }
    } else {
      await agentPanelRef.value?.refresh?.();
    }
  } catch (e) {
    agentStore.error = e.message;
    if (deletingCurrent && agentStore.agentId === aid) {
      await activateAgentStream();
    }
  } finally {
    agentPanelRef.value?.setDeleting?.("");
  }
}

async function toggleThinkingMode() {
  if (!chromeStore.llmSettings?.thinking_supported) {
    agentStore.error = "当前 provider 不支持 thinking 控制（需 deepseek 或 qwen）";
    return;
  }
  agentStore.error = "";
  const t = String(chromeStore.llmSettings.thinking || "").toLowerCase();
  const enabled = !["disabled", "off"].includes(t);
  try {
    chromeStore.llmSettings = await api.patchLLMSettings({ thinking: enabled ? "disabled" : "enabled" });
    syncReasoningDisplay(chromeStore.llmSettings);
  } catch (e) {
    agentStore.error = e.message;
  }
}

async function cycleThinkingEffort() {
  if (!chromeStore.llmSettings?.thinking_supported) return;
  agentStore.error = "";
  const current = String(chromeStore.llmSettings.reasoning_effort || "high").toLowerCase();
  const next = current === "max" ? "high" : "max";
  try {
    chromeStore.llmSettings = await api.patchLLMSettings({ reasoning_effort: next });
  } catch (e) {
    agentStore.error = e.message;
  }
}

async function refreshLLMSettings() {
  try {
    chromeStore.llmSettings = await api.getLLMSettings();
    syncReasoningDisplay(chromeStore.llmSettings);
  } catch {
    /* best-effort */
  }
}

async function switchLLMProfile(id) {
  const profileId = String(id || "").trim();
  if (!profileId) return;
  if (!agentStore.agentId) {
    agentStore.error = "请先选择 Agent";
    return;
  }
  if (profileId === chromeStore.llmSettings?.active_profile) return;
  agentStore.error = "";
  try {
    // 绑定到当前 Agent；ensure/reload 时会应用到进程 LLM（含多模态）。
    await api.patchAgent(agentStore.agentId, { llm_active: profileId });
    await refreshLLMSettings();
    try {
      chromeStore.agentInfo = await api.getAgentInfo();
    } catch {
      /* agent info refresh best-effort */
    }
  } catch (e) {
    agentStore.error = e.message;
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
    agentStore.error = "用法: /thinking on|off 或 /thinking effort high|max";
    return;
  }
  chromeStore.llmSettings = await api.patchLLMSettings(patch);
  addSystem(`thinking: ${chromeStore.llmSettings.thinking || "-"}`);
}

function openLeftActivity() {
  agentPanelRef.value?.expandSection?.("activity");
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
  if (agentStore.agentId) {
    // 校验持久化 id 是否仍存在
    try {
      await api.getAgent(agentStore.agentId);
      syncRouteAgent(agentStore.agentId);
    } catch {
      persistAgentId("");
    }
  }
}

async function cancelTurn() {
  if (!agentStore.agentId || cancelling.value || !agentStore.awaitingTurn) return;
  cancelling.value = true;
  agentStore.error = "";
  try {
    finishWaitingStatuses();
    finalizePartialToolCalls({ interrupted: true });
    await api.cancelAgentTurn(agentStore.agentId);
    finishTurn();
    clearHitl();
    finalizeAssistant();
    finalizeReasoning();
    resetToolStream();
    addSystem("turn 已取消");
  } catch (e) {
    agentStore.error = e.message;
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
  await syncCurrentAgentDisplayName();
  await refreshMeta();
  await activateAgentStream();
  // ensure 会按 Agent 绑定应用 LLM；再拉一次设置，避免刷新后仍显示进程默认档案。
  await refreshLLMSettings();
  refreshContextTokens();
  consumeComposerDraft();
  startDesktopFocusHeartbeat(() => agentStore.agentId);
  startToolJobsPolling(() => agentStore.agentId);
  window.addEventListener("keydown", onKeydown);
  window.addEventListener("pageshow", onPageShow);
});

watch(
  () => agentStore.agentId,
  () => {
    void syncCurrentAgentDisplayName();
  },
);

onActivated(() => {
  if (agentStore.agentId && !streamHandle.value) {
    void activateAgentStream();
  }
  consumeComposerDraft();
  startDesktopFocusHeartbeat(() => agentStore.agentId);
});

watch(
  () => route.params.agentId,
  async (id, prev) => {
    const aid = String(id || "").trim();
    if (!aid) {
      if (agentStore.agentId) {
        // 允许停留在 /agents 空态
        return;
      }
      return;
    }
    if (aid === agentStore.agentId || aid === prev) return;
    await switchAgent(aid);
  },
);

watch(
  () => route.query.createAgent,
  (v) => {
    if (String(v || "") === "1") {
      openCreateWizard();
      const q = { ...route.query };
      delete q.createAgent;
      router.replace({ query: q });
    }
  },
  { immediate: true },
);

onUnmounted(() => {
  stopDesktopFocusHeartbeat();
  stopToolJobsPolling();
  streamHandle.value?.close();
  window.removeEventListener("keydown", onKeydown);
  window.removeEventListener("pageshow", onPageShow);
});
</script>

<template>
  <div class="app__body app__body--chat-v61">
    <aside class="app__col app__col--agents">
      <NavRail
        ref="agentPanelRef"
        @switch="switchAgent"
        @create="openCreateWizard()"
        @delete="deleteAgentById"
        @agents-updated="onAgentsUpdated"
        @create-member="
          (id) =>
            router.push({
              name: 'workgroups',
              params: { workgroupId: id },
              query: { createMember: '1' },
            })
        "
        @configure-member="
          (p) =>
            router.push({
              name: 'workgroups',
              params: { workgroupId: p.workgroupId },
              query: { member: p.memberId, editMember: '1' },
            })
        "
      />
    </aside>

    <div class="app__main-col">
      <div v-if="agentStore.error" class="chat-error-banner">{{ agentStore.error }}</div>
      <AgentEmptyState
        v-if="showNoAgentWelcome"
        @create="openCreateWizard()"
        @pick-template="openCreateWizard"
      />
      <div v-else-if="!agentStore.agentId" class="chat-empty-agent">
        <p>选择左侧 Agent，或点击 + 从模板新建。</p>
      </div>

      <MainChatPanel
        v-else
        ref="chatPanelRef"
        :entries="entries"
        :hitl-queue="hitlStore.queue"
        :tool-verbose="transcriptStore.toolFoldVerbose"
        :disabled="!canSend"
        :sending="sending"
        :cancelling="cancelling"
        :hitl-busy="hitlStore.busy"
        :hitl-busy-index="hitlStore.busyIndex"
        :thinking-supported="thinkingSupported"
        :llm-settings="chromeStore.llmSettings"
        :agent-title="currentAgentTitle"
        @send="onSendMessage"
        @cancel="cancelTurn"
        @toggle-thinking="toggleThinkingMode"
        @cycle-effort="cycleThinkingEffort"
        @switch-profile="switchLLMProfile"
        @open-activity="openLeftActivity"
        @approve-all="(idx) => submitHitlApproval(true, idx)"
        @reject-all="(idx) => submitHitlApproval(false, idx)"
        @approve-one="(payload) => submitHitlOne(payload, true)"
        @reject-one="(payload) => submitHitlOne(payload, false)"
        @user-info-submit="(idx) => submitHitlUserInfo(idx, '')"
        @user-info-selected="onHitlUserInfoSelected"
        @memory-conflict-decide="(payload) => submitHitlMemoryConflict(payload.index, payload.decision)"
        @memory-conflict-cancel="(idx) => submitHitlMemoryConflict(idx, 'cancelled', { cancelled: true })"
      />

      <div v-if="chromeStore.panel === 'children'" class="panel-overlay" @click.self="closePanel">
        <ChildrenPanel @close="closePanel" />
      </div>
    </div>

    <AgentCreateModal
      :open="showAgentCreateModal"
      :initial-template-id="createModalTemplateId"
      @close="onCreateModalClose"
      @created="onAgentCreated"
    />
  </div>
</template>
