async function readJSON(response) {
  try {
    return await response.json();
  } catch {
    return null;
  }
}

async function apiFetch(path, { method = "GET", body, params, signal } = {}) {
  const url = new URL(path, window.location.origin);
  if (params) {
    Object.entries(params).forEach(([k, v]) => {
      if (v !== undefined && v !== null && v !== "") url.searchParams.set(k, String(v));
    });
  }
  const headers = { Accept: "application/json" };
  const init = { method, headers };
  if (signal) init.signal = signal;
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(body);
  }
  // Desktop supplies this transport through Tauri so settings keep using the
  // same Node-owned API without an HTTP port exposed to the WebView.
  if (typeof globalThis.__DAGENTS_API_REQUEST__ === "function") {
    return globalThis.__DAGENTS_API_REQUEST__({
      method,
      path: `${url.pathname}${url.search}`,
      body,
    });
  }
  const resp = await fetch(url, init);
  const data = await readJSON(resp);
  if (!resp.ok) {
    const msg = data?.error?.message || data?.message || `HTTP ${resp.status}`;
    const error = new Error(msg);
    error.status = resp.status;
    error.data = data;
    error.response = { status: resp.status };
    throw error;
  }
  return data;
}

export function getHealth() {
  return apiFetch("/health");
}

export function getAgentInfo() {
  return apiFetch("/v1/node/info");
}

// Lightweight, content-free conversation revisions used to detect missed SSE
// delivery without hydrating every background Agent.
export function getAgentWatermarks() {
  return apiFetch("/v1/agents/watermarks");
}

// 用户反馈由 Node 权威保存并负责向当前 Manage 投递；Desktop 只保存未提交草稿。
export function listFeedback(params = {}) {
  return apiFetch("/v1/feedback", { params });
}

export function getFeedbackTarget() {
  return apiFetch("/v1/feedback/target");
}

export function getFeedback(feedbackId) {
  return apiFetch(`/v1/feedback/${encodeURIComponent(feedbackId)}`);
}

export function createFeedback(payload = {}) {
  return apiFetch("/v1/feedback", { method: "POST", body: payload });
}

export function syncFeedback(feedbackId) {
  return apiFetch(`/v1/feedback/${encodeURIComponent(feedbackId)}/sync`, {
    method: "POST",
    body: {},
  });
}

/** 聚合 health + agent/info + llm/settings（Chat 首屏）。 */
export function getUIBootstrap() {
  return apiFetch("/v1/bootstrap");
}

export function getAgentUpdate() {
  return apiFetch("/v1/node/update");
}

/** 当前 Node 可用的宿主/桌面能力；UI 不直接探测 Shell 端口。 */
export function getPlatformCapabilities() {
  return apiFetch("/v1/platform/capabilities").then(async (node) => {
    const desktopRequest = globalThis.__DAGENTS_DESKTOP_REQUEST__;
    if (typeof desktopRequest !== "function") return node;
    try {
      const desktop = await desktopRequest({ method: "GET", path: "/v1/platform/capabilities" });
      return { ...node, ...desktop };
    } catch {
      return node;
    }
  });
}

export function pickPlatformDirectory() {
  const desktopPicker = globalThis.__DAGENTS_PICK_DIRECTORY__;
  if (typeof desktopPicker === "function") return desktopPicker();
  return apiFetch("/v1/platform/directory-picker", { method: "POST", body: {} });
}

export function getPlatformClipboardFiles() {
  const desktopRequest = globalThis.__DAGENTS_DESKTOP_REQUEST__;
  if (typeof desktopRequest !== "function") {
    return Promise.resolve({ available: false, files: [] });
  }
  return desktopRequest({ method: "GET", path: "/v1/platform/clipboard/files" });
}

export function reportPlatformUIFocus(payload = {}) {
  const desktopRequest = globalThis.__DAGENTS_DESKTOP_REQUEST__;
  if (typeof desktopRequest !== "function") return Promise.resolve({ ok: false });
  return desktopRequest({ method: "POST", path: "/v1/platform/ui-focus", body: payload });
}

export function applyAgentUpdate({ force = false } = {}) {
  const desktopRequest = globalThis.__DAGENTS_DESKTOP_REQUEST__;
  if (typeof desktopRequest !== "function") return Promise.reject(new Error("Desktop updater is unavailable"));
  return desktopRequest({ method: "POST", path: "/v1/platform/update/apply", body: { force } });
}


