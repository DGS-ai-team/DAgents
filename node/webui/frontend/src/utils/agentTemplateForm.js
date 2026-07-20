const TOOL_GROUPS = [
  { name: "fs", label: "文件系统" },
  { name: "skills", label: "Skills" },
  { name: "bash", label: "Shell / Bash" },
  { name: "child_agents", label: "子 Agent" },
  { name: "triggers", label: "触发器" },
  { name: "hitl", label: "人工审批" },
  { name: "browser", label: "浏览器", beta: true },
  { name: "a2a", label: "A2A", beta: true },
];

function clone(value) {
  return JSON.parse(JSON.stringify(value ?? null));
}

function asObject(value) {
  return value && typeof value === "object" && !Array.isArray(value) ? value : {};
}

export function emptyAgentDraft() {
  return {
    templateId: "",
    displayName: "",
    description: "",
    sandboxEnabled: false,
    sandboxBackend: "process",
    enabledGroups: [],
    maxToolLoops: 16,
    childAgentsEnabled: true,
    skillsEnabled: true,
    hooks: {
      inject_today_date_enabled: true,
      tool_result_enabled: true,
      tool_result_spill_threshold_tokens: 12000,
      duplicate_tool_call_enabled: true,
      duplicate_tool_call_window_seconds: 60,
    },
  };
}

export function readTemplateDefaults(template) {
  return asObject(template?.defaults);
}

export function enabledGroupsFromDefaults(defaults) {
  const groups = defaults?.tools?.enabled_groups;
  return Array.isArray(groups) ? [...groups] : [];
}

export function draftFromTemplate(template, nodeHooks = {}) {
  const defaults = readTemplateDefaults(template);
  const draft = emptyAgentDraft();
  draft.templateId = String(template?.id || "").trim();
  draft.displayName = String(template?.display_name || template?.id || "").trim();
  draft.description = String(template?.description || "").trim();
  draft.sandboxEnabled = !!template?.sandbox?.enabled;
  draft.sandboxBackend = String(template?.sandbox?.backend || "process").trim() || "process";
  draft.enabledGroups = enabledGroupsFromDefaults(defaults);
  draft.maxToolLoops = Number(defaults?.llm?.max_tool_loops) || draft.maxToolLoops;
  draft.childAgentsEnabled = defaults?.child_agents?.enabled !== false;
  draft.skillsEnabled = defaults?.skills?.enabled !== false;
  draft.hooks = {
    ...draft.hooks,
    ...clone(nodeHooks),
    ...clone(defaults?.hooks || {}),
  };
  return draft;
}

export function buildCreateAgentPayload(draft) {
  const defaults = {
    llm: {
      max_tool_loops: Number(draft.maxToolLoops) || 16,
    },
    tools: {
      enabled_groups: [...(draft.enabledGroups || [])].sort(),
    },
    skills: {
      enabled: !!draft.skillsEnabled,
    },
    child_agents: {
      enabled: !!draft.childAgentsEnabled,
    },
    hooks: {
      inject_today_date_enabled: !!draft.hooks.inject_today_date_enabled,
      tool_result_enabled: !!draft.hooks.tool_result_enabled,
      tool_result_spill_threshold_tokens: Number(draft.hooks.tool_result_spill_threshold_tokens) || 12000,
      duplicate_tool_call_enabled: !!draft.hooks.duplicate_tool_call_enabled,
      duplicate_tool_call_window_seconds: Number(draft.hooks.duplicate_tool_call_window_seconds) || 60,
    },
  };
  return {
    templateId: draft.templateId,
    displayName: String(draft.displayName || "").trim() || undefined,
    sandbox: {
      enabled: !!draft.sandboxEnabled,
      backend: draft.sandboxBackend || "process",
    },
    defaults,
  };
}

export function toggleToolGroup(draft, name) {
  const set = new Set(draft.enabledGroups || []);
  if (set.has(name)) set.delete(name);
  else set.add(name);
  draft.enabledGroups = [...set].sort();
}

export { TOOL_GROUPS };
