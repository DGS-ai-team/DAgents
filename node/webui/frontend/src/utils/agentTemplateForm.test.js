import { describe, expect, it } from "vitest";
import {
  BLANK_TEMPLATE_ID,
  buildCreateAgentPayload,
  buildCreateTemplatePayload,
  buildPatchAgentPayload,
  draftFromAgentView,
  draftFromBlank,
  draftFromTemplate,
  llmActiveFromAgentView,
} from "./agentTemplateForm.js";

describe("agentTemplateForm", () => {
  it("expands full draft from template", () => {
    const draft = draftFromTemplate(
      {
        id: "ops-runner",
        display_name: "运维执行助手",
        description: "沙箱运维",
        defaults: {
          llm: { active: "deepseek", max_tool_loops: 24 },
          tools: { enabled_groups: ["fs", "bash"] },
          skills: { enabled: false },
        },
        sandbox: { enabled: true, backend: "process", allow_bash: true },
      },
      ["default", "deepseek"],
    );
    expect(draft.templateId).toBe("ops-runner");
    expect(draft.sandboxEnabled).toBe(true);
    expect(draft.llmProfileId).toBe("deepseek");
    expect(draft.toolGroups).toEqual(["fs", "bash"]);
    expect(draft.skillsEnabled).toBe(false);
    expect(draft.visibleSkills).toBeNull();
    expect(draft.promptLongTermEnabled).toBe(true);
  });

  it("falls back to first llm profile when template active missing", () => {
    const draft = draftFromTemplate(
      {
        id: "general",
        display_name: "通用",
        defaults: { llm: { active: "missing" } },
        sandbox: { enabled: false },
      },
      ["default", "qwen"],
    );
    expect(draft.llmProfileId).toBe("default");
  });

  it("builds full create payload without requiring template merge on server", () => {
    const payload = buildCreateAgentPayload({
      templateId: "general",
      displayName: "我的助手",
      description: "日常助手",
      role: "assistant",
      sandboxEnabled: false,
      sandboxBackend: "process",
      workspaceSubdir: "data",
      fsRootIsolation: false,
      allowBash: true,
      allowNetworkTools: true,
      llmProfileId: "qwen-plus",
      maxToolLoops: 16,
      toolGroups: ["fs", "skills"],
      skillsEnabled: true,
      visibleSkills: ["write-skill", "write-hook"],
      childAgentsEnabled: true,
      promptSoulEnabled: true,
      promptUserEnabled: false,
      promptCustomEnabled: true,
      promptLongTermEnabled: false,
    });
    expect(payload.template_id).toBe("general");
    expect(payload.display_name).toBe("我的助手");
    expect(payload.defaults.llm).toEqual({ active: "qwen-plus", max_tool_loops: 16 });
    expect(payload.defaults.tools.enabled_groups).toEqual(["fs", "skills"]);
    expect(payload.defaults.skills).toEqual({
      enabled: true,
      visible: ["write-skill", "write-hook"],
    });
    expect(payload.defaults.prompt_context.long_term_enabled).toBe(false);
    expect(payload.defaults.prompt_context.user_enabled).toBe(false);
    expect(payload.sandbox.allow_bash).toBe(true);
  });

  it("builds patch payload without template_id", () => {
    const patch = buildPatchAgentPayload({
      templateId: "general",
      displayName: "改名",
      llmProfileId: "a",
      maxToolLoops: 8,
      toolGroups: ["fs"],
      skillsEnabled: true,
      childAgentsEnabled: false,
      sandboxEnabled: true,
      sandboxBackend: "process",
      workspaceSubdir: "data",
      fsRootIsolation: true,
      allowBash: false,
      allowNetworkTools: false,
      promptSoulEnabled: true,
      promptUserEnabled: true,
      promptCustomEnabled: true,
      promptLongTermEnabled: true,
      role: "assistant",
      description: "",
    });
    expect(patch.template_id).toBeUndefined();
    expect(patch.display_name).toBe("改名");
    expect(patch.defaults.tools.enabled_groups).toEqual(["fs"]);
  });

  it("reads draft from agent view", () => {
    const draft = draftFromAgentView(
      {
        display_name: "已有",
        template_id: "general",
        sandbox_enabled: true,
        config_snapshot: {
          defaults: {
            llm: { active: "deepseek", max_tool_loops: 10 },
            tools: { enabled_groups: ["bash"] },
            skills: { enabled: true, visible: ["write-skill"] },
            prompt_context: { long_term_enabled: false },
          },
          sandbox: { enabled: true, backend: "process", allow_bash: true },
        },
      },
      ["deepseek"],
    );
    expect(draft.displayName).toBe("已有");
    expect(draft.toolGroups).toEqual(["bash"]);
    expect(draft.visibleSkills).toEqual(["write-skill"]);
    expect(draft.promptLongTermEnabled).toBe(false);
    expect(draft.llmProfileId).toBe("deepseek");
  });

  it("omits skills.visible when unrestricted", () => {
    const payload = buildCreateAgentPayload({
      displayName: "全技能",
      llmProfileId: "default",
      maxToolLoops: 32,
      toolGroups: ["skills"],
      skillsEnabled: true,
      visibleSkills: null,
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
      role: "assistant",
      description: "",
    });
    expect(payload.defaults.skills).toEqual({ enabled: true });
  });

  it("builds create payload without template_id for blank draft", () => {
    const payload = buildCreateAgentPayload({
      templateId: BLANK_TEMPLATE_ID,
      displayName: "空白",
      llmProfileId: "default",
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
      role: "assistant",
      description: "",
    });
    expect(payload.template_id).toBeUndefined();
    expect(payload.display_name).toBe("空白");
  });

  it("draftFromBlank picks first llm profile", () => {
    const draft = draftFromBlank(["qwen", "deepseek"]);
    expect(draft.templateId).toBe("");
    expect(draft.llmProfileId).toBe("qwen");
  });

  it("builds create template payload from draft", () => {
    const payload = buildCreateTemplatePayload(
      { id: "my-bot", displayName: "我的 Bot", description: "测试" },
      {
        displayName: "ignored",
        description: "desc",
        role: "assistant",
        llmProfileId: "default",
        maxToolLoops: 20,
        toolGroups: ["fs"],
        skillsEnabled: true,
        childAgentsEnabled: false,
        sandboxEnabled: true,
        sandboxBackend: "process",
        workspaceSubdir: "data",
        fsRootIsolation: false,
        allowBash: true,
        allowNetworkTools: false,
        promptSoulEnabled: true,
        promptUserEnabled: true,
        promptCustomEnabled: true,
        promptLongTermEnabled: false,
      },
    );
    expect(payload.id).toBe("my-bot");
    expect(payload.display_name).toBe("我的 Bot");
    expect(payload.defaults.llm).toEqual({ active: "default", max_tool_loops: 20 });
    expect(payload.defaults.child_agents.enabled).toBe(false);
    expect(payload.sandbox.enabled).toBe(true);
  });

  it("reads llm active from agent view snapshot", () => {
    expect(
      llmActiveFromAgentView({
        config_snapshot: { defaults: { llm: { active: "deepseek" } } },
      }),
    ).toBe("deepseek");
  });
});