export function getLLMSettings() {
  return apiFetch("/v1/llm/settings");
}

export function getLLMCapabilities(provider, model) {
  return apiFetch("/v1/llm/capabilities", { params: { provider, model } });
}

export function getSetupConfig() {
  return apiFetch("/v1/settings");
}

export function patchSetupConfig(patch) {
  return apiFetch("/v1/settings", { method: "PATCH", body: patch });
}

/** 用 base_url + api_key 探测 OpenAI 兼容 /models 列表。 */
export function probeLLMModels(payload = {}) {
  return apiFetch("/v1/llm/probe-models", { method: "POST", body: payload });
}

export function listAgentTemplates() {
  return apiFetch("/v1/agent-templates");
}

export function getAgentTemplate(templateId) {
  return apiFetch(`/v1/agent-templates/${encodeURIComponent(templateId)}`);
}

export function createAgentTemplate(payload = {}) {
  return apiFetch("/v1/agent-templates", { method: "POST", body: payload });
}

export function deleteAgentTemplate(templateId) {
  return apiFetch(`/v1/agent-templates/${encodeURIComponent(templateId)}`, { method: "DELETE" });
}

export function createAgent(payload = {}) {
  const body = {};
  if (payload.template_id != null || payload.templateId != null) {
    const tid = String(payload.template_id ?? payload.templateId ?? "").trim();
    if (tid) body.template_id = tid;
  }
  if (payload.display_name != null || payload.displayName != null) {
    const name = String(payload.display_name ?? payload.displayName ?? "").trim();
    if (name) body.display_name = name;
  }
  if (payload.defaults && typeof payload.defaults === "object") body.defaults = payload.defaults;
  if (payload.workspace && typeof payload.workspace === "object") body.workspace = payload.workspace;
  if (payload.agent_type === "auto" || payload.agent_type === "normal") body.agent_type = payload.agent_type;
  return apiFetch("/v1/agents", { method: "POST", body });
}

export function listAgents() {
  return apiFetch("/v1/agents");
}

export function getAgent(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}`);
}

export function getAgentModelView(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/model-view`);
}

export function getAgentHandbook(agentId, path = "") {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/handbook`, { params: { path } });
}
export function getAgentHandbookHistory(agentId, path) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/handbook/history`, { params: { path } });
}
export function restoreAgentHandbook(agentId, payload = {}) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/handbook/restore`, { method: "POST", body: payload });
}
// Simplified Auto configuration API. These endpoints intentionally do not
// depend on the retired Goal/Cycle autonomy resources.
export function getAutoConfig(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/auto-config`);
}

export function getAutoSettings(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/auto-settings`);
}

export function putAutoConfig(agentId, payload = {}) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/auto-config`, { method: "PUT", body: payload });
}
export function reconcileAutoConfig(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/auto-config/reconcile`, { method: "POST", body: {} });
}

export function listAgentTodos(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/todos`);
}

export function createAgentTodo(agentId, payload = {}) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/todos`, { method: "POST", body: payload });
}

export function updateAgentTodo(agentId, todoId, payload = {}) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/todos/${encodeURIComponent(todoId)}`, { method: "PATCH", body: payload });
}

export function getAutoExperience(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/experience`);
}

export function getAgentDreamingStatus(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/dreaming`);
}

export function deleteAgentTodo(agentId, todoId, revision) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/todos/${encodeURIComponent(todoId)}`, { method: "DELETE", body: { expected_revision: revision } });
}

export function getAutoOverview(params = {}) {
  return apiFetch("/v1/auto/overview", { params });
}

/** Read-only browser page view for an already-running Agent browser session. */
export function getAgentBrowserView(agentId, pageId = "") {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/browser/view`, {
    params: { page_id: pageId || undefined },
  });
}

/** Explicit user navigation in the same browser session used by the Agent. */
export function controlAgentBrowser(agentId, command, pageId = "", url = "") {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/browser/control`, {
    method: "POST", body: { command, page_id: pageId, url },
  });
}

export function patchAgent(agentId, patch = {}) {
  const body = {};
  if (patch.display_name != null || patch.displayName != null) {
    body.display_name = patch.display_name ?? patch.displayName;
  }
  if (patch.defaults && typeof patch.defaults === "object") body.defaults = patch.defaults;
  if (patch.agent_type === "auto" || patch.agent_type === "normal") body.agent_type = patch.agent_type;
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}`, { method: "PATCH", body });
}

