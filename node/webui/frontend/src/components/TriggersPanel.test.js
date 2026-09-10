/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import TriggersPanel from "./TriggersPanel.vue";
import * as api from "../api/node.js";

const routerMock = vi.hoisted(() => ({ push: vi.fn() }));
vi.mock("vue-router", () => ({ useRouter: () => routerMock }));

vi.mock("../api/node.js", () => ({
  listTriggers: vi.fn(), listAgents: vi.fn(), fireTrigger: vi.fn(), getTriggerHistory: vi.fn(),
  recoverTrigger: vi.fn(), createTrigger: vi.fn(), updateTrigger: vi.fn(), deleteTrigger: vi.fn(),
}));
vi.mock("./settings/TriggerEditor.vue", () => ({ default: { template: "<div />" } }));

describe("TriggersPanel retired legacy records", () => {
  it("keeps current auto triggers actionable and marks legacy goal records retired", async () => {
    api.listAgents.mockResolvedValue({ agents: [] });
    api.listTriggers.mockResolvedValue({ triggers: [
      { trigger_id: "auto-default-agent", name: "Auto 默认触发", controller: "auto", owner_agent_id: "agent-auto", target_agent_id: "agent-auto", enabled: true, condition: { interval_seconds: 60 } },
      { trigger_id: "legacy-controller", name: "旧任务", controller: "retired", enabled: false, recovery_required: true, condition: { interval_seconds: 60 } },
      { trigger_id: "unknown-maintenance", name: "未知维护", controller: "maintenance", enabled: true, condition: { interval_seconds: 60 } },
      { trigger_id: "user-trigger", name: "用户任务", controller: "user", enabled: true, condition: { interval_seconds: 60 } },
    ] });
    const wrapper = mount(TriggersPanel, { props: { embedded: true } });
    await flushPromises();
    expect(wrapper.text()).toContain("Auto 默认触发");
    expect(wrapper.text()).toContain("已退役");
    expect(wrapper.text()).toContain("配置入口受限 · 仅查看");
    expect(wrapper.text()).not.toContain("自主任务调度");
    expect(wrapper.text()).not.toContain("自主任务托管");
    expect(wrapper.text()).not.toContain("Auto 运行时管理");
    expect(wrapper.find("header").exists()).toBe(false);
    expect(wrapper.findAll("button").some((button) => button.text().trim() === "打开 Auto 设置")).toBe(true);
    expect(wrapper.findAll("button").some((button) => button.text().trim() === "编辑")).toBe(true);
    expect(wrapper.text()).not.toContain("确认并丢弃旧投递");

    const cards = wrapper.findAll(".command-card");
    expect(cards).toHaveLength(4);
    const cardByText = (text) => cards.find((card) => card.text().includes(text));
    const autoCard = cardByText("Auto 默认触发");
    expect(autoCard.findAll("button").map((button) => button.text().trim())).toEqual(["打开 Auto 设置", "查看历史"]);
    await autoCard.find("button").trigger("click");
    expect(routerMock.push).toHaveBeenCalledWith({ name: "settings-agent-detail", params: { agentId: "agent-auto" }, query: { section: "autonomy" } });

    const userCard = cardByText("用户任务");
    expect(userCard.find("input[type=checkbox]").exists()).toBe(true);
    expect(userCard.text()).toContain("编辑");
    expect(userCard.text()).toContain("立即运行");
    expect(userCard.text()).toContain("触发历史");
    expect(userCard.text()).toContain("删除");
    for (const retiredName of ["旧任务", "未知维护"]) {
      const retired = cardByText(retiredName);
      expect(retired.find("input[type=checkbox]").exists()).toBe(false);
      expect(retired.text()).not.toContain("编辑");
      expect(retired.text()).not.toContain("立即运行");
      expect(retired.text()).not.toContain("删除");
    }
  });

  it("renders the non-embedded header", async () => {
    api.listAgents.mockResolvedValue({ agents: [] });
    api.listTriggers.mockResolvedValue({ triggers: [] });
    const wrapper = mount(TriggersPanel, { props: { embedded: false } });
    await flushPromises();
    expect(wrapper.find("header").exists()).toBe(true);
  });

  it("allows Auto recovery with its CAS identity while keeping legacy records read-only", async () => {
    routerMock.push.mockReset();
    api.listAgents.mockResolvedValue({ agents: [] });
    api.listTriggers.mockResolvedValue({ triggers: [
      { trigger_id: "auto-recovery", name: "Auto 唤醒", controller: "auto", owner_agent_id: "agent-auto", target_agent_id: "agent-auto", enabled: false, recovery_required: true, revision: 7, pending_delivery_id: "delivery-7", condition: { interval_seconds: 60 } },
      { trigger_id: "legacy-controller", name: "旧维护", controller: "retired", enabled: false, recovery_required: true, pending_delivery_id: "legacy-delivery" },
    ] });
    api.recoverTrigger.mockResolvedValue({ trigger_id: "auto-recovery", name: "Auto 唤醒", controller: "auto", owner_agent_id: "agent-auto", enabled: false, revision: 8 });
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
    const wrapper = mount(TriggersPanel, { props: { embedded: true } });
    await flushPromises();
    const cards = wrapper.findAll(".command-card");
    const auto = cards.find((card) => card.text().includes("Auto 唤醒"));
    const legacy = cards.find((card) => card.text().includes("旧维护"));
    expect(auto.text()).toContain("确认并丢弃旧投递");
    await auto.find("button").trigger("click");
    expect(api.recoverTrigger).toHaveBeenCalledWith("auto-recovery", "delivery-7", 7);
    expect(legacy.text()).not.toContain("确认并丢弃旧投递");
    confirm.mockRestore();
  });
});
