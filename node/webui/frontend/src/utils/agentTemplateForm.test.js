import { describe, expect, it } from "vitest";
import { buildCreateAgentPayload, draftFromTemplate, llmActiveFromAgentView } from "./agentTemplateForm.js";

describe("agentTemplateForm", () => {
  it("builds draft from template with llm profile bind", () => {
    const draft = draftFromTemplate(
      {
        id: "ops-runner",
        display_name: "运维执行助手",
        description: "沙箱运维",
        defaults: {
          llm: { active: "deepseek", max_tool_loops: 24 },
        },
        sandbox: { enabled: true, backend: "process" },
      },
      ["default", "deepseek"],
    );
    expect(draft.templateId).toBe("ops-runner");
    expect(draft.sandboxEnabled).toBe(true);
    expect(draft.llmProfileId).toBe("deepseek");
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

  it("builds create payload with llm active only", () => {
    const payload = buildCreateAgentPayload({
      templateId: "general",
      displayName: "我的助手",
      sandboxEnabled: false,
      sandboxBackend: "process",
      llmProfileId: "qwen-plus",
    });
    expect(payload.templateId).toBe("general");
    expect(payload.defaults).toEqual({ llm: { active: "qwen-plus" } });
    expect(payload.defaults.tools).toBeUndefined();
    expect(payload.defaults.hooks).toBeUndefined();
  });

  it("reads llm active from agent view snapshot", () => {
    expect(
      llmActiveFromAgentView({
        config_snapshot: { defaults: { llm: { active: "deepseek" } } },
      }),
    ).toBe("deepseek");
  });
});