export function deleteAgent(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}`, { method: "DELETE" });
}

export function ensureAgentRuntime(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/ensure`, { method: "POST", body: {} });
}

export function reloadAgentRuntime(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/reload`, { method: "POST", body: {} });
}

export function getAgentHydrate(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/hydrate`);
}

export function listConversationRounds(agentId, conversationId = agentId, { beforeRoundId = "", limit = 10 } = {}) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/conversations/${encodeURIComponent(conversationId)}/rounds`, {
    params: { before_round_id: beforeRoundId, limit },
  });
}

export function listConversationRoundRevisions(agentId, conversationId, roundId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/conversations/${encodeURIComponent(conversationId)}/rounds/${encodeURIComponent(roundId)}/revisions`);
}

export function listConversationRoundItems(agentId, conversationId, roundId, { revisionId = "", afterItemId = "", limit = 50 } = {}) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/conversations/${encodeURIComponent(conversationId)}/rounds/${encodeURIComponent(roundId)}/items`, {
    params: { revision_id: revisionId, after_item_id: afterItemId, limit },
  });
}

export function listConversationRoundToolSteps(agentId, conversationId, roundId, { revisionId = "", sourceItemId = "", afterToolCallId = "", limit = 50 } = {}) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/conversations/${encodeURIComponent(conversationId)}/rounds/${encodeURIComponent(roundId)}/tool-steps`, {
    params: { revision_id: revisionId, source_item_id: sourceItemId, after_tool_call_id: afterToolCallId, limit },
  });
}

export function getConversationRoundToolStep(agentId, conversationId, roundId, toolCallId, { revisionId = "", sourceItemId = "" } = {}) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/conversations/${encodeURIComponent(conversationId)}/rounds/${encodeURIComponent(roundId)}/tool-steps/${encodeURIComponent(toolCallId)}`, {
    params: { revision_id: revisionId, source_item_id: sourceItemId },
  });
}

export function getConversationItemContent(agentId, conversationId, roundId, itemId, { revisionId = "", part = "content" } = {}) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/conversations/${encodeURIComponent(conversationId)}/rounds/${encodeURIComponent(roundId)}/items/${encodeURIComponent(itemId)}/content`, {
    params: { revision_id: revisionId, part },
  });
}

export function listConversationLegacyItems(agentId, conversationId = agentId, { afterItemId = "", limit = 50 } = {}) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/conversations/${encodeURIComponent(conversationId)}/legacy-items`, {
    params: { after_item_id: afterItemId, limit },
  });
}

export function getConversationResubmitTarget(agentId, conversationId, roundId, revisionId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/conversations/${encodeURIComponent(conversationId)}/rounds/${encodeURIComponent(roundId)}/resubmit-target`, {
    params: { revision_id: revisionId },
  });
}

export function getAgentContext(agentId, { fullMessages = false } = {}) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/context`, {
    params: fullMessages ? { full_messages: "1" } : {},
  });
}

export function cancelAgentTurn(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/cancel`, { method: "POST", body: {} });
}

export function setInputQueuePaused(agentId, paused, expectedRevision) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/inputs/control`, {
    method: "POST",
    body: { paused: Boolean(paused), expected_revision: Number(expectedRevision) || 0 },
  });
}

export function listTransfers(agentId = "") {
  return apiFetch("/v1/transfers", { params: agentId ? { agent_id: agentId } : {} });
}

export function cancelTransfer(transferId) {
  return apiFetch(`/v1/transfers/${encodeURIComponent(transferId)}/cancel`, {
    method: "POST",
    body: {},
  });
}

export function listAgentTerminals(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/terminals`);
}

export function closeAgentTerminal(agentId, terminalId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/terminals/${encodeURIComponent(terminalId)}`, { method: "DELETE" });
}

export function clearContext(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/clear-context`, { method: "POST", body: {} });
}

export function compressContext(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/compress`, { method: "POST", body: {} });
}

export function postAgentAck(agentId, agentSeq) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/ack`, {
    method: "POST",
    body: { agent_seq: agentSeq },
  });
}

function makeClientRequestId() {
  if (globalThis.crypto?.randomUUID) return globalThis.crypto.randomUUID()
  return `msg-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}-${Math.random().toString(36).slice(2)}`
}

export function newClientRequestId() {
  return makeClientRequestId()
}

function stableStringify(value) {
  if (Array.isArray(value)) return `[${value.map(stableStringify).join(',')}]`
  if (value && typeof value === 'object') {
    return `{${Object.keys(value).sort().map(key => `${JSON.stringify(key)}:${stableStringify(value[key])}`).join(',')}}`
  }
  return JSON.stringify(value)
}

