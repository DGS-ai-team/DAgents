import { describe, expect, it } from "vitest";
import {
  WORKGROUP_REALTIME_EVENT_TYPES,
  isKnownWorkgroupRealtimeEvent,
} from "./workgroupEvents.js";

describe("workgroup realtime event registry", () => {
  it("covers the protocol's known ephemeral frames", () => {
    expect(WORKGROUP_REALTIME_EVENT_TYPES).toEqual([
      "queued",
      "status",
      "delta",
      "assistant_final",
      "final",
    ]);
  });

  it("allows unknown frames to take the durable resync path", () => {
    expect(isKnownWorkgroupRealtimeEvent("future.realtime")).toBe(false);
    expect(isKnownWorkgroupRealtimeEvent("status")).toBe(true);
  });
});
