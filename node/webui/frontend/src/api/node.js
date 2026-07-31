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
  let data = null;
  try {
    data = await resp.json();
  } catch {
    data = null;
  }
  if (!resp.ok) {
    const code = data?.error?.code || "";
    const base = data?.error?.message || data?.message || `HTTP ${resp.status}`;
    const msg =
      code === "edge_required" || code === "edge_session_failed" || code === "edge_proxy_failed"
        ? `[Edge] ${base}`
        : base;
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
  if (payload.sandbox && typeof payload.sandbox === "object") body.sandbox = payload.sandbox;
  if (payload.defaults && typeof payload.defaults === "object") body.defaults = payload.defaults;
  return apiFetch("/v1/agents", { method: "POST", body });
}

/** 同组可放置的 peer Node（需启用 Manage）。 */
export function listPeerNodes() {
  return apiFetch("/v1/peers/nodes");
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
  if (patch.sandbox && typeof patch.sandbox === "object") body.sandbox = patch.sandbox;
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

export function createWorkgroup(displayName) {
  return apiFetch("/v1/workgroups", {
    method: "POST",
    body: { display_name: displayName },
  });
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

export function getWorkgroupTimeline(workgroupId) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/timeline`);
}

export function postWorkgroupMessage(workgroupId, text, clientMessageId) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/messages`, {
    method: "POST",
    body: {
      text,
      client_message_id: clientMessageId || undefined,
    },
  });
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

export function listWorkgroupGrants(workgroupId) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/grants`);
}

export function inviteWorkgroupGrant(workgroupId, memberId, toolAllowNames) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/grants`, {
    method: "POST",
    body: {
      member_id: memberId,
      tool_allow_names: toolAllowNames,
    },
  });
}

export function acceptWorkgroupGrant(workgroupId, grantId, memberSpecDigest) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/grants/${encodeURIComponent(grantId)}/accept`,
    {
      method: "POST",
      body: memberSpecDigest ? { member_spec_digest: memberSpecDigest } : {},
    },
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

export function resolveWorkgroupHITL(workgroupId, hitlId, answer) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/hitl/${encodeURIComponent(hitlId)}/resolve`,
    {
      method: "POST",
      body: { answer, resolution: { answer } },
    },
  );
}
