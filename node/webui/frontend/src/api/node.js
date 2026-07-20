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

export function listAgentTemplates() {
  return apiFetch("/v1/agent-templates");
}

export function getAgentTemplate(templateId) {
  return apiFetch(`/v1/agent-templates/${encodeURIComponent(templateId)}`);
}

export function createAgent({ templateId, displayName, sandbox } = {}) {
  const body = { template_id: templateId };
  if (displayName) body.display_name = displayName;
  if (sandbox && typeof sandbox === "object") body.sandbox = sandbox;
  return apiFetch("/v1/agents", { method: "POST", body });
}

export function listAgents() {
  return apiFetch("/v1/agents");
}

export function getAgent(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}`);
}

export function patchAgent(agentId, patch) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}`, { method: "PATCH", body: patch });
}

export function deleteAgent(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}`, { method: "DELETE" });
}

export function ensureAgentRuntime(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/ensure`, { method: "POST", body: {} });
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

export function clearContext(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/clear-context`, { method: "POST", body: {} });
}

export function compressContext(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/compress`, { method: "POST", body: {} });
}

export function getSessionContext(agentId, { fullMessages = false } = {}) {
  return getAgentContext(agentId, { fullMessages });
}

export function getSessionHydrate(agentId) {
  return getAgentHydrate(agentId);
}

export function postSessionAck(agentId, sseSeq) {
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

export function cancelTurn(agentId) {
  return cancelAgentTurn(agentId);
}

export function listSkills(agentId) {
  return apiFetch(`/v1/agents/${encodeURIComponent(agentId)}/skills`);
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

export function cancelChildAgent(agentId, childSessionId, reason = "") {
  const body = reason ? { reason } : {};
  return apiFetch(
    `/v1/agents/${encodeURIComponent(agentId)}/child-agents/${encodeURIComponent(childSessionId)}/cancel`,
    { method: "POST", body },
  );
}

export function getPolicy(shellQuery) {
  return apiFetch("/v1/policy", { params: shellQuery ? { shell: shellQuery } : {} });
}

export function updateToolPolicy(updates) {
  return apiFetch("/v1/policy/tools", { method: "PUT", body: { updates } });
}

export function updateShellPolicy(shellType, updates, deletes = []) {
  const body = { updates: updates || [] };
  if (Array.isArray(deletes) && deletes.length) body.deletes = deletes;
  return apiFetch(`/v1/policy/shell/${encodeURIComponent(shellType)}`, {
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
