import { describe, expect, it, vi } from "vitest";
import { createMediaUrlRegistry } from "./mediaUrlRegistry.js";

describe("media URL ownership", () => {
  it("keeps a committed object URL until its owning transcript releases it", async () => {
    const revoke = vi.fn();
    const registry = createMediaUrlRegistry({ createObjectURL: () => "blob:one", revokeObjectURL: revoke });
    const lease = registry.begin("agent-a");
    const url = await registry.acquire(lease, "media-1", async () => new Blob(["one"]));

    expect(registry.commit(lease)).toBe(true);
    expect(revoke).not.toHaveBeenCalled();
    expect(registry.release("agent-a")).toBe(true);
    expect(revoke).toHaveBeenCalledOnce();
    expect(revoke).toHaveBeenCalledWith(url);
  });

  it("retains a 1,000-item media window beyond the former 64-URL cap until replacement or release", async () => {
    let sequence = 0;
    const revoke = vi.fn();
    const load = vi.fn(async () => new Blob(["media"]));
    const registry = createMediaUrlRegistry({
      createObjectURL: () => `blob:stress-${++sequence}`,
      revokeObjectURL: revoke,
    });
    const resourceIds = Array.from({ length: 1000 }, (_, index) => `media-${index}`);
    const first = registry.begin("agent-a");
    const originalUrls = await Promise.all(resourceIds.map(id => registry.acquire(first, id, load)));

    expect(originalUrls).toHaveLength(1000);
    expect(new Set(originalUrls).size).toBe(1000);
    expect(revoke).not.toHaveBeenCalled();
    expect(registry.commit(first)).toBe(true);

    const refresh = registry.begin("agent-a");
    const shouldNotReload = vi.fn(async () => new Blob(["unexpected reload"]));
    const refreshedUrls = await Promise.all(resourceIds.map(id => registry.acquire(refresh, id, shouldNotReload)));
    expect(refreshedUrls).toEqual(originalUrls);
    expect(shouldNotReload).not.toHaveBeenCalled();
    expect(registry.commit(refresh)).toBe(true);
    expect(revoke).not.toHaveBeenCalled();

    expect(registry.release("agent-a")).toBe(true);
    expect(revoke).toHaveBeenCalledTimes(1000);
    expect(new Set(revoke.mock.calls.map(([url]) => url)).size).toBe(1000);
  });

  it("replaces an owner's reference set only when the new transcript is committed", async () => {
    let sequence = 0;
    const revoke = vi.fn();
    const registry = createMediaUrlRegistry({
      createObjectURL: () => `blob:${++sequence}`,
      revokeObjectURL: revoke,
    });
    const first = registry.begin("agent-a");
    const firstUrl = await registry.acquire(first, "old", async () => new Blob(["old"]));
    registry.commit(first);

    const next = registry.begin("agent-a");
    const nextUrl = await registry.acquire(next, "new", async () => new Blob(["new"]));
    expect(revoke).not.toHaveBeenCalled();
    registry.commit(next);

    expect(revoke).toHaveBeenCalledExactlyOnceWith(firstUrl);
    const sameOwner = registry.begin("agent-a");
    await registry.acquire(sameOwner, "new", async () => new Blob(["duplicate"]));
    registry.commit(sameOwner);
    expect(nextUrl).toBe("blob:2");
    expect(revoke).toHaveBeenCalledOnce();
  });

  it("keeps identical media IDs isolated across Agent owners", async () => {
    let sequence = 0;
    const revoke = vi.fn();
    const registry = createMediaUrlRegistry({
      createObjectURL: () => `blob:agent-${++sequence}`,
      revokeObjectURL: revoke,
    });
    const agentA = registry.begin("agent-a");
    const agentB = registry.begin("agent-b");
    const [urlA, urlB] = await Promise.all([
      registry.acquire(agentA, "media-shared-id", async () => new Blob(["a"])),
      registry.acquire(agentB, "media-shared-id", async () => new Blob(["b"])),
    ]);

    expect(urlA).not.toBe(urlB);
    registry.commit(agentA);
    registry.commit(agentB);
    expect(registry.release("agent-a")).toBe(true);
    expect(revoke).toHaveBeenCalledExactlyOnceWith(urlA);

    const agentBRefresh = registry.begin("agent-b");
    await expect(registry.acquire(agentBRefresh, "media-shared-id", async () => new Blob(["duplicate"])))
      .resolves.toBe(urlB);
    registry.commit(agentBRefresh);
    expect(revoke).toHaveBeenCalledExactlyOnceWith(urlA);
    expect(registry.release("agent-b")).toBe(true);
    expect(revoke).toHaveBeenCalledTimes(2);
    expect(revoke).toHaveBeenLastCalledWith(urlB);
  });

  it("does not let a discarded cross-Agent restore revoke the current Agent's URL", async () => {
    const revoke = vi.fn();
    let sequence = 0;
    const registry = createMediaUrlRegistry({
      createObjectURL: () => `blob:restore-${++sequence}`,
      revokeObjectURL: revoke,
    });
    const current = registry.begin("agent-current");
    const currentUrl = await registry.acquire(current, "media-1", async () => new Blob(["current"]));
    registry.commit(current);

    let finishStaleLoad;
    const stale = registry.begin("agent-stale");
    const staleLoad = registry.acquire(stale, "media-1", () => new Promise(resolve => { finishStaleLoad = resolve; }));
    await Promise.resolve();
    registry.discard(stale);
    finishStaleLoad(new Blob(["stale"]));
    const staleUrl = await staleLoad;

    expect(staleUrl).not.toBe(currentUrl);
    expect(revoke).toHaveBeenCalledExactlyOnceWith(staleUrl);
    const retained = registry.begin("agent-current");
    await expect(registry.acquire(retained, "media-1", async () => new Blob(["duplicate"])))
      .resolves.toBe(currentUrl);
    registry.commit(retained);
    expect(revoke).toHaveBeenCalledExactlyOnceWith(staleUrl);
    registry.release("agent-current");
    expect(revoke).toHaveBeenCalledTimes(2);
    expect(revoke).toHaveBeenLastCalledWith(currentUrl);
  });

  it("shares concurrent loads and lets a committed lease retain the URL after another is discarded", async () => {
    let finishLoad;
    const load = vi.fn(() => new Promise(resolve => { finishLoad = resolve; }));
    const revoke = vi.fn();
    const registry = createMediaUrlRegistry({ createObjectURL: () => "blob:shared", revokeObjectURL: revoke });
    const abandoned = registry.begin("agent-a");
    const retained = registry.begin("agent-a");
    const first = registry.acquire(abandoned, "media-1", load);
    const second = registry.acquire(retained, "media-1", load);

    await Promise.resolve();
    expect(load).toHaveBeenCalledOnce();
    registry.discard(abandoned);
    finishLoad(new Blob(["shared"]));
    await expect(first).resolves.toBe("blob:shared");
    await expect(second).resolves.toBe("blob:shared");
    registry.commit(retained);
    expect(revoke).not.toHaveBeenCalled();
    registry.release("agent-a");
    expect(revoke).toHaveBeenCalledOnce();
  });

  it("revokes a URL produced for a discarded restore lease", async () => {
    const revoke = vi.fn();
    const registry = createMediaUrlRegistry({ createObjectURL: () => "blob:stale", revokeObjectURL: revoke });
    const lease = registry.begin("agent-a");
    let finishLoad;
    const pending = registry.acquire(lease, "media-1", () => new Promise(resolve => { finishLoad = resolve; }));
    await Promise.resolve();
    registry.discard(lease);
    finishLoad(new Blob(["stale"]));

    await expect(pending).resolves.toBe("blob:stale");
    expect(revoke).toHaveBeenCalledExactlyOnceWith("blob:stale");
  });

  it("allows retrying an object URL creation failure without retaining a rejected load", async () => {
    let createCount = 0;
    const registry = createMediaUrlRegistry({
      createObjectURL: () => {
        createCount += 1;
        if (createCount === 1) throw new Error("URL unavailable");
        return "blob:retry";
      },
      revokeObjectURL: vi.fn(),
    });
    const first = registry.begin("agent-a");
    await expect(registry.acquire(first, "media-1", async () => new Blob(["media"]))).rejects.toThrow("URL unavailable");
    registry.discard(first);

    const retry = registry.begin("agent-a");
    await expect(registry.acquire(retry, "media-1", async () => new Blob(["media"]))).resolves.toBe("blob:retry");
    registry.commit(retry);
  });

  it("releases active URLs at Desktop teardown and cleans pending URLs when they settle", async () => {
    const revoke = vi.fn();
    const registry = createMediaUrlRegistry({ createObjectURL: () => "blob:teardown", revokeObjectURL: revoke });
    const active = registry.begin("agent-a");
    await registry.acquire(active, "active", async () => new Blob(["active"]));
    registry.commit(active);
    const pending = registry.begin("agent-a");
    let finishLoad;
    const pendingUrl = registry.acquire(pending, "pending", () => new Promise(resolve => { finishLoad = resolve; }));
    await Promise.resolve();
    registry.dispose();
    expect(revoke).toHaveBeenCalledOnce();
    finishLoad(new Blob(["pending"]));
    await pendingUrl;
    expect(revoke).toHaveBeenCalledTimes(2);
  });
});
