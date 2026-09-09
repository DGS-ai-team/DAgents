// @vitest-environment jsdom
import { mount } from "@vue/test-utils";
import { nextTick } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";
import AgentMaintenancePanel from "./AgentMaintenancePanel.vue";

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));
const cfg = (revision = 3) => ({
  maintenance_enabled: true,
  maintenance_schedule: "daily 08:30",
  timezone: "Asia/Shanghai",
  profile_revision: revision,
});

describe("AgentMaintenancePanel", () => {
  afterEach(() => vi.restoreAllMocks());
  it("shows recovery and accepts continuation without claiming completion", async () => {
    const fetch = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ ...cfg(), last: { status: "recovery_required", local_date: "2026-09-09", schedule_revision: 2 } }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ local_date: "2026-09-09", schedule_revision: 2, stage: "recovery_required", known: true, used_tokens: 4, token: "secret" }) })
      .mockResolvedValueOnce({ ok: true, status: 202, json: async () => ({ status: "accepted", stage: "pending" }) });
    vi.stubGlobal("fetch", fetch); const w = mount(AgentMaintenancePanel, { props: { agentId: "auto-a" } }); await flush();
    expect(w.text()).toContain("请先核对结果，再继续原维护批次"); await w.findAll("button").find((b) => b.text().includes("核对并继续")).trigger("click"); await flush();
    expect(fetch.mock.calls[2][1].method).toBe("POST"); expect(w.text()).toContain("已接受，等待继续"); expect(w.text()).not.toContain("已完成");
  });
  it("disables other actions and prevents duplicate recovery submits while pending", async () => {
    let resolvePost;
    const fetch = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ ...cfg(), last: { status: "recovery_required", local_date: "2026-09-09", schedule_revision: 2 } }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ local_date: "2026-09-09", schedule_revision: 2, stage: "recovery_required", known: true, token: "secret" }) })
      .mockReturnValueOnce(new Promise((resolve) => { resolvePost = resolve; }));
    vi.stubGlobal("fetch", fetch); const w = mount(AgentMaintenancePanel, { props: { agentId: "auto-a" } }); await flush();
    const recoverButton = w.findAll("button").find((b) => b.text().includes("核对并继续")); await recoverButton.trigger("click"); await nextTick();
    expect(w.findAll("button").find((b) => b.text().includes("保存维护设置")).element.disabled).toBe(true);
    expect(w.findAll("button").find((b) => b.text().includes("立即运行维护")).element.disabled).toBe(true);
    await recoverButton.trigger("click"); expect(fetch).toHaveBeenCalledTimes(3);
    resolvePost({ ok: true, status: 202, json: async () => ({ status: "accepted", stage: "pending" }) }); await flush();
  });

  it("ignores a late recovery response after switching Agent", async () => {
    let resolvePost;
    const fetch = vi.fn().mockImplementation((url, options) => {
      if (options?.method === "POST") return new Promise((resolve) => { resolvePost = resolve; });
      return Promise.resolve({ ok: true, json: async () => url.includes("recovery") ? ({ local_date: "d", schedule_revision: 2, stage: "recovery_required", known: true, token: "t" }) : ({ ...cfg(), last: { status: "recovery_required", local_date: "d", schedule_revision: 2 } }) });
    });
    vi.stubGlobal("fetch", fetch); const w = mount(AgentMaintenancePanel, { props: { agentId: "auto-a" } }); await flush();
    await w.findAll("button").find((b) => b.text().includes("核对并继续")).trigger("click"); await w.setProps({ agentId: "auto-b" }); await flush();
    resolvePost({ ok: true, status: 202, json: async () => ({ status: "accepted", stage: "pending" }) }); await flush();
    expect(w.text()).not.toContain("恢复请求已接受");
  });
  it("refreshes to completed after 202 while keeping an edited settings draft", async () => {
    const fetch = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ ...cfg(), last: { status: "recovery_required", local_date: "d", schedule_revision: 2 } }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ local_date: "d", schedule_revision: 2, stage: "recovery_required", known: true, token: "t" }) })
      .mockResolvedValueOnce({ ok: true, status: 202, json: async () => ({ status: "accepted", stage: "pending" }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ local_date: "d", schedule_revision: 2, stage: "completed", known: true, used_tokens: 3 }) });
    vi.stubGlobal("fetch", fetch); const w = mount(AgentMaintenancePanel, { props: { agentId: "auto-a" } }); await flush();
    await w.find('input[type="text"]').setValue("Europe/Paris"); await w.findAll("button").find((b) => b.text().includes("核对并继续")).trigger("click"); await flush();
    expect(w.text()).toContain("维护已完成"); expect(w.find('input[type="text"]').element.value).toBe("Europe/Paris");
    expect(w.text()).not.toContain("上次维护中断"); expect(w.text()).not.toContain("请先核对结果，再继续原维护批次");
  });

  it("shows a friendly unsupported message for legacy recovery Nodes", async () => {
    const fetch = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ ...cfg(), last: { status: "recovery_required", local_date: "d", schedule_revision: 2 } }) })
      .mockResolvedValueOnce({ ok: false, status: 404, text: async () => "not found" });
    vi.stubGlobal("fetch", fetch); const w = mount(AgentMaintenancePanel, { props: { agentId: "auto-a" } }); await flush();
    expect(w.text()).toContain("当前 Node 版本尚不支持此功能");
  });
  it("loads, saves with expected revision, and shows run result", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce({ ok: true, json: async () => cfg() })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ ...cfg(), profile_revision: 4 }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          status: "completed",
          sequence: 12,
          usage: { business_tokens: 4, maintenance_tokens: 9, unknown: false },
        }),
      });
    vi.stubGlobal("fetch", fetch);
    const w = mount(AgentMaintenancePanel, { props: { agentId: "auto-a" } });
    await flush();
    await nextTick();
    expect(w.find('input[type="time"]').element.value).toBe("08:30");
    await w.find('input[type="text"]').setValue("Asia/Tokyo");
    await w.findAll("button")[0].trigger("click");
    expect(JSON.parse(fetch.mock.calls[1][1].body)).toMatchObject({
      expected_revision: 3,
      timezone: "Asia/Tokyo",
    });
    await flush();
    await nextTick();
    await w.findAll("button")[1].trigger("click");
    await flush();
    await nextTick();
    expect(w.text()).toContain("累计维护用量：9 tokens");
    expect(w.text()).toContain("本次维护状态：已完成");
  });

  it.each([
    ["pending", "等待维护"],
    ["recovery_required", "需要恢复"],
    ["unexpected_status", "未知状态"],
    [undefined, "状态未知"],
  ])("renders maintenance result status %s without claiming completion", async (status, label) => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce({ ok: true, json: async () => cfg() })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ status, usage: { maintenance_tokens: 6, unknown: false } }),
      });
    vi.stubGlobal("fetch", fetch);
    const w = mount(AgentMaintenancePanel, { props: { agentId: "auto-a" } });
    await flush();
    await w.findAll("button")[1].trigger("click");
    await flush();
    await nextTick();
    expect(w.text()).toContain(`本次维护状态：${label}`);
    if (label !== "已完成") expect(w.text()).not.toContain("本次维护状态：已完成");
    expect(w.text()).toContain("累计维护用量：6 tokens");
  });
  it("prevents duplicate run and ignores a late result after agent switch", async () => {
    let resolveRun;
    const fetch = vi.fn().mockImplementation((url, opts) => {
      if (opts?.method === "POST")
        return new Promise((resolve) => {
          resolveRun = resolve;
        });
      return Promise.resolve({ ok: true, json: async () => cfg() });
    });
    vi.stubGlobal("fetch", fetch);
    const w = mount(AgentMaintenancePanel, { props: { agentId: "auto-a" } });
    await flush();
    await w.findAll("button")[1].trigger("click");
    await w.findAll("button")[1].trigger("click");
    expect(
      fetch.mock.calls.filter((c) => c[1]?.method === "POST"),
    ).toHaveLength(1);
    await w.setProps({ agentId: "auto-b" });
    resolveRun({ ok: true, json: async () => ({ sequence: 99 }) });
    await flush();
    await nextTick();
    expect(w.text()).not.toContain("99");
  });

  it("allows retry after load failure and keeps save/run mutually exclusive", async () => {
    let retry = false;
    const fetch = vi.fn().mockImplementation((url, opts) => {
      if (opts?.method === "POST") return new Promise(() => {});
      if (opts?.method === "PATCH")
        return Promise.resolve({ ok: true, json: async () => cfg(4) });
      if (!retry)
        return Promise.resolve({ ok: false, text: async () => "暂时失败" });
      return Promise.resolve({ ok: true, json: async () => cfg() });
    });
    vi.stubGlobal("fetch", fetch);
    const w = mount(AgentMaintenancePanel, { props: { agentId: "auto-a" } });
    await flush();
    await nextTick();
    expect(w.find("button").text()).toContain("重新加载");
    retry = true;
    await w.find("button").trigger("click");
    await flush();
    await nextTick();
    await w.findAll("button")[1].trigger("click");
    await w.findAll("button")[0].trigger("click");
    expect(
      fetch.mock.calls.filter((c) => c[1]?.method === "POST"),
    ).toHaveLength(1);
    expect(
      fetch.mock.calls.filter((c) => c[1]?.method === "PATCH"),
    ).toHaveLength(0);
  });

  it("shows unknown usage and explains recovery-required status", async () => {
    const fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        ...cfg(),
        usage: { unknown: true },
        last: { status: "recovery_required" },
        next_at: null,
      }),
    });
    vi.stubGlobal("fetch", fetch);
    const w = mount(AgentMaintenancePanel, { props: { agentId: "auto-a" } });
    await flush();
    await nextTick();
    expect(w.text()).toContain("累计维护用量：待对账");
    expect(w.text()).toContain("需要恢复");
    expect(w.text()).toContain("上次维护中断，需要核对执行结果和用量");
    expect(w.text()).toContain("下次维护：未安排");
  });

  it("does not apply a late save response after switching agents", async () => {
    let resolveSave;
    const fetch = vi.fn().mockImplementation((url, opts) => {
      if (opts?.method === "PATCH")
        return new Promise((resolve) => {
          resolveSave = resolve;
        });
      return Promise.resolve({ ok: true, json: async () => cfg() });
    });
    vi.stubGlobal("fetch", fetch);
    const w = mount(AgentMaintenancePanel, { props: { agentId: "auto-a" } });
    await flush();
    await nextTick();
    await w.findAll("button")[0].trigger("click");
    await w.setProps({ agentId: "auto-b" });
    resolveSave({
      ok: true,
      json: async () => ({ ...cfg(), timezone: "Asia/Old" }),
    });
    await flush();
    await nextTick();
    expect(w.find('input[type="text"]').element.value).toBe("Asia/Shanghai");
  });

  it.each([
    [null],
    [{}],
    [{ maintenance_tokens: null }],
    [{ maintenance_tokens: -1 }],
  ])("shows unaccounted usage for %o", async (value) => {
    const fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ ...cfg(), usage: value }),
    });
    vi.stubGlobal("fetch", fetch);
    const w = mount(AgentMaintenancePanel, { props: { agentId: "auto-a" } });
    await flush();
    expect(w.text()).toContain("累计维护用量：待对账");
  });

  it("keeps edited fields after a save conflict until reload is clicked", async () => {
    let calls = 0;
    const fetch = vi.fn().mockImplementation((url, opts) => {
      if (opts?.method === "PATCH")
        return Promise.resolve({
          ok: false,
          status: 409,
          text: async () => "conflict",
        });
      calls += 1;
      return Promise.resolve({
        ok: true,
        json: async () => cfg(calls === 1 ? 3 : 4),
      });
    });
    vi.stubGlobal("fetch", fetch);
    const w = mount(AgentMaintenancePanel, { props: { agentId: "auto-a" } });
    await flush();
    await w.find('input[type="text"]').setValue("Asia/Tokyo");
    await w.findAll("button")[0].trigger("click");
    await flush();
    expect(w.text()).toContain("请刷新后重试");
    expect(w.find('input[type="text"]').element.value).toBe("Asia/Tokyo");
    const reload = w.find("button.maintenance-reload");
    expect(reload.exists()).toBe(true);
    await reload.trigger("click");
    await flush();
    expect(w.find('input[type="text"]').element.value).toBe("Asia/Shanghai");
  });

  it("explains when the Node version lacks maintenance", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValue({
        ok: false,
        status: 404,
        text: async () => "404 page not found",
      });
    vi.stubGlobal("fetch", fetch);
    const w = mount(AgentMaintenancePanel, { props: { agentId: "auto-a" } });
    await flush();
    expect(w.text()).toContain(
      "当前 Node 版本尚不支持此功能，请更新 Node 后重试",
    );
    expect(w.text()).not.toContain("404 page not found");
    expect(w.text()).toContain("重新加载");
  });
});
