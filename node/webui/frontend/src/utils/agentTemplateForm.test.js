import { describe, expect, it } from "vitest";
import { buildCreateAgentPayload, draftFromTemplate } from "./agentTemplateForm.js";

describe("agentTemplateForm", () => {
  it("builds draft from template defaults", () => {
    const draft = draftFromTemplate({
      id: "ops-runner",
      display_name: "运维执行助手",
      description: "沙箱运维",
      defaults: {
        llm: { max_tool_loops: 24 },
        tools: { enabled_groups: ["fs", "bash"] },
        child_agents: { enabled: true },
        skills: { enabled: true },
      },
      sandbox: { enabled: true, backend: "process" },
    });
    expect(draft.templateId).toBe("ops-runner");
    expect(draft.enabledGroups).toEqual(["fs", "bash"]);
    expect(draft.maxToolLoops).toBe(24);
    expect(draft.sandboxEnabled).toBe(true);
  });

  it("builds create payload with defaults overrides", () => {
    const payload = buildCreateAgentPayload({
      templateId: "general",
      displayName: "我的助手",
      sandboxEnabled: false,
      sandboxBackend: "process",
      enabledGroups: ["fs", "skills"],
      maxToolLoops: 20,
      childAgentsEnabled: false,
      skillsEnabled: true,
      hooks: {
        inject_today_date_enabled: false,
        tool_result_enabled: true,
        tool_result_spill_threshold_tokens: 8000,
        duplicate_tool_call_enabled: true,
        duplicate_tool_call_window_seconds: 45,
      },
    });
    expect(payload.templateId).toBe("general");
    expect(payload.defaults.tools.enabled_groups).toEqual(["fs", "skills"]);
    expect(payload.defaults.hooks.inject_today_date_enabled).toBe(false);
    expect(payload.defaults.llm.max_tool_loops).toBe(20);
  });
});
