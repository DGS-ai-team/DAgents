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
});
