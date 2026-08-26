import { describe, expect, it } from "vitest";
import { createSerializedRefresh } from "../../../../../shared/frontend/serializedRefresh.js";

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe("createSerializedRefresh", () => {
  it("coalesces overlapping refreshes and does not apply the intermediate snapshot", async () => {
    const requests = [];
    const applied = [];
    const refresh = createSerializedRefresh(
      () => {
        const request = deferred();
        requests.push(request);
        return request.promise;
      },
      (snapshot) => applied.push(snapshot),
    );

    const first = refresh.refresh();
    const second = refresh.refresh();
    expect(second).toBe(first);
    expect(requests).toHaveLength(1);

    requests[0].resolve([]);
    await Promise.resolve();
    await Promise.resolve();
    expect(requests).toHaveLength(2);
    expect(applied).toEqual([]);

    requests[1].resolve([{ hitl_id: "pending-1" }]);
    await first;
    expect(applied).toEqual([[{ hitl_id: "pending-1" }]]);
  });

  it("ignores an old generation after the workgroup changes", async () => {
    const oldRequest = deferred();
    const newRequest = deferred();
    const requests = [oldRequest, newRequest];
    const applied = [];
    const refresh = createSerializedRefresh(
      () => requests.shift().promise,
      (snapshot) => applied.push(snapshot),
    );

    const oldRefresh = refresh.refresh();
    refresh.reset();
    const newRefresh = refresh.refresh();
    expect(newRefresh).not.toBe(oldRefresh);

    oldRequest.resolve([{ hitl_id: "old-workgroup" }]);
    newRequest.resolve([{ hitl_id: "new-workgroup" }]);
    await Promise.all([oldRefresh, newRefresh]);

    expect(applied).toEqual([[{ hitl_id: "new-workgroup" }]]);
  });

  it("continues with a queued refresh after a transient failure", async () => {
    const requests = [];
    const applied = [];
    const refresh = createSerializedRefresh(
      () => {
        const request = deferred();
        requests.push(request);
        return request.promise;
      },
      (snapshot) => applied.push(snapshot),
    );

    const run = refresh.refresh();
    refresh.refresh();
    requests[0].reject(new Error("temporary failure"));
    await Promise.resolve();
    await Promise.resolve();
    expect(requests).toHaveLength(2);

    requests[1].resolve([{ hitl_id: "pending-after-retry" }]);
    await run;
    expect(applied).toEqual([[{ hitl_id: "pending-after-retry" }]]);
  });
});
