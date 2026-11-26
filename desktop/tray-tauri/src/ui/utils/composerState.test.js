import { describe, expect, it } from "vitest";
import { normalComposerIntent } from "./composerState.js";

describe("normal composer intent", () => {
  it("queues ordinary messages during generation, including pending approvals", () => {
    expect(normalComposerIntent({ turnRunning: true })).toBe("queue");
    expect(normalComposerIntent({ turnRunning: true, interactionPending: true })).toBe("queue");
  });

  it("keeps pending interaction decisions separate when no turn is running", () => {
    expect(normalComposerIntent({ interactionPending: true })).toBe("blocked");
  });

  it("sends normally while idle", () => {
    expect(normalComposerIntent()).toBe("send");
    expect(normalComposerIntent({ turnRunning: false, interactionPending: false })).toBe("send");
  });
});
