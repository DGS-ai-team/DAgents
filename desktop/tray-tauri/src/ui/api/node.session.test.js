/** @vitest-environment jsdom */
import { afterEach, describe, expect, it, vi } from "vitest";
import { createAgentTodo, deleteAgentTodo, getAgentBrowserView, pickPlatformDirectory, putAutoConfig, updateAgentTodo } from "./node.js";

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });

describe("simplified Auto todo API", () => {
  it("uses the correct verbs and CAS request bodies", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async () => ({ ok: true, json: async () => ({}) }));
    await putAutoConfig("a/1", { expected_revision: 2, responsibility: "owner" });
    await createAgentTodo("a/1", { text: "x" });
    await updateAgentTodo("a/1", "t/2", { expected_revision: 3, status: "completed" });
    await deleteAgentTodo("a/1", "t/2", 4);
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ method: "PUT" });
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ expected_revision: 2, responsibility: "owner" });
    expect(fetchMock.mock.calls[1][1]).toMatchObject({ method: "POST" });
    expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({ text: "x" });
    expect(fetchMock.mock.calls[2][1]).toMatchObject({ method: "PATCH" });
    expect(JSON.parse(fetchMock.mock.calls[2][1].body).expected_revision).toBe(3);
    expect(fetchMock.mock.calls[3][1]).toMatchObject({ method: "DELETE" });
    expect(JSON.parse(fetchMock.mock.calls[3][1].body)).toEqual({ expected_revision: 4 });
    expect(String(fetchMock.mock.calls[2][0])).toContain("t%2F2");
  });
});

describe("read-only Agent browser view API", () => {
  it("uses the authenticated Desktop JSON bridge with the Agent-owned path and optional page id", async () => {
    const request = vi.fn(async () => ({ page_id: "page-1", image_base64: "cG5n" }));
    vi.stubGlobal("__DAGENTS_API_REQUEST__", request);
    await getAgentBrowserView("agent/1", "page/2");
    expect(request).toHaveBeenCalledWith({ method: "GET", path: "/v1/agents/agent%2F1/browser/view?page_id=page%2F2", body: undefined });
  });
});

describe("platform directory picker", () => {
  it("uses the Desktop-owned native dialog when the Desktop bridge is available", async () => {
    const picker = vi.fn(async () => ({ ok: true, cancelled: false, path: "D:\\Projects\\agent" }));
    const fetchMock = vi.spyOn(globalThis, "fetch");
    vi.stubGlobal("__DAGENTS_PICK_DIRECTORY__", picker);

    await expect(pickPlatformDirectory()).resolves.toEqual({ ok: true, cancelled: false, path: "D:\\Projects\\agent" });
    expect(picker).toHaveBeenCalledTimes(1);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("uses the Node API when the native picker bridge is not injected", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue({ ok: true, json: async () => ({ ok: true, cancelled: true }) });
    await expect(pickPlatformDirectory()).resolves.toEqual({ ok: true, cancelled: true });
    expect(String(fetchMock.mock.calls[0][0])).toContain("/v1/platform/directory-picker");
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ method: "POST" });
  });
});