async function messageFingerprint(body) {
  const text = stableStringify(body)
  if (globalThis.crypto?.subtle && globalThis.TextEncoder) {
    const bytes = new TextEncoder().encode(text)
    const digest = await globalThis.crypto.subtle.digest('SHA-256', bytes)
    return [...new Uint8Array(digest)].map(byte => byte.toString(16).padStart(2, '0')).join('')
  }
  // This fallback only indexes a session-local retry hint; Node performs the
  // authoritative SHA-256 payload check before accepting a duplicate.
  let a = 0x811c9dc5
  let b = 0x9e3779b9
  for (let index = 0; index < text.length; index++) {
    const code = text.charCodeAt(index)
    a = Math.imul(a ^ code, 0x01000193)
    b = Math.imul(b ^ code, 0x85ebca6b)
  }
  return `${(a >>> 0).toString(16)}${(b >>> 0).toString(16)}`
}

function pendingMessageStorageKey(agentId) {
  return `dagents.pending-message.${encodeURIComponent(agentId)}`
}

function readPendingMessageIds(agentId) {
  try {
    const parsed = JSON.parse(globalThis.sessionStorage?.getItem(pendingMessageStorageKey(agentId)) || '[]')
    return Array.isArray(parsed) ? parsed.filter(item => item && typeof item.id === 'string' && typeof item.fingerprint === 'string') : []
  } catch {
    return []
  }
}

function writePendingMessageIds(agentId, entries) {
  try {
    if (!globalThis.sessionStorage) return
    const bounded = entries.slice(-32)
    if (bounded.length) globalThis.sessionStorage.setItem(pendingMessageStorageKey(agentId), JSON.stringify(bounded))
    else globalThis.sessionStorage.removeItem(pendingMessageStorageKey(agentId))
  } catch {
    // The durable Node receipt remains authoritative; sessionStorage only lets
    // an in-window retry reuse the same request ID after an uncertain outcome.
  }
}

function forgetPendingMessageId(agentId, requestId) {
  writePendingMessageIds(agentId, readPendingMessageIds(agentId).filter(item => item.id !== requestId))
}

function receiptAsAccepted(agentId, receipt) {
  return {
    accepted: true,
    agent_id: agentId,
    input_id: receipt.input_id,
    input_seq: receipt.input_seq,
    input_revision: receipt.input_revision,
    status: receipt.status,
    conversation_epoch: receipt.conversation_epoch,
    duplicate: true,
  }
}

