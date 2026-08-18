import { describe, expect, it } from "vitest";
import { transferStore, replaceTransfers, applyTransferEvent } from "./transfers.js";

describe("transfer store", () => {
  it("hydrates and updates a transfer without replacing unrelated rows", () => {
    replaceTransfers({
      max_concurrent_files: 2,
      transfers: [{ transfer_id: "t-1", status: "queued", progress: 0 }],
    });
    applyTransferEvent({ transfer_id: "t-1", status: "transferring", progress: 42, bytes_done: 42 });
    applyTransferEvent({ transfer_id: "t-2", status: "completed", progress: 100 });

    expect(transferStore.maxConcurrentFiles).toBe(2);
    expect(transferStore.items).toHaveLength(2);
    expect(transferStore.items.find((item) => item.transfer_id === "t-1").progress).toBe(42);
    expect(transferStore.items.find((item) => item.transfer_id === "t-2").status).toBe("completed");
  });
});
