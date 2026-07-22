function clone(value) {
  return JSON.parse(JSON.stringify(value ?? null));
}

function asObject(value) {
  return value && typeof value === "object" && !Array.isArray(value) ? value : {};
}

export const TOOL_GROUPS = [
  { name: "a2a", label: "A2A 协作" },
  { name: "bash", label: "命令行" },
  { name: "browser", label: "浏览器", beta: true },
  { name: "child_agents", label: "子 Agent" },
  { name: "fs", label: "文件" },
  { name: "hitl", label: "人工确认 / 记忆" },
  { name: "skills", label: "技能" },
  { name: "triggers", label: "定时任务" },
];

export const LONG_TERM_SCOPES = [
  { value: "agent", label: "独立（仅本 Agent）" },
  { value: "global", label: "全局（所有 Agent 共享）" },
];

export function emptyAgentDraft() {
  return {
    // 溯源：创建请求可不传；仅用于 UI 展示来源模板
    templateId: "",
    displayName: "",
    description: "",
    role: "assistant",
    llmProfileId: "",
    maxToolLoops: 32,
    toolGroups: [],
    skillsEnabled: true,
    childAgentsEnabled: true,
    sandboxEnabled: false,
    sandboxBackend: "process",
    workspaceSubdir: "data",
    fsRootIsolation: false,
    allowBash: true,
    allowNetworkTools: true,
    promptSoulEnabled: true,
    promptUserEnabled: true,
    promptCustomEnabled: true,
    promptLongTermEnabled: true,
    promptLongTermScope: "agent",
    promptSoulMd: "",
    promptUserMd: "",
    promptCustomMd: "",
    promptLongTermEntries: [],
    promptGlobalLongTermEntries: [],
  };
}

export function readTemplateDefaults(template) {
  return asObject(template?.defaults);
}

function boolOr(value, fallback) {
  return typeof value === "boolean" ? value : fallback;
}

function numberOr(value, fallback) {
  const n = Number(value);
  return Number.isFinite(n) && n > 0 ? n : fallback;
}

/** 从模板展开为可编辑草稿（创建时由前端持有完整设置）。 */
export function draftFromTemplate(template, llmProfileIds = []) {
  const defaults = readTemplateDefaults(template);
  const agent = asObject(defaults.agent);
  const llm = asObject(defaults.llm);
  const tools = asObject(defaults.tools);
  const skills = asObject(defaults.skills);
  const childAgents = asObject(defaults.child_agents);
  const prompt = asObject(defaults.prompt_context);
  const sandbox = asObject(template?.sandbox);

  const draft = emptyAgentDraft();
  draft.templateId = String(template?.id || "").trim();
  draft.displayName = String(template?.display_name || template?.id || "").trim();
  draft.description = String(template?.description || agent.description || "").trim();
  draft.role = String(agent.role || "assistant").trim() || "assistant";
  draft.maxToolLoops = numberOr(llm.max_tool_loops, 32);
  draft.toolGroups = Array.isArray(tools.enabled_groups)
    ? tools.enabled_groups.map((x) => String(x || "").trim()).filter(Boolean)
    : [];
  draft.skillsEnabled = boolOr(skills.enabled, true);
  draft.childAgentsEnabled = boolOr(childAgents.enabled, true);
  draft.sandboxEnabled = !!sandbox.enabled;
  draft.sandboxBackend = String(sandbox.backend || "process").trim() || "process";
  draft.workspaceSubdir = String(sandbox.workspace_subdir || "data").trim() || "data";
  draft.fsRootIsolation = !!sandbox.fs_root_isolation;
  draft.allowBash = sandbox.allow_bash !== false;
  draft.allowNetworkTools = sandbox.allow_network_tools !== false;
  draft.promptSoulEnabled = boolOr(prompt.soul_enabled, true);
  draft.promptUserEnabled = boolOr(prompt.user_enabled, true);
  draft.promptCustomEnabled = boolOr(prompt.custom_enabled, true);
  draft.promptLongTermEnabled = boolOr(prompt.long_term_enabled, true);
  draft.promptLongTermScope = String(prompt.long_term_scope || "agent").trim() === "global" ? "global" : "agent";

  const fromTpl = String(llm.active || "").trim();
  const ids = Array.isArray(llmProfileIds) ? llmProfileIds.map((x) => String(x || "").trim()).filter(Boolean) : [];
  if (fromTpl && ids.includes(fromTpl)) {
    draft.llmProfileId = fromTpl;
  } else {
    draft.llmProfileId = ids[0] || fromTpl || "";
  }
  return draft;
}