export function getInputReceipt(agentId, clientRequestId, conversationEpoch) {
  const params = new URLSearchParams({ client_request_id: clientRequestId, conversation_epoch: String(conversationEpoch) })
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/inputs?${params.toString()}`)
}

async function submitIdempotentInput(agentId, endpoint, body, { clientRequestId: requestedId = "", onClientRequestId } = {}) {
  const fingerprint = await messageFingerprint({ endpoint, body })
  const pending = readPendingMessageIds(agentId)
  const existing = pending.find(item => item.fingerprint === fingerprint && item.conversation_epoch === body.conversation_epoch)
  const clientRequestId = existing?.id || String(requestedId || "").trim() || makeClientRequestId()
  if (!existing) {
    pending.push({ id: clientRequestId, fingerprint, conversation_epoch: body.conversation_epoch })
    writePendingMessageIds(agentId, pending)
  }
  onClientRequestId?.(clientRequestId)
  body.client_request_id = clientRequestId

  const post = () => apiFetch(endpoint, { method: "POST", body })
  const reconcile = async () => {
    const receipt = await getInputReceipt(agentId, clientRequestId, body.conversation_epoch)
    return receipt?.found ? receiptAsAccepted(agentId, receipt) : null
  }
  const isDefiniteRejection = error => Number(error?.status) >= 400 && Number(error?.status) < 500
  try {
    const result = await post()
    forgetPendingMessageId(agentId, clientRequestId)
    return result
  } catch (firstError) {
    if (isDefiniteRejection(firstError)) {
      forgetPendingMessageId(agentId, clientRequestId)
      throw firstError
    }
    try {
      const recovered = await reconcile()
      if (recovered) {
        forgetPendingMessageId(agentId, clientRequestId)
        return recovered
      }
    } catch {
      firstError.unknownSubmission = true
      throw firstError
    }
    try {
      const retried = await post()
      forgetPendingMessageId(agentId, clientRequestId)
      return retried
    } catch (retryError) {
      if (isDefiniteRejection(retryError)) {
        forgetPendingMessageId(agentId, clientRequestId)
        throw retryError
      }
      try {
        const recovered = await reconcile()
        if (recovered) {
          forgetPendingMessageId(agentId, clientRequestId)
          return recovered
        }
      } catch {
        // Keep the request ID in sessionStorage. A later identical send will
        // reconcile or replay the same ID rather than silently creating a
        // second accepted input.
      }
      retryError.unknownSubmission = true
      throw retryError
    }
  }
}

export async function submitMessage(agentId, content, contentParts = null, fileRefs = null, {
  conversationEpoch = 1,
  clientRequestId = "",
  onClientRequestId,
} = {}) {
  const body = { agent_id: agentId, request_type: "message", content: content || "" };
  if (Array.isArray(contentParts) && contentParts.length) body.content_parts = contentParts;
  if (Array.isArray(fileRefs) && fileRefs.length) {
    body.file_refs = fileRefs.map((file) => ({
      path: file?.path || "",
      name: file?.name || "",
      ...(file?.id ? { id: file.id } : {}),
      ...(file?.revision ? { revision: file.revision } : {}),
      ...(file?.draft ? { draft: true } : {}),
      ...(file?.annotation ? { annotation: file.annotation } : {}),
      ...(file?.anchor && typeof file.anchor === "object" ? { anchor: file.anchor } : {}),
    }));
  }
  body.conversation_epoch = Math.max(1, Number(conversationEpoch) || 1)
  return submitIdempotentInput(agentId, "/v1/messages", body, { clientRequestId, onClientRequestId })
}

export async function submitConversationResubmit(agentId, roundId, {
  revisionId,
  conversationRevision,
  conversationEpoch,
  preCheckpointRevision,
  content,
  contentParts = null,
  fileRefs = null,
  userMessageName = "",
} = {}) {
  const body = {
    expected_selected_revision_id: String(revisionId || ""),
    expected_conversation_revision: Number(conversationRevision) || 0,
    pre_checkpoint_revision: Number(preCheckpointRevision) || 0,
    conversation_epoch: Math.max(1, Number(conversationEpoch) || 1),
    content: String(content || ""),
  }
  if (Array.isArray(contentParts) && contentParts.length) body.content_parts = contentParts
  if (Array.isArray(fileRefs) && fileRefs.length) body.file_refs = fileRefs
  if (userMessageName) body.user_message_name = userMessageName
  const endpoint = `/v1/agents/${encodeURIComponent(agentId)}/conversations/${encodeURIComponent(agentId)}/rounds/${encodeURIComponent(roundId)}/resubmit`
  return submitIdempotentInput(agentId, endpoint, body)
}

export function editQueuedMessage(agentId, inputSeq, { expectedRevision, content, contentParts, fileRefs, userMessageName } = {}) {
  const body = { expected_revision: expectedRevision, content: content || "" };
  if (Array.isArray(contentParts)) body.content_parts = contentParts;
  if (Array.isArray(fileRefs)) body.file_refs = fileRefs;
  if (userMessageName) body.user_message_name = userMessageName;
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/inputs/${encodeURIComponent(inputSeq)}`, {
    method: "PATCH",
    body,
  });
}

export function withdrawQueuedMessage(agentId, inputSeq, expectedRevision) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/inputs/${encodeURIComponent(inputSeq)}`, {
    method: "DELETE",
    body: { expected_revision: expectedRevision },
  });
}

export function submitResume(agentId, resumeValue) {
  return apiFetch("/v1/messages", {
    method: "POST",
    body: { agent_id: agentId, request_type: "resume", resume_value: resumeValue },
  });
}

export function listSkills(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/skills`);
}

/** Node 级 skills 目录（创建/编辑 Agent 勾选可见技能用，不受 Agent 白名单过滤）。 */
export function listNodeSkillsCatalog() {
  return apiFetch("/v1/skills/catalog");
}

export function loadSkill(agentId, skillName) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/skills/load`, {
    method: "POST",
    body: { skill_name: skillName },
  });
}

export function unloadSkill(agentId, skillName) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/skills/unload`, {
    method: "POST",
    body: { skill_name: skillName },
  });
}

