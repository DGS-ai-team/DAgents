import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import {
  SCROLL_TAIL_THRESHOLD,
  createFollowTailController,
  distanceFromTail,
  isNearTail,
  pinToTail,
} from "./scrollTail.js";

function fakeScroller({ scrollHeight, clientHeight, scrollTop = 0 }) {
  return {
    scrollHeight,
    clientHeight,
    scrollTop,
    style: { scrollBehavior: "smooth" },
  };
}

describe("scrollTail helpers", () => {
  it("isNearTail uses threshold from bottom", () => {
    const el = fakeScroller({ scrollHeight: 1000, clientHeight: 400, scrollTop: 552 });
    // distance = 1000 - 552 - 400 = 48
    expect(distanceFromTail(el)).toBe(48);
    expect(isNearTail(el, SCROLL_TAIL_THRESHOLD)).toBe(true);
    el.scrollTop = 551;
    expect(isNearTail(el, SCROLL_TAIL_THRESHOLD)).toBe(false);
  });

  it("pinToTail forces instant scrollBehavior then restores", () => {
    const el = fakeScroller({ scrollHeight: 800, clientHeight: 200, scrollTop: 0 });
    el.style.scrollBehavior = "smooth";
    pinToTail(el);
    expect(el.scrollTop).toBe(800);
    expect(el.style.scrollBehavior).toBe("smooth");
  });
});

describe("createFollowTailController — streaming smooth desync repro", () => {
  let rafQueue;

  beforeEach(() => {
    rafQueue = [];
    vi.stubGlobal("requestAnimationFrame", (fn) => {
      rafQueue.push(fn);
      return rafQueue.length;
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  function flushRaf() {
    const q = rafQueue.splice(0, rafQueue.length);
    for (const fn of q) fn();
  }

  it("repro: scroll events during smooth pin must not clear followTail", () => {
    const ctrl = createFollowTailController();
    const el = fakeScroller({ scrollHeight: 500, clientHeight: 200, scrollTop: 300 });
    // at bottom initially
    expect(isNearTail(el)).toBe(true);

    // content grows; pin starts (smooth CSS would animate — we simulate mid-animation)
    el.scrollHeight = 900;
    ctrl.pinIfFollowing(el);
    expect(ctrl.isPinning()).toBe(true);
    // mid-smooth: still far from new bottom
    el.scrollTop = 400;
    expect(distanceFromTail(el)).toBeGreaterThan(SCROLL_TAIL_THRESHOLD);

    // BUG without pin guard: onScroll would set follow=false
    ctrl.onScroll(el);
    expect(ctrl.follow, "programmatic mid-pin scroll must keep followTail").toBe(true);

    flushRaf();
    expect(ctrl.isPinning()).toBe(false);
    // after pin settles at true bottom
    el.scrollTop = el.scrollHeight - el.clientHeight;
    ctrl.onScroll(el);
    expect(ctrl.follow).toBe(true);
  });

  it("user scroll-up pauses follow; return near bottom resumes", () => {
    const ctrl = createFollowTailController();
    const el = fakeScroller({ scrollHeight: 1000, clientHeight: 300, scrollTop: 700 });
    expect(ctrl.onScroll(el)).toBe(true);

    el.scrollTop = 100;
    expect(ctrl.onScroll(el)).toBe(false);
    expect(ctrl.pinIfFollowing(el)).toBe(false);

    el.scrollTop = 700;
    expect(ctrl.onScroll(el)).toBe(true);
    expect(ctrl.pinIfFollowing(el)).toBe(true);
    flushRaf();
  });

  it("forcePin re-enables follow even after user scrolled up", () => {
    const ctrl = createFollowTailController();
    const el = fakeScroller({ scrollHeight: 1000, clientHeight: 300, scrollTop: 0 });
    ctrl.onScroll(el);
    expect(ctrl.follow).toBe(false);
    ctrl.forcePin(el);
    expect(ctrl.follow).toBe(true);
    expect(el.scrollTop).toBe(1000);
    flushRaf();
  });

  it("approval card arrival follows the current tail preference", () => {
    const ctrl = createFollowTailController();
    const el = fakeScroller({ scrollHeight: 600, clientHeight: 300, scrollTop: 300 });
    ctrl.onScroll(el);

    // The card is rendered below the existing content; a user at the tail
    // should follow it to the new bottom.
    el.scrollHeight = 900;
    expect(ctrl.pinIfFollowing(el)).toBe(true);
    expect(el.scrollTop).toBe(900);
    flushRaf();

    // A user reading history should keep their position when the card arrives.
    const historyCtrl = createFollowTailController();
    const historyEl = fakeScroller({ scrollHeight: 600, clientHeight: 300, scrollTop: 0 });
    historyCtrl.onScroll(historyEl);
    historyEl.scrollHeight = 900;
    expect(historyCtrl.pinIfFollowing(historyEl)).toBe(false);
    expect(historyEl.scrollTop).toBe(0);
  });
});