/** 从已有 Agent 视图还原草稿（设置页编辑）。 */
export function draftFromAgentView(agent, llmProfileIds = []) {
  const snap = parseSnapshot(agent?.config_snapshot);
  const defaults = asObject(snap.defaults);
  const agentMeta = asObject(defaults.agent);
  const llm = asObject(defaults.llm);
  const tools = asObject(defaults.tools);
  const skills = asObject(defaults.skills);
  const childAgents = asObject(defaults.child_agents);
  const prompt = asObject(defaults.prompt_context);
  const sandbox = asObject(snap.sandbox);

  const draft = emptyAgentDraft();
  draft.templateId = String(agent?.template_id || snap.template_id || "").trim();
  draft.displayName = String(agent?.display_name || "").trim();
  draft.description = String(agentMeta.description || "").trim();
  draft.role = String(agentMeta.role || "assistant").trim() || "assistant";
  draft.maxToolLoops = numberOr(llm.max_tool_loops, 32);
  draft.toolGroups = Array.isArray(tools.enabled_groups)
    ? tools.enabled_groups.map((x) => String(x || "").trim()).filter(Boolean)
    : [];
  draft.skillsEnabled = boolOr(skills.enabled, true);
  draft.childAgentsEnabled = boolOr(childAgents.enabled, true);
  draft.sandboxEnabled = boolOr(sandbox.enabled, !!agent?.sandbox_enabled);
  draft.sandboxBackend = String(sandbox.backend || agent?.sandbox_backend || "process").trim() || "process";
  draft.workspaceSubdir = String(sandbox.workspace_subdir || "data").trim() || "data";
  draft.fsRootIsolation = !!sandbox.fs_root_isolation;
  draft.allowBash = sandbox.allow_bash !== false;
  draft.allowNetworkTools = sandbox.allow_network_tools !== false;
  draft.promptSoulEnabled = boolOr(prompt.soul_enabled, true);
  draft.promptUserEnabled = boolOr(prompt.user_enabled, true);
  draft.promptCustomEnabled = boolOr(prompt.custom_enabled, true);
  draft.promptLongTermEnabled = boolOr(prompt.long_term_enabled, true);
  draft.promptLongTermScope = String(prompt.long_term_scope || "agent").trim() === "global" ? "global" : "agent";

  const fromSnap = String(llm.active || "").trim();
  const ids = Array.isArray(llmProfileIds) ? llmProfileIds.map((x) => String(x || "").trim()).filter(Boolean) : [];
  if (fromSnap && ids.includes(fromSnap)) {
    draft.llmProfileId = fromSnap;
  } else {
    draft.llmProfileId = ids[0] || fromSnap || "";
  }
  return draft;
}

function parseSnapshot(snap) {
  if (!snap) return {};
  if (typeof snap === "string") {
    try {
      return asObject(JSON.parse(snap));
    } catch {
      return {};
    }
  }
  return asObject(snap);
}

/** 创建入参：完整 Agent 设置；template_id 仅溯源可选。 */
export function buildCreateAgentPayload(draft) {
  const llmActive = String(draft.llmProfileId || "").trim();
  const payload = {
    display_name: String(draft.displayName || "").trim() || undefined,
    sandbox: {
      enabled: !!draft.sandboxEnabled,
      backend: draft.sandboxBackend || "process",
      workspace_subdir: draft.workspaceSubdir || "data",
      fs_root_isolation: !!draft.fsRootIsolation,
      allow_bash: !!draft.allowBash,
      allow_network_tools: !!draft.allowNetworkTools,
    },
    defaults: {
      agent: {
        role: String(draft.role || "assistant").trim() || "assistant",
        description: String(draft.description || "").trim(),
      },
      llm: {
        ...(llmActive ? { active: llmActive } : {}),
        max_tool_loops: numberOr(draft.maxToolLoops, 32),
      },
      tools: {
        enabled_groups: Array.isArray(draft.toolGroups) ? [...draft.toolGroups] : [],
      },
      skills: { enabled: !!draft.skillsEnabled },
      child_agents: { enabled: !!draft.childAgentsEnabled },
      prompt_context: {
        soul_enabled: !!draft.promptSoulEnabled,
        user_enabled: !!draft.promptUserEnabled,
        custom_enabled: !!draft.promptCustomEnabled,
        long_term_enabled: !!draft.promptLongTermEnabled,
        long_term_scope: draft.promptLongTermScope === "global" ? "global" : "agent",
      },
    },
  };
  const tpl = String(draft.templateId || "").trim();
  if (tpl) payload.template_id = tpl;
  return payload;
}

/** PATCH 入参：完整可编辑字段。 */
export function buildPatchAgentPayload(draft) {
  const created = buildCreateAgentPayload(draft);
  return {
    display_name: created.display_name,
    sandbox: created.sandbox,
    defaults: created.defaults,
  };
}

export function llmActiveFromAgentView(agent) {
  const snap = parseSnapshot(agent?.config_snapshot);
  const active = snap?.defaults?.llm?.active;
  return String(active || "").trim();
}

export { clone };
