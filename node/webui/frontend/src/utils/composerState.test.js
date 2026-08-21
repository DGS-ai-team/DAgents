import { describe, expect, it } from "vitest";
import {
  canSubmitComposer,
  hasPendingUserInformation,
  shouldShowCancel,
} from "./composerState.js";

describe("composer HITL state", () => {
  it("allows a user-information answer while the Turn is sending", () => {
    expect(hasPendingUserInformation([{ kind: "user_information" }])).toBe(true);
    expect(canSubmitComposer({
      sending: true,
      hasUserInformation: true,
      hasContent: true,
    })).toBe(true);
    expect(shouldShowCancel({
      sending: true,
      hasUserInformation: true,
    })).toBe(false);
  });

  it("keeps the ordinary sending gate for a draft message", () => {
    expect(hasPendingUserInformation([{ kind: "approval" }])).toBe(false);
    expect(canSubmitComposer({
      sending: true,
      hasContent: true,
    })).toBe(false);
    expect(shouldShowCancel({ sending: true })).toBe(true);
  });

  it("does not submit while the HITL request itself is busy", () => {
    expect(canSubmitComposer({
      sending: true,
      hitlBusy: true,
      hasUserInformation: true,
      hasContent: true,
    })).toBe(false);
  });
});