export function cancelChildAgent(agentId, childAgentId, reason = "") {
  const body = reason ? { reason } : {};
  return apiFetch(
    `/v1/agents/${encodeURIComponent(agentId)}/child-agents/${encodeURIComponent(childAgentId)}/cancel`,
    { method: "POST", body },
  );
}

export function getPolicy(agentId, shellQuery) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/policy`, {
    params: shellQuery ? { shell: shellQuery } : {},
  });
}

export function updateToolPolicy(agentId, updates) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/policy/tools`, {
    method: "PUT",
    body: { updates },
  });
}

export function updateShellPolicy(agentId, shellType, updates, deletes = []) {
  const body = { updates: updates || [] };
  if (Array.isArray(deletes) && deletes.length) body.deletes = deletes;
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/policy/shell/${encodeURIComponent(shellType)}`, {
    method: "PUT",
    body,
  });
}

export function getAgentPromptContext(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/prompt-context`);
}

export function putAgentPromptContext(agentId, body) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/prompt-context`, {
    method: "PUT",
    body,
  });
}

export function patchAgentMemoryEntry(agentId, scope, entryId, content) {
  return apiFetch(
    `/v1/agents/${encodeURIComponent(agentId)}/prompt-context/memory/${encodeURIComponent(entryId)}`,
    {
      method: "PATCH",
      body: { scope, content },
    },
  );
}

export function deleteAgentMemoryEntry(agentId, scope, entryId) {
  return apiFetch(
    `/v1/agents/${encodeURIComponent(agentId)}/prompt-context/memory/${encodeURIComponent(entryId)}?scope=${encodeURIComponent(scope)}`,
    { method: "DELETE" },
  );
}

export function listMcpServers() {
  return apiFetch("/v1/mcp/servers");
}

export function getMcpStatus() {
  return apiFetch("/v1/mcp/status");
}

export function getMcpConfig() {
  return apiFetch("/v1/mcp/config");
}

export function saveMcpConfig(configText) {
  return apiFetch("/v1/mcp/config", {
    method: "PUT",
    body: { config_text: String(configText || "") },
  });
}

export function createMcpServer(payload = {}) {
  return apiFetch("/v1/mcp/servers", { method: "POST", body: payload });
}

export function patchMcpServer(serverId, payload = {}) {
  return apiFetch(`/v1/mcp/servers/${encodeURIComponent(serverId)}`, { method: "PATCH", body: payload });
}

export function deleteMcpServer(serverId) {
  return apiFetch(`/v1/mcp/servers/${encodeURIComponent(serverId)}`, { method: "DELETE" });
}

export function testMcpServer(serverId) {
  return apiFetch(`/v1/mcp/servers/${encodeURIComponent(serverId)}/test`, { method: "POST", body: {} });
}

export function refreshMcpServer(serverId) {
  return apiFetch(`/v1/mcp/servers/${encodeURIComponent(serverId)}/refresh`, { method: "POST", body: {} });
}

export function getAgentMcp(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/mcp`);
}

export function putAgentMcp(agentId, bindings) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/mcp`, {
    method: "PUT",
    body: { bindings: Array.isArray(bindings) ? bindings : [] },
  });
}

export function getAgentMcpEffectiveTools(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/mcp/effective-tools`);
}

export function listLinuxChannels() {
  return apiFetch("/v1/linux/channels");
}

export function createLinuxChannel(payload = {}) {
  return apiFetch("/v1/linux/channels", { method: "POST", body: payload });
}

export function patchLinuxChannel(channelId, payload = {}) {
  return apiFetch(`/v1/linux/channels/${encodeURIComponent(channelId)}`, { method: "PATCH", body: payload });
}

export function deleteLinuxChannel(channelId) {
  return apiFetch(`/v1/linux/channels/${encodeURIComponent(channelId)}`, { method: "DELETE" });
}

export function testLinuxChannel(channelId) {
  return apiFetch(`/v1/linux/channels/${encodeURIComponent(channelId)}/test`, { method: "POST", body: {} });
}

export function listLinuxCredentials() {
  return apiFetch("/v1/linux/credentials");
}

export function createLinuxCredential(payload = {}) {
  return apiFetch("/v1/linux/credentials", { method: "POST", body: payload });
}

export function patchLinuxCredential(credentialId, payload = {}) {
  return apiFetch(`/v1/linux/credentials/${encodeURIComponent(credentialId)}`, { method: "PATCH", body: payload });
}

export function deleteLinuxCredential(credentialId) {
  return apiFetch(`/v1/linux/credentials/${encodeURIComponent(credentialId)}`, { method: "DELETE" });
}

