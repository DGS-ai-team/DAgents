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
  memoryEnabledFromToolGroups,
  skillsEnabledFromToolGroups,
} from "./agentTemplateForm.js";

describe("agentTemplateForm", () => {
  it("expands full draft from template", () => {
    const draft = draftFromTemplate(
      {
        id: "ops-runner",
        display_name: "运维执行助手",
        description: "运维助手",
        defaults: {
          llm: { active: "deepseek", max_tool_loops: 24 },
          tools: { enabled_groups: ["fs", "bash"] },
          skills: { enabled: false },
        },
      },
      ["default", "deepseek"],
    );
    expect(draft.templateId).toBe("ops-runner");
    expect(draft.llmProfileId).toBe("deepseek");
    expect(draft.toolGroups).toEqual(["fs", "bash"]);
    expect(draft.visibleSkills).toBeNull();
    expect(skillsEnabledFromToolGroups(draft.toolGroups)).toBe(false);
    expect(memoryEnabledFromToolGroups(draft.toolGroups)).toBe(false);
  });

  it("ignores legacy skills.enabled and trusts tool groups", () => {
    const draft = draftFromTemplate(
      {
        id: "legacy",
        display_name: "旧",
        defaults: {
          tools: { enabled_groups: ["fs", "skills"] },
          skills: { enabled: false },
        },
      },
      ["default"],
    );
    expect(draft.toolGroups).toEqual(["fs", "skills"]);
    expect(skillsEnabledFromToolGroups(draft.toolGroups)).toBe(true);
  });

  it("falls back to first llm profile when template active missing", () => {
    const draft = draftFromTemplate(
      {
        id: "general",
        display_name: "通用",
        defaults: { llm: { active: "missing" } },
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
      llmProfileId: "qwen-plus",
      maxToolLoops: 16,
      toolGroups: ["fs", "skills"],
      visibleSkills: ["write-skill", "write-hook"],
      promptSoulMd: "你是助手",
      promptCustomMd: "",
    });
    expect(payload.template_id).toBe("general");
    expect(payload.display_name).toBe("我的助手");
    expect(payload.sandbox).toBeUndefined();
    expect(payload.defaults.llm).toEqual({ active: "qwen-plus", max_tool_loops: 16 });
    expect(payload.defaults.tools.enabled_groups).toEqual(["fs", "skills"]);
    expect(payload.defaults.skills).toEqual({
      visible: ["write-skill", "write-hook"],
    });
    expect(payload.defaults.prompt_context.soul_enabled).toBe(true);
    expect(payload.defaults.prompt_context.custom_enabled).toBe(false);
    expect(payload.defaults.prompt_context.long_term_enabled).toBe(false);
    expect(payload.defaults.prompt_context.user_enabled).toBeUndefined();
  });

  it("enables long-term memory when memory tool group is selected", () => {
    const payload = buildCreateAgentPayload({
      displayName: "有记忆",
      llmProfileId: "default",
      maxToolLoops: 8,
      toolGroups: ["memory", "fs"],
      promptLongTermScope: "global",
      role: "assistant",
      description: "",
    });
    expect(payload.defaults.prompt_context.long_term_enabled).toBe(true);
    expect(payload.defaults.prompt_context.long_term_scope).toBe("global");
  });

  it("does not include placement fields", () => {
    const payload = buildCreateAgentPayload({
      templateId: "general",
      displayName: "远端",
      llmProfileId: "qwen-plus",
      maxToolLoops: 16,
      toolGroups: ["fs"],
    });
    expect(payload.placement).toBeUndefined();
  });

  it("builds patch payload without template_id", () => {
    const patch = buildPatchAgentPayload({
      templateId: "general",
      displayName: "改名",
      llmProfileId: "a",
      maxToolLoops: 8,
      toolGroups: ["fs"],
      promptSoulMd: "角色",
      promptCustomMd: "临时",
      role: "assistant",
      description: "",
    });
    expect(patch.template_id).toBeUndefined();
    expect(patch.display_name).toBe("改名");
    expect(patch.sandbox).toBeUndefined();
    expect(patch.defaults.tools.enabled_groups).toEqual(["fs"]);
    expect(patch.defaults.skills).toEqual({});
    expect(patch.defaults.prompt_context.soul_enabled).toBe(true);
    expect(patch.defaults.prompt_context.custom_enabled).toBe(true);
    expect(patch.defaults.prompt_context.long_term_enabled).toBe(false);
  });

  it("reads draft from agent view", () => {
    const draft = draftFromAgentView(
      {
        display_name: "已有",
        template_id: "general",
        config_snapshot: {
          defaults: {
            llm: { active: "deepseek", max_tool_loops: 10 },
            tools: { enabled_groups: ["bash"] },
            skills: { enabled: true, visible: ["write-skill"] },
            prompt_context: { long_term_enabled: false },
          },
        },
      },
      ["deepseek"],
    );
    expect(draft.displayName).toBe("已有");
    expect(draft.toolGroups).toEqual(["bash"]);
    expect(draft.visibleSkills).toEqual(["write-skill"]);
    expect(draft.llmProfileId).toBe("deepseek");
  });

  it("omits skills.visible when unrestricted", () => {
    const payload = buildCreateAgentPayload({
      displayName: "全技能",
      llmProfileId: "default",
      maxToolLoops: 32,
      toolGroups: ["skills"],
      visibleSkills: null,
      role: "assistant",
      description: "",
    });
    expect(payload.defaults.skills).toEqual({});
    expect(payload.defaults.prompt_context.soul_enabled).toBe(false);
    expect(payload.defaults.prompt_context.custom_enabled).toBe(false);
    expect(payload.defaults.prompt_context.long_term_enabled).toBe(false);
  });

  it("builds create payload without template_id for blank draft", () => {
    const payload = buildCreateAgentPayload({
      templateId: BLANK_TEMPLATE_ID,
      displayName: "空白",
      llmProfileId: "default",
      maxToolLoops: 32,
      toolGroups: [],
      role: "assistant",
      description: "",
    });
    expect(payload.template_id).toBeUndefined();
    expect(payload.display_name).toBe("空白");
    expect(payload.defaults.skills).toEqual({});
    expect(payload.defaults.prompt_context.long_term_enabled).toBe(false);
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
        toolGroups: ["fs", "memory"],
        promptSoulMd: "  ",
        promptCustomMd: "补充",
      },
    );
    expect(payload.id).toBe("my-bot");
    expect(payload.display_name).toBe("我的 Bot");
    expect(payload.defaults.llm).toEqual({ active: "default", max_tool_loops: 20 });
    expect(payload.defaults.tools.enabled_groups).toEqual(["fs", "memory"]);
    expect(payload.defaults.child_agents).toBeUndefined();
    expect(payload.defaults.skills).toEqual({});
    expect(payload.sandbox).toBeUndefined();
    expect(payload.defaults.prompt_context.soul_enabled).toBe(false);
    expect(payload.defaults.prompt_context.custom_enabled).toBe(true);
    expect(payload.defaults.prompt_context.long_term_enabled).toBe(true);
  });

  it("reads llm active from agent view snapshot", () => {
    expect(
      llmActiveFromAgentView({
        config_snapshot: { defaults: { llm: { active: "deepseek" } } },
      }),
    ).toBe("deepseek");
  });
});
