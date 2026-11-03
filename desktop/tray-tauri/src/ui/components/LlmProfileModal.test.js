// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import LlmProfileModal from "./LlmProfileModal.vue";

const { getLLMCapabilities, probeLLMModels } = vi.hoisted(() => ({
  getLLMCapabilities: vi.fn(),
  probeLLMModels: vi.fn(),
}));

vi.mock("../api/node.js", () => ({ getLLMCapabilities, probeLLMModels }));

let wrapper;

afterEach(() => {
  wrapper?.unmount();
  wrapper = null;
  document.body.innerHTML = "";
  vi.resetAllMocks();
});

describe("LlmProfileModal reasoning profile controls", () => {
  it("uses localized provider/model labels and keeps Mock recognizable in test mode", async () => {
    getLLMCapabilities.mockResolvedValue({ supported: false });
    wrapper = mount(LlmProfileModal, {
      attachTo: document.body,
      props: {
        open: true,
        mode: "edit",
        profile: { id: "openai", provider: "openai", model: "gpt-5.1", mock: true },
      },
    });
    await flushPromises();

    expect(document.body.textContent).toContain("服务商");
    expect(document.body.textContent).toContain("模型");
    expect(document.body.textContent).toContain("测试模式（Mock）");
    expect(document.body.querySelector('[aria-label="服务商"]')).toBeTruthy();
    expect(document.body.querySelector('[aria-label="模型"]')).toBeTruthy();
  });

  it("keeps a Qwen budget as a number and leaves empty budgets to the provider default", async () => {
    getLLMCapabilities.mockResolvedValue({
      supported: true,
      thinking_control: "budget",
      thinking_label: "深度思考",
      thinking_secondary_label: "思考预算（Token）",
      can_disable_thinking: true,
      thinking_budget_min: 1,
      thinking_budget_max: 32768,
    });
    wrapper = mount(LlmProfileModal, {
      attachTo: document.body,
      props: {
        open: true,
        mode: "edit",
        profile: { id: "qwen", provider: "qwen", model: "qwen-plus", thinking_budget: null },
      },
    });
    await flushPromises();

    const budget = document.body.querySelector("input[type='number']");
    expect(budget).toBeTruthy();
    expect(budget.placeholder).toContain("跟随服务默认");
    budget.value = "12000";
    budget.dispatchEvent(new Event("input", { bubbles: true }));
    await flushPromises();

    document.body.querySelector(".btn--primary").click();
    const [payload] = wrapper.emitted("confirm")[0];
    expect(payload.thinking_budget).toBe(12000);
    expect(payload.clear_thinking_budget).toBe(false);
  });

  it("clears stored controls when the edited model becomes unsupported", async () => {
    getLLMCapabilities.mockImplementation(async (_provider, model) => ({
      supported: model === "qwen-plus",
      thinking_control: model === "qwen-plus" ? "budget" : "",
      can_disable_thinking: model === "qwen-plus",
      thinking_budget_min: 1,
      thinking_budget_max: 32768,
    }));
    wrapper = mount(LlmProfileModal, {
      attachTo: document.body,
      props: {
        open: true,
        mode: "edit",
        profile: { id: "qwen", provider: "qwen", model: "qwen-plus", thinking_budget: 10000 },
      },
    });
    await flushPromises();

    const model = document.body.querySelector("input[placeholder='模型名称']");
    model.value = "custom-model";
    model.dispatchEvent(new Event("input", { bubbles: true }));
    await flushPromises();

    expect(document.body.querySelector("input[type='number']")).toBeNull();
    document.body.querySelector(".btn--primary").click();
    const [payload] = wrapper.emitted("confirm")[0];
    expect(payload.thinking_budget).toBeNull();
    expect(payload.clear_thinking_budget).toBe(true);
  });

  it("explains that GPT-5.1 can disable reasoning through the effort selector", async () => {
    getLLMCapabilities.mockResolvedValue({
      supported: true,
      thinking_control: "effort",
      thinking_secondary_label: "推理强度",
      allowed_efforts: ["none", "low", "medium", "high"],
      default_effort: "medium",
      can_disable_thinking: false,
    });
    wrapper = mount(LlmProfileModal, {
      attachTo: document.body,
      props: {
        open: true,
        mode: "edit",
        profile: { id: "openai", provider: "openai", model: "gpt-5.1" },
      },
    });
    await flushPromises();

    expect(document.body.textContent).toContain("可在推理强度中选择关闭。");
    expect(document.body.textContent).not.toContain("此模型始终启用思考。");
  });
});