export function getAgentLinuxChannels(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/linux-channels`);
}

export function putAgentLinuxChannels(agentId, bindings) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/linux-channels`, {
    method: "PUT",
    body: { bindings: Array.isArray(bindings) ? bindings : [] },
  });
}

export function listTriggers() {
  return apiFetch("/v1/triggers");
}

export function getTrigger(triggerId) {
  return apiFetch(`/v1/triggers/${encodeURIComponent(triggerId)}`);
}

export function createTrigger(body) {
  return apiFetch("/v1/triggers", { method: "POST", body });
}

export function updateTrigger(triggerId, patch) {
  return apiFetch(`/v1/triggers/${encodeURIComponent(triggerId)}`, {
    method: "PATCH",
    body: patch,
  });
}

export function deleteTrigger(triggerId) {
  return apiFetch(`/v1/triggers/${encodeURIComponent(triggerId)}`, { method: "DELETE" });
}

export function fireTrigger(triggerId) {
  return apiFetch(`/v1/triggers/${encodeURIComponent(triggerId)}/fire`, { method: "POST", body: {} });
}

export function getTriggerHistory(triggerId) {
  return apiFetch(`/v1/triggers/${encodeURIComponent(triggerId)}/history`);
}

export function recoverTrigger(triggerId, deliveryId, revision) {
  const body = { delivery_id: deliveryId };
  if (revision !== undefined && revision !== null) body.revision = revision;
  return apiFetch(`/v1/triggers/${encodeURIComponent(triggerId)}/recover`, { method: "POST", body });
}

/** 工作组列表：scope=subscribed|acl|all */
export function listWorkgroups({ scope = "subscribed" } = {}) {
  return apiFetch("/v1/workgroups", { params: { scope } });
}

export function getDesktopWorkgroupList() {
  return apiFetch("/v1/workgroups/desktop/list");
}

export function getDesktopWorkgroupSnapshot(workgroupId, { includeAgents = false } = {}) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/desktop-snapshot`, { params: { include_agents: includeAgents ? "true" : undefined } });
}

export function getDesktopWorkgroupProgress(workgroupId) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/desktop-progress`);
}

export function listWorkgroupAgents() {
  return apiFetch("/v1/workgroups/meta/agents");
}

export function createWorkgroup(displayName, { llmProfileId, llmProfileRevision } = {}) {
  const body = { display_name: displayName };
  if (llmProfileId) body.llm_profile_id = llmProfileId;
  if (llmProfileRevision) body.llm_profile_revision = llmProfileRevision;
  return apiFetch("/v1/workgroups", {
    method: "POST",
    body,
  });
}

export function getWorkgroup(workgroupId) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}`);
}

export function patchWorkgroup(workgroupId, body) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}`, {
    method: "PATCH",
    body,
  });
}

export function publishWorkgroup(workgroupId) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/publish`, {
    method: "POST",
    body: {},
  });
}

export function listWorkgroupLLMConfigs(workgroupId) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/llm-configs`);
}

export function getWorkgroupACL(workgroupId) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/acl`);
}

export function addWorkgroupCollaborator(workgroupId, nodeId) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/collaborators`, {
    method: "POST",
    body: { node_id: nodeId },
  });
}

export function subscribeWorkgroup(workgroupId) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/subscribe`, {
    method: "POST",
    body: {},
  });
}

export function unsubscribeWorkgroup(workgroupId) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/subscribe`, {
    method: "DELETE",
  });
}

export function getWorkgroupTimeline(workgroupId, { limit } = {}) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/timeline`, {
    params: { limit },
  });
}

export function getWorkgroupEventsURL(workgroupId, afterSeq = 0) {
  const url = new URL(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/events`,
    window.location.origin,
  );
  if (Number(afterSeq) > 0) url.searchParams.set("after_seq", String(Number(afterSeq)));
  return url.toString();
}

