function clone(value) {
  return JSON.parse(JSON.stringify(value ?? null));
}

function asObject(value) {
  return value && typeof value === "object" && !Array.isArray(value) ? value : {};
}

export const BLANK_TEMPLATE_ID = "__blank__";

export const TOOL_GROUPS = [
  { name: "a2a", label: "A2A 协作" },
  { name: "bash", label: "命令行" },
  { name: "browser", label: "浏览器", beta: true },
  { name: "child_agents", label: "子 Agent" },
  { name: "fs", label: "文件" },
  { name: "hitl", label: "人工确认 / 记忆" },
  { name: "skills", label: "技能" },
  { name: "triggers", label: "定时任务" },
  { name: "wecom", label: "企业微信" },
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
    // null = 不限制（全部可见）；string[] = 显式白名单（可为空）
    visibleSkills: null,
    childAgentsEnabled: true,
    sandboxEnabled: false,
    sandboxBackend: "process",
    sandboxImage: "dagents-sandbox:latest",
    sandboxNetwork: "none",
    sandboxMemory: "",
    sandboxCpus: "",
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

/** 技能能力与工具组 skills 收敛：未收窄（空列表）视为开启；否则看是否勾选 skills。 */
export function skillsEnabledFromToolGroups(toolGroups) {
  const groups = Array.isArray(toolGroups) ? toolGroups : [];
  if (groups.length === 0) return true;
  return groups.some((g) => String(g || "").trim() === "skills");
}

/** null = 未限制；数组 = 显式白名单。 */
function normalizeVisibleSkills(skills) {
  if (!skills || typeof skills !== "object" || !Object.prototype.hasOwnProperty.call(skills, "visible")) {
    return null;
  }
  if (!Array.isArray(skills.visible)) return [];
  const out = [];
  const seen = new Set();
  for (const item of skills.visible) {
    const name = String(item || "").trim();
    if (!name || seen.has(name)) continue;
    seen.add(name);
    out.push(name);
  }
  return out;
}

/** 写入 defaults.skills：能力由工具组控制；此处仅保留 visible 白名单。 */
function skillsPayload(draft) {
  if (!skillsEnabledFromToolGroups(draft?.toolGroups)) {
    return {};
  }
  if (draft?.visibleSkills === null || draft?.visibleSkills === undefined) {
    return {};
  }
  const visible = Array.isArray(draft.visibleSkills)
    ? draft.visibleSkills.map((x) => String(x || "").trim()).filter(Boolean)
    : [];
  return { visible };
}

/** 从模板展开为可编辑草稿（创建时由前端持有完整设置）。 */
/** 空白 Agent 草稿（不依赖模板）。 */
export function draftFromBlank(llmProfileIds = []) {
  const draft = emptyAgentDraft();
  const ids = Array.isArray(llmProfileIds) ? llmProfileIds.map((x) => String(x || "").trim()).filter(Boolean) : [];
  draft.llmProfileId = ids[0] || "";
  return draft;
}

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
  draft.visibleSkills = normalizeVisibleSkills(skills);
  draft.childAgentsEnabled = boolOr(childAgents.enabled, true);
  draft.sandboxEnabled = !!sandbox.enabled;
  draft.sandboxBackend = String(sandbox.backend || "process").trim() || "process";
  draft.sandboxImage = String(sandbox.image || "dagents-sandbox:latest").trim() || "dagents-sandbox:latest";
  draft.sandboxNetwork = String(sandbox.network || "none").trim() || "none";
  draft.sandboxMemory = String(sandbox.memory || "").trim();
  draft.sandboxCpus = String(sandbox.cpus || "").trim();
  draft.workspaceSubdir = String(sandbox.workspace_subdir || "data").trim() || "data";
  draft.fsRootIsolation = draft.sandboxBackend === "docker" ? true : !!sandbox.fs_root_isolation;
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
  draft.visibleSkills = normalizeVisibleSkills(skills);
  draft.childAgentsEnabled = boolOr(childAgents.enabled, true);
  draft.sandboxEnabled = boolOr(sandbox.enabled, !!agent?.sandbox_enabled);
  draft.sandboxBackend = String(sandbox.backend || agent?.sandbox_backend || "process").trim() || "process";
  draft.sandboxImage = String(sandbox.image || "dagents-sandbox:latest").trim() || "dagents-sandbox:latest";
  draft.sandboxNetwork = String(sandbox.network || "none").trim() || "none";
  draft.sandboxMemory = String(sandbox.memory || "").trim();
  draft.sandboxCpus = String(sandbox.cpus || "").trim();
  draft.workspaceSubdir = String(sandbox.workspace_subdir || "data").trim() || "data";
  draft.fsRootIsolation = draft.sandboxBackend === "docker" ? true : !!sandbox.fs_root_isolation;
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
      fs_root_isolation: draft.sandboxBackend === "docker" ? true : !!draft.fsRootIsolation,
      allow_bash: !!draft.allowBash,
      allow_network_tools: !!draft.allowNetworkTools,
      ...(draft.sandboxBackend === "docker"
        ? {
            image: String(draft.sandboxImage || "dagents-sandbox:latest").trim() || "dagents-sandbox:latest",
            network: String(draft.sandboxNetwork || "none").trim() || "none",
            ...(String(draft.sandboxMemory || "").trim() ? { memory: String(draft.sandboxMemory).trim() } : {}),
            ...(String(draft.sandboxCpus || "").trim() ? { cpus: String(draft.sandboxCpus).trim() } : {}),
          }
        : {}),
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
      skills: skillsPayload(draft),
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
  if (tpl && tpl !== BLANK_TEMPLATE_ID) payload.template_id = tpl;
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

/** 从 Agent 草稿构建创建模板 API 入参。 */
export function buildCreateTemplatePayload(meta, draft) {
  const id = String(meta?.id || "").trim();
  const displayName = String(meta?.displayName || meta?.display_name || "").trim();
  const description = String(meta?.description || "").trim();
  const llmActive = String(draft?.llmProfileId || "").trim();
  return {
    id,
    display_name: displayName || id,
    description,
    version: 1,
    sandbox: {
      enabled: !!draft?.sandboxEnabled,
      backend: draft?.sandboxBackend || "process",
      workspace_subdir: draft?.workspaceSubdir || "data",
      fs_root_isolation: !!draft?.fsRootIsolation,
      allow_bash: draft?.allowBash !== false,
      allow_network_tools: draft?.allowNetworkTools !== false,
    },
    defaults: {
      agent: {
        role: String(draft?.role || "assistant").trim() || "assistant",
        description: String(draft?.description || "").trim(),
      },
      llm: {
        ...(llmActive ? { active: llmActive } : {}),
        max_tool_loops: numberOr(draft?.maxToolLoops, 32),
      },
      tools: {
        enabled_groups: Array.isArray(draft?.toolGroups) ? [...draft.toolGroups] : [],
      },
      skills: skillsPayload(draft),
      child_agents: { enabled: !!draft?.childAgentsEnabled },
      prompt_context: {
        soul_enabled: !!draft?.promptSoulEnabled,
        user_enabled: !!draft?.promptUserEnabled,
        custom_enabled: !!draft?.promptCustomEnabled,
        long_term_enabled: !!draft?.promptLongTermEnabled,
      },
    },
  };
}

export function llmActiveFromAgentView(agent) {
  const snap = parseSnapshot(agent?.config_snapshot);
  const active = snap?.defaults?.llm?.active;
  return String(active || "").trim();
}

export { clone };
