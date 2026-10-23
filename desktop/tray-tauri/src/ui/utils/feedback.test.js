import { describe, expect, it } from "vitest";
import { feedbackDelivery, feedbackDraft, feedbackProcessing, hasDestinationChanged, unwrapFeedback } from "./feedback.js";

describe("feedback presentation", () => {
  it("keeps delivery and processing states independent", () => {
    const item = { delivery_status: "delivered", processing_status: "processing" };
    expect(feedbackDelivery(item)).toBe("已送达");
    expect(feedbackProcessing(item)).toBe("处理中");
  });

  it("unwraps API envelopes and recognizes destination changes", () => {
    const item = { feedback_id: "f-1", manage_changed: true };
    expect(unwrapFeedback({ feedback: item })).toBe(item);
    expect(hasDestinationChanged({ feedback: item })).toBe(true);
  });

  it("sanitizes malformed local drafts into plain text fields", () => {
    expect(feedbackDraft({ type: "other", title: 42, body: null })).toEqual({
      type: "problem",
      title: "42",
      body: "",
    });
  });
});