export function postWorkgroupMessage(workgroupId, text, clientMessageId, directMemberId) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/messages`, {
    method: "POST",
    body: {
      text,
      client_message_id: clientMessageId || undefined,
      direct_member_id: directMemberId || undefined,
    },
  });
}

/**
 * 工作组消息 SSE；onEvent(eventName, data)。可传 signal 中断读取。
 * @returns {Promise<{ finalText?: string }>}
 */
export async function postWorkgroupMessageStream(
  workgroupId,
  { text, clientMessageId, directMemberId } = {},
  { onEvent, signal } = {},
) {
  const url = new URL(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/messages/stream`,
    window.location.origin,
  );
  const resp = await fetch(url, {
    method: "POST",
    headers: {
      Accept: "text/event-stream",
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      text,
      client_message_id: clientMessageId || undefined,
      direct_member_id: directMemberId || undefined,
    }),
    signal,
  });
  if (!resp.ok) {
    let message = `HTTP ${resp.status}`;
    try {
      const errBody = await resp.json();
      message = errBody?.error?.message || errBody?.message || message;
    } catch {
      /* ignore */
    }
    const err = new Error(message);
    err.status = resp.status;
    throw err;
  }
  if (!resp.body) {
    throw new Error("流式响应不可用");
  }

  const reader = resp.body.getReader();
  const decoder = new TextDecoder("utf-8");
  let buffer = "";
  let finalText = "";
  let sawError = null;

  const flushBlock = (block) => {
    const lines = block.split(/\r?\n/);
    let eventName = "message";
    const dataLines = [];
    for (const line of lines) {
      if (line.startsWith("event:")) eventName = line.slice(6).trim() || "message";
      else if (line.startsWith("data:")) dataLines.push(line.slice(5).trimStart());
    }
    if (!dataLines.length && eventName === "message") return;
    let data = {};
    const raw = dataLines.join("\n");
    if (raw) {
      try {
        data = JSON.parse(raw);
      } catch {
        data = { raw };
      }
    }
    if (eventName === "error") {
      sawError = data;
    }
    if (eventName === "final" || eventName === "assistant_final") {
      finalText = data?.loop?.final_text || data?.text || finalText;
    }
    if (typeof onEvent === "function") onEvent(eventName, data);
  };

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    buffer = buffer.replace(/\r\n/g, "\n");
    let idx;
    while ((idx = buffer.indexOf("\n\n")) >= 0) {
      const block = buffer.slice(0, idx);
      buffer = buffer.slice(idx + 2);
      if (block.trim()) flushBlock(block);
    }
  }
  if (buffer.trim()) flushBlock(buffer);

  if (sawError) {
    const err = new Error(sawError.message || sawError.code || "流式错误");
    err.detail = sawError;
    throw err;
  }
  return { finalText };
}

export function cancelWorkgroupTurn(workgroupId) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/turn/cancel`, {
    method: "POST",
    body: {},
  });
}

export function fetchWorkgroupHumanQueue(workgroupId) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/human-queue`);
}

export function patchWorkgroupHumanQueueItem(workgroupId, queueId, text) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/human-queue/${encodeURIComponent(queueId)}`,
    { method: "PATCH", body: { text } },
  );
}

export function cancelWorkgroupHumanQueueItem(workgroupId, queueId) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/human-queue/${encodeURIComponent(queueId)}`,
    { method: "DELETE" },
  );
}

export function sendWorkgroupHumanQueueItemNow(workgroupId, queueId) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/human-queue/${encodeURIComponent(queueId)}/send-now`,
    { method: "POST", body: {} },
  );
}

export function listWorkgroupMembers(workgroupId) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/members`);
}

export function createWorkgroupMember(workgroupId, body) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/members`, {
    method: "POST",
    body,
  });
}

export function patchWorkgroupMember(workgroupId, memberId, body) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/members/${encodeURIComponent(memberId)}`,
    { method: "PATCH", body },
  );
}

export function archiveWorkgroupMember(workgroupId, memberId) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/members/${encodeURIComponent(memberId)}/archive`,
    { method: "POST", body: {} },
  );
}

export function listWorkgroupHITL(workgroupId, pendingOnly = true) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/hitl`, {
    params: { pending_only: pendingOnly ? "true" : "false" },
  });
}

export function createWorkgroupHITL(workgroupId, prompt) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/hitl`, {
    method: "POST",
    body: { prompt },
  });
}

export function resolveWorkgroupHITL(workgroupId, hitlId, resolution) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/hitl/${encodeURIComponent(hitlId)}/resolve`,
    {
      method: "POST",
      body: { resolution: resolution || {} },
    },
  );
}

export function listWorkgroupRuns(workgroupId, { actorId, limit = 20 } = {}) {
  const params = { limit: String(limit) };
  if (actorId) params.actor_id = actorId;
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/runs`, { params });
}

export function getWorkgroupRunHistory(workgroupId, runId) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/runs/${encodeURIComponent(runId)}/history`,
  );
}
