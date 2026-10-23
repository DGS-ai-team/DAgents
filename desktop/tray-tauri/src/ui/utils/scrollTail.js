/** 对话流 stick-to-bottom：阈值更新时避免 smooth + scroll 监听误关 follow。 */

export const SCROLL_TAIL_THRESHOLD = 48;

export function distanceFromTail(el) {
  if (!el) return Number.POSITIVE_INFINITY;
  return el.scrollHeight - el.scrollTop - el.clientHeight;
}

export function isNearTail(el, threshold = SCROLL_TAIL_THRESHOLD) {
  return distanceFromTail(el) <= threshold;
}

/**
 * 瞬时钉到末尾。临时关掉 scroll-behavior，避免 CSS `smooth` 让
 * scrollTop 赋值变成动画，从而在流式高频更新时触发「假上滑」。
 */
export function pinToTail(el) {
  if (!el) return;
  const prev = el.style.scrollBehavior;
  el.style.scrollBehavior = "auto";
  el.scrollTop = el.scrollHeight;
  el.style.scrollBehavior = prev;
}

/**
 * followTail 控制器：程序化 pin 期间忽略 scroll 事件，防止误关跟随。
 *
 * @param {{ threshold?: number, schedule?: (fn: () => void) => void }} [opts]
 */
export function createFollowTailController(opts = {}) {
  const threshold = opts.threshold ?? SCROLL_TAIL_THRESHOLD;
  const schedule =
    opts.schedule ||
    ((fn) => {
      if (typeof requestAnimationFrame === "function") {
        requestAnimationFrame(fn);
      } else {
        setTimeout(fn, 0);
      }
    });

  let follow = true;
  let pinDepth = 0;

  return {
    get follow() {
      return follow;
    },
    isPinning() {
      return pinDepth > 0;
    },
    setFollow(value) {
      follow = !!value;
    },
    /** @returns {boolean} 更新后是否仍跟随末尾 */
    onScroll(el) {
      if (pinDepth > 0) return follow;
      follow = isNearTail(el, threshold);
      return follow;
    },
    /**
     * 若正在跟随则钉到末尾。
     * @returns {boolean} 是否执行了 pin
     */
    pinIfFollowing(el) {
      if (!follow || !el) return false;
      pinDepth += 1;
      try {
        pinToTail(el);
        follow = true;
      } finally {
        schedule(() => {
          pinDepth = Math.max(0, pinDepth - 1);
        });
      }
      return true;
    },
    /** 强制跟随并钉尾（发送消息 / 切会话）。 */
    forcePin(el) {
      follow = true;
      if (!el) return false;
      pinDepth += 1;
      try {
        pinToTail(el);
      } finally {
        schedule(() => {
          pinDepth = Math.max(0, pinDepth - 1);
        });
      }
      return true;
    },
  };
}
