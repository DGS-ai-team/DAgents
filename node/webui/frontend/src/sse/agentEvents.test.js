import { describe, expect, it } from "vitest";
import {
  AGENT_STREAM_EVENT_POLICIES,
  AGENT_STREAM_EVENT_TYPES,
  getAgentStreamEventPolicy,
} from "./agentEvents.js";

describe("agent stream event registry", () => {
  it("assigns a non-unknown UI policy to every registered event", () => {
    for (const type of AGENT_STREAM_EVENT_TYPES) {
      expect(getAgentStreamEventPolicy(type)).not.toBe("unknown");
      expect(AGENT_STREAM_EVENT_POLICIES[type]).toBeTruthy();
    }
  });

  it("classifies events that are not part of the registry as unknown", () => {
    expect(getAgentStreamEventPolicy("future.backend.event")).toBe("unknown");
  });
});
