import { beforeEach, describe, expect, it, vi } from "vitest";
import * as api from "../api/node.js";
import { patchBashResultStatus } from "./transcript.js";
import {
  applyToolJobsSnapshot,
  cancelBashToolCall,
  toolJobsStore,
} from "./toolJobs.js";

vi.mock("../api/node.js", () => ({
  cancelAgentToolCall: vi.fn(),
  getAgentToolJobs: vi.fn(),
}));

vi.mock("./transcript.js", () => ({
  patchBashResultStatus: vi.fn(),
}));

describe("cancelBashToolCall", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    applyToolJobsSnapshot({ running: 0, background: 0, running_call_ids: [], background_call_ids: [] });
  });

  it("updates the bubble only after the backend confirms cancellation", async () => {
    vi.mocked(api.cancelAgentToolCall).mockResolvedValueOnce({ cancelled: true });
    vi.mocked(api.getAgentToolJobs).mockResolvedValueOnce({ running: 0, background: 0 });

    await cancelBashToolCall("agt-1", "call-1");

    expect(patchBashResultStatus).toHaveBeenCalledWith("call-1", "CANCELLED");
    expect(toolJobsStore.busyCallIds["call-1"]).toBeUndefined();
  });

  it("keeps the existing state when the backend does not confirm cancellation", async () => {
    vi.mocked(api.cancelAgentToolCall).mockResolvedValueOnce({ cancelled: false });

    await expect(cancelBashToolCall("agt-1", "call-1")).rejects.toThrow("未确认");

    expect(patchBashResultStatus).not.toHaveBeenCalled();
    expect(api.getAgentToolJobs).not.toHaveBeenCalled();
    expect(toolJobsStore.busyCallIds["call-1"]).toBeUndefined();
  });
});
