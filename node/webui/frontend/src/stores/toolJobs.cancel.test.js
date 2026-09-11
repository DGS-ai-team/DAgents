import { beforeEach, describe, expect, it, vi } from "vitest";
import * as api from "../api/node.js";
import {
  applyToolJobsSnapshot,
  cancelBashToolCall,
  toolJobsStore,
} from "./toolJobs.js";

vi.mock("../api/node.js", () => ({
  cancelAgentToolCall: vi.fn(),
  getAgentToolJobs: vi.fn(),
}));

describe("cancelBashToolCall", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    applyToolJobsSnapshot({ running: 0, running_call_ids: [] });
  });

  it("refreshes execution state after the backend confirms cancellation", async () => {
    vi.mocked(api.cancelAgentToolCall).mockResolvedValueOnce({ cancelled: true, scope: "tool_execution" });
    vi.mocked(api.getAgentToolJobs).mockResolvedValueOnce({ running: 0, running_call_ids: [] });

    await cancelBashToolCall("agt-1", "call-1");

    expect(api.getAgentToolJobs).toHaveBeenCalledWith("agt-1");
    expect(toolJobsStore.busyCallIds["call-1"]).toBeUndefined();
  });

  it("keeps the existing state when the backend does not confirm cancellation", async () => {
    vi.mocked(api.cancelAgentToolCall).mockResolvedValueOnce({ cancelled: false });

    await expect(cancelBashToolCall("agt-1", "call-1")).rejects.toThrow("未确认");

    expect(api.getAgentToolJobs).not.toHaveBeenCalled();
    expect(toolJobsStore.busyCallIds["call-1"]).toBeUndefined();
  });
});
