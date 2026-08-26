async function readJSON(response) {
  try {
    return await response.json();
  } catch {
    return null;
  }
}

async function apiFetch(path, { method = "GET", body, params } = {}) {
  const url = new URL(path, window.location.origin);
  if (params) {
    Object.entries(params).forEach(([k, v]) => {
      if (v !== undefined && v !== null && v !== "") url.searchParams.set(k, String(v));
    });
  }
  const headers = { Accept: "application/json" };
  const init = { method, headers };
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(body);
  }
  const resp = await fetch(url, init);
  const data = await readJSON(resp);
  if (!resp.ok) {
    const msg = data?.error?.message || data?.message || `HTTP ${resp.status}`;
    throw new Error(msg);
  }
  return data;
}

export function getHealth() {
  return apiFetch("/health");
}

export function getAgentInfo() {
  return apiFetch("/v1/agent/info");
}

/** 聚合 health + agent/info + llm/settings（Chat 首屏）。 */
export function getUIBootstrap() {
  return apiFetch("/v1/ui/bootstrap");
}

export function getWorkspaceActivity(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/workspace-activity`);
}

export function getAgentUpdate() {
  return apiFetch("/v1/agent/update");
}

export function getLLMSettings() {
  return apiFetch("/v1/llm/settings");
}

export function patchLLMSettings(patch) {
  return apiFetch("/v1/llm/settings", { method: "PATCH", body: patch });
}

export function getSetupConfig() {
  return apiFetch("/v1/setup/config");
}

export function patchSetupConfig(patch) {
  return apiFetch("/v1/setup/config", { method: "PATCH", body: patch });
}

/** 用 base_url + api_key 探测 OpenAI 兼容 /models 列表。 */
export function probeLLMModels(payload = {}) {
  return apiFetch("/v1/setup/llm/probe-models", { method: "POST", body: payload });
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
  if (payload.origin) body.origin = payload.origin;
  if (payload.defaults && typeof payload.defaults === "object") body.defaults = payload.defaults;
  return apiFetch("/v1/agents", { method: "POST", body });
}

export function listAgents() {
  return apiFetch("/v1/agents");
}

export function getAgent(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}`);
}

export function patchAgent(agentId, patch = {}) {
  const body = {};
  if (patch.display_name != null || patch.displayName != null) {
    body.display_name = patch.display_name ?? patch.displayName;
  }
  if (patch.llm_active != null || patch.llmActive != null) {
    body.llm_active = patch.llm_active ?? patch.llmActive;
  }
  if (patch.defaults && typeof patch.defaults === "object") body.defaults = patch.defaults;
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

export function getAgentContext(agentId, { fullMessages = false } = {}) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/context`, {
    params: fullMessages ? { full_messages: "1" } : {},
  });
}

export function cancelAgentTurn(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/cancel`, { method: "POST", body: {} });
}

export function cancelAgentToolCall(agentId, toolCallId) {
  return apiFetch(
    `/v1/agents/${encodeURIComponent(agentId)}/tool-calls/${encodeURIComponent(toolCallId)}/cancel`,
    { method: "POST", body: {} },
  );
}

export function backgroundAgentToolCall(agentId, toolCallId) {
  return apiFetch(
    `/v1/agents/${encodeURIComponent(agentId)}/tool-calls/${encodeURIComponent(toolCallId)}/background`,
    { method: "POST", body: {} },
  );
}

export function getAgentToolJobs(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/tool-jobs`);
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

export function clearContext(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/clear-context`, { method: "POST", body: {} });
}

export function compressContext(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/compress`, { method: "POST", body: {} });
}

export function postAgentAck(agentId, sseSeq) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/ack`, {
    method: "POST",
    body: { sse_seq: sseSeq },
  });
}

export function submitMessage(agentId, content, contentParts = null) {
  const body = { agent_id: agentId, request_type: "message", content: content || "" };
  if (Array.isArray(contentParts) && contentParts.length) {
    body.content_parts = contentParts;
  }
  return apiFetch("/v1/messages", { method: "POST", body });
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

export function listChildAgents(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/child-agents`);
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

export function uploadSkillToManage({ path, skillId, version, name, publish = false }) {
  return apiFetch("/v1/manage/upload/skill", {
    method: "POST",
    body: { path, skill_id: skillId, version, name, publish },
  });
}

export function uploadExternalToolToManage({ path, toolId, version, name, platform = "", publish = false }) {
  return apiFetch("/v1/manage/upload/externaltool", {
    method: "POST",
    body: { path, tool_id: toolId, version, name, platform, publish },
  });
}

export function uploadPluginToManage({ path, pluginId, version, name, platform = "", publish = false }) {
  return apiFetch("/v1/manage/upload/plugin", {
    method: "POST",
    body: { path, plugin_id: pluginId, version, name, platform, publish },
  });
}

/** 工作组列表：scope=subscribed|acl|all */
export function listWorkgroups({ scope = "subscribed" } = {}) {
  return apiFetch("/v1/workgroups", { params: { scope } });
}

/** 成员可勾选工具目录（Node 本地嵌入 shared catalog；不依赖 Manage） */
export function getMemberToolCatalog() {
  return apiFetch("/v1/workgroups/meta/member-tools");
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

export function getWorkgroupMemberSpec(workgroupId, memberId) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/members/${encodeURIComponent(memberId)}/spec`,
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

export function resolveWorkgroupHITL(workgroupId, hitlId, answer, resolution = null) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/hitl/${encodeURIComponent(hitlId)}/resolve`,
    {
      method: "POST",
      body: { answer, resolution: resolution || { answer } },
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
