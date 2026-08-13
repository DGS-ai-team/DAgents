import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  hasWorkgroupUnread,
  markWorkgroupRead,
  noteWorkgroupTimeline,
  resetUnreadStoreForTests,
} from "./unread.js";

const mockGetItem = vi.fn(() => "{}");
const mockSetItem = vi.fn();

vi.stubGlobal("localStorage", {
  getItem: mockGetItem,
  setItem: mockSetItem,
});

beforeEach(() => {
  resetUnreadStoreForTests();
  mockGetItem.mockReset();
  mockSetItem.mockReset();
  mockGetItem.mockReturnValue("{}");
});

describe("workgroup unread store", () => {
  it("reports a timeline newer than the local read cursor", () => {
    noteWorkgroupTimeline("wg-1", 4);
    expect(hasWorkgroupUnread("wg-1")).toBe(true);

    markWorkgroupRead("wg-1", 4);
    expect(hasWorkgroupUnread("wg-1")).toBe(false);
    expect(mockSetItem).toHaveBeenCalledOnce();
  });

  it("does not move the read cursor backwards", () => {
    noteWorkgroupTimeline("wg-2", 8);
    markWorkgroupRead("wg-2", 8);
    noteWorkgroupTimeline("wg-2", 6);
    markWorkgroupRead("wg-2", 6);

    expect(hasWorkgroupUnread("wg-2")).toBe(false);
    expect(mockSetItem).toHaveBeenCalledOnce();
  });
});
